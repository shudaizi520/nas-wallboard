package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/persist"
)

type RuntimeHealth struct {
	InstanceID  string    `json:"instance_id"`
	Type        string    `json:"type"`
	Running     bool      `json:"running"`
	Healthy     bool      `json:"healthy"`
	Message     string    `json:"message"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastSuccess time.Time `json:"last_success"`
}

type runtimeEntry struct {
	fingerprint string
	typeID      string
	cancel      context.CancelFunc
	done        chan struct{}
	collector   Collector
}

type Manager struct {
	mu       sync.Mutex
	registry *Registry
	state    *persist.Store
	secrets  *persist.SecretStore
	ctx      context.Context
	cancel   context.CancelFunc
	runtimes map[string]*runtimeEntry
	health   map[string]RuntimeHealth
	closed   bool
}

func NewManager(registry *Registry, state *persist.Store, secrets *persist.SecretStore) *Manager {
	return &Manager{registry: registry, state: state, secrets: secrets, runtimes: map[string]*runtimeEntry{}, health: map[string]RuntimeHealth{}}
}

func (manager *Manager) Start(ctx context.Context) error {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return errors.New("integration manager is closed")
	}
	manager.ensureContext(ctx)
	manager.mu.Unlock()
	return manager.Apply(ctx, nil, manager.state.Snapshot().Integrations)
}

func (manager *Manager) Apply(ctx context.Context, _ []persist.Integration, after []persist.Integration) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return errors.New("integration manager is closed")
	}
	manager.ensureContext(ctx)
	wanted := map[string]persist.Integration{}
	for _, instance := range after {
		wanted[instance.ID] = instance
	}
	var failures []error
	for id, running := range manager.runtimes {
		instance, exists := wanted[id]
		if !exists || !instance.Enabled {
			running.cancel()
			delete(manager.runtimes, id)
			<-running.done
			manager.health[id] = RuntimeHealth{InstanceID: id, Type: running.typeID, Message: "已停止", UpdatedAt: time.Now()}
		}
	}
	for _, instance := range after {
		if !instance.Enabled {
			continue
		}
		fingerprint, err := runtimeFingerprint(instance)
		if err != nil {
			failures = append(failures, err)
			manager.setBuildFailure(instance)
			continue
		}
		if current, exists := manager.runtimes[instance.ID]; exists && current.fingerprint == fingerprint {
			continue
		}
		definition, ok := manager.registry.Definition(instance.Type)
		if !ok {
			failures = append(failures, ErrUnknownIntegration)
			manager.setBuildFailure(instance)
			continue
		}
		secrets, err := manager.readSecrets(instance)
		if err != nil {
			failures = append(failures, err)
			manager.setBuildFailure(instance)
			continue
		}
		collector, err := definition.Collector(cloneConfig(instance.Config), secrets)
		if err != nil {
			failures = append(failures, err)
			manager.setBuildFailure(instance)
			continue
		}
		if collector == nil {
			failures = append(failures, errors.New("collector is nil"))
			manager.setBuildFailure(instance)
			continue
		}
		runtimeCtx, cancel := context.WithCancel(manager.ctx)
		replacement := &runtimeEntry{fingerprint: fingerprint, typeID: instance.Type, cancel: cancel, done: make(chan struct{}), collector: collector}
		previous := manager.runtimes[instance.ID]
		if previous != nil {
			previous.cancel()
			<-previous.done
		}
		manager.runtimes[instance.ID] = replacement
		manager.health[instance.ID] = RuntimeHealth{InstanceID: instance.ID, Type: instance.Type, Running: true, Healthy: true, Message: "运行中", UpdatedAt: time.Now()}
		go manager.run(instance.ID, runtimeCtx, replacement, collector)
	}
	return errors.Join(failures...)
}

func (manager *Manager) Health() []RuntimeHealth {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := make([]RuntimeHealth, 0, len(manager.health))
	for _, value := range manager.health {
		if entry := manager.runtimes[value.InstanceID]; value.Running && value.Healthy && entry != nil {
			if reader, ok := entry.collector.(interface {
				CollectionHealth() (bool, string, time.Time)
			}); ok {
				value.Healthy, value.Message, value.LastSuccess = reader.CollectionHealth()
			}
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].InstanceID < result[j].InstanceID })
	return result
}

func (manager *Manager) Close() {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	if manager.cancel != nil {
		manager.cancel()
	}
	entries := make([]*runtimeEntry, 0, len(manager.runtimes))
	for _, entry := range manager.runtimes {
		entry.cancel()
		entries = append(entries, entry)
	}
	manager.runtimes = map[string]*runtimeEntry{}
	manager.mu.Unlock()
	for _, entry := range entries {
		<-entry.done
	}
}

func (manager *Manager) run(instanceID string, ctx context.Context, entry *runtimeEntry, collector Collector) {
	err := collector.Run(ctx)
	close(entry.done)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.runtimes[instanceID] != entry {
		return
	}
	if ctx.Err() != nil {
		return
	}
	health := manager.health[instanceID]
	health.Running, health.Healthy, health.UpdatedAt = false, false, time.Now()
	if err != nil {
		health.Message = "采集器运行失败"
	} else {
		health.Message = "采集器意外停止"
	}
	manager.health[instanceID] = health
}

func (manager *Manager) ensureContext(parent context.Context) {
	if manager.ctx == nil {
		manager.ctx, manager.cancel = context.WithCancel(parent)
	}
}

func (manager *Manager) readSecrets(instance persist.Integration) (Secrets, error) {
	result := Secrets{}
	for key, ref := range instance.SecretRefs {
		value, err := manager.secrets.Read(ref)
		if err != nil {
			return nil, fmt.Errorf("read configured secret %q: %w", key, err)
		}
		result[key] = value
	}
	return result, nil
}

func (manager *Manager) setBuildFailure(instance persist.Integration) {
	_, running := manager.runtimes[instance.ID]
	manager.health[instance.ID] = RuntimeHealth{InstanceID: instance.ID, Type: instance.Type, Running: running, Message: "无法应用新配置", UpdatedAt: time.Now()}
}

func runtimeFingerprint(instance persist.Integration) (string, error) {
	data, err := json.Marshal(struct {
		Type    string                       `json:"type"`
		Config  map[string]any               `json:"config"`
		Secrets map[string]persist.SecretRef `json:"secrets"`
	}{instance.Type, instance.Config, instance.SecretRefs})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
