package truenas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

type Collectors struct {
	caller   Caller
	selected []config.AppConfig
	now      func() time.Time

	metadataMu   sync.Mutex
	graphsLoaded bool
	interfaceIDs []string

	systemMu        sync.Mutex
	systemCache     systemWire
	systemReadAt    time.Time
	systemAttemptAt time.Time
	systemError     error

	memoryMu        sync.Mutex
	memoryAvailable uint64
	memoryReadAt    time.Time
	memoryAttemptAt time.Time
	memoryError     error

	alertsMu        sync.Mutex
	alertsCache     []alertWire
	alertsReadAt    time.Time
	alertsAttemptAt time.Time
	alertsError     error

	sourceMu     sync.Mutex
	sourceReadAt map[string]time.Time
}

func NewCollectors(caller Caller, selected []config.AppConfig) *Collectors {
	apps := append([]config.AppConfig(nil), selected...)
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Sort == apps[j].Sort {
			return apps[i].ID < apps[j].ID
		}
		return apps[i].Sort < apps[j].Sort
	})
	return &Collectors{
		caller:       caller,
		selected:     apps,
		now:          time.Now,
		sourceReadAt: make(map[string]time.Time),
	}
}

type systemWire struct {
	Hostname      string          `json:"hostname"`
	Version       string          `json:"version"`
	UptimeSeconds json.RawMessage `json:"uptime_seconds"`
	Physmem       json.RawMessage `json:"physmem"`
}

func (c *Collectors) CollectSystem(ctx context.Context) (model.SystemStatus, error) {
	raw, readAt, err := c.readSystem(ctx)
	if err != nil {
		return model.SystemStatus{}, fmt.Errorf("collect system information: %w", err)
	}
	memoryTotal := uintFromJSON(raw.Physmem)
	c.markSource("system", readAt)
	return model.SystemStatus{
		Hostname:         raw.Hostname,
		Version:          raw.Version,
		UptimeSeconds:    int64(uintFromJSON(raw.UptimeSeconds)),
		MemoryTotalBytes: memoryTotal,
	}, nil
}

// SourceReadAt is the oldest successful source read used by the latest
// successful collection. Cache reuse never advances this timestamp.
func (c *Collectors) SourceReadAt(module string) time.Time {
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()
	return c.sourceReadAt[module]
}

func (c *Collectors) markSource(module string, at time.Time) {
	c.sourceMu.Lock()
	c.sourceReadAt[module] = at
	c.sourceMu.Unlock()
}

func freshRead(now, readAt time.Time, ttl time.Duration) bool {
	age := now.Sub(readAt)
	return !readAt.IsZero() && age >= 0 && age < ttl
}

func (c *Collectors) readSystem(ctx context.Context) (systemWire, time.Time, error) {
	c.systemMu.Lock()
	defer c.systemMu.Unlock()
	if freshRead(c.now(), c.systemReadAt, time.Minute) {
		return c.systemCache, c.systemReadAt, nil
	}
	if c.systemError != nil && freshRead(c.now(), c.systemAttemptAt, time.Minute) {
		// Realtime runs every five seconds, but a failed minute-level source
		// must not acquire that faster retry cadence. Return failure, not an
		// expired successful value, until the original interval elapses.
		return systemWire{}, time.Time{}, c.systemError
	}
	c.systemAttemptAt = c.now()
	var raw systemWire
	if err := c.caller.Call(ctx, "system.info", []any{}, &raw); err != nil {
		c.systemError = err
		return systemWire{}, time.Time{}, err
	}
	c.systemError = nil
	c.systemCache, c.systemReadAt = raw, c.now()
	return raw, c.systemReadAt, nil
}

func (c *Collectors) CollectMemory(ctx context.Context) (model.MemoryStatus, error) {
	raw, systemAt, err := c.readSystem(ctx)
	if err != nil {
		return model.MemoryStatus{}, fmt.Errorf("collect memory pressure: %w", err)
	}
	total := uintFromJSON(raw.Physmem)
	if total == 0 {
		return model.MemoryStatus{}, errors.New("collect memory pressure: total memory unavailable")
	}
	available, readAt, err := c.readAvailableMemory(ctx)
	if err != nil {
		return model.MemoryStatus{}, fmt.Errorf("collect memory pressure: %w", err)
	}
	if available > total {
		available = total
	}
	if systemAt.Before(readAt) {
		readAt = systemAt
	}
	c.markSource("memory", readAt)
	return model.MemoryStatus{TotalBytes: total, AvailableBytes: available, AvailablePercent: percent(available, total)}, nil
}

func (c *Collectors) readAvailableMemory(ctx context.Context) (uint64, time.Time, error) {
	c.memoryMu.Lock()
	defer c.memoryMu.Unlock()
	if freshRead(c.now(), c.memoryReadAt, 5*time.Second) {
		return c.memoryAvailable, c.memoryReadAt, nil
	}
	if c.memoryError != nil && freshRead(c.now(), c.memoryAttemptAt, 5*time.Second) {
		return 0, time.Time{}, c.memoryError
	}
	c.memoryAttemptAt = c.now()
	now := c.now().Unix()
	start := now - 10
	if start < 1 {
		start = 1
	}
	var graphs []netdataWire
	if err := c.caller.Call(ctx, "reporting.netdata_get_data", []any{
		[]any{map[string]any{"name": "memory"}},
		map[string]any{"start": start, "end": now, "aggregate": false},
	}, &graphs); err != nil {
		c.memoryError = err
		return 0, time.Time{}, err
	}
	available := uint64(0)
	foundAvailable := false
	for _, graph := range graphs {
		if graph.Name == "memory" {
			if value, found := latestMetric(graph, "available"); found && value >= 0 {
				available = uint64(value)
				foundAvailable = true
			}
			break
		}
	}
	if !foundAvailable {
		c.memoryError = errors.New("available memory unavailable")
		return 0, time.Time{}, c.memoryError
	}
	c.memoryError = nil
	c.memoryAvailable, c.memoryReadAt = available, c.now()
	return available, c.memoryReadAt, nil
}

type reportingGraphWire struct {
	Name        string   `json:"name"`
	Identifiers []string `json:"identifiers"`
}

type netdataWire struct {
	Name       string              `json:"name"`
	Identifier string              `json:"identifier"`
	Legend     []string            `json:"legend"`
	Data       [][]json.RawMessage `json:"data"`
}

func (c *Collectors) CollectRealtime(ctx context.Context) (model.RealtimeStatus, error) {
	interfaces, memoryTotal, err := c.realtimeMetadata(ctx)
	if err != nil {
		return model.RealtimeStatus{}, err
	}

	graphs := []any{
		map[string]any{"name": "cpu"},
		map[string]any{"name": "cputemp"},
		map[string]any{"name": "memory"},
	}
	for _, identifier := range interfaces {
		graphs = append(graphs, map[string]any{"name": "interface", "identifier": identifier})
	}
	now := c.now().Unix()
	start := now - 10
	if start < 1 {
		start = 1
	}
	params := []any{graphs, map[string]any{
		"start":     start,
		"end":       now,
		"aggregate": false,
	}}
	var raw []netdataWire
	// Serialize the available-memory source with its dedicated collector so
	// a concurrent memory job can reuse this successful realtime read.
	c.memoryMu.Lock()
	if err := c.caller.Call(ctx, "reporting.netdata_get_data", params, &raw); err != nil {
		c.memoryMu.Unlock()
		return model.RealtimeStatus{}, fmt.Errorf("collect realtime information: %w", err)
	}

	var cpu, cpuTemperature, rxRate, txRate float64
	var available uint64
	availableFound := false
	cpuFound, rxFound, txFound := false, false, false
	for _, graph := range raw {
		switch graph.Name {
		case "cpu":
			if value, found := latestMetric(graph, "cpu"); found {
				cpu = clampPercent(value)
				cpuFound = true
			}
		case "cputemp":
			if value, found := latestCPUTemperature(graph); found && value > 0 {
				cpuTemperature = value
			}
		case "memory":
			if value, found := latestMetric(graph, "available"); found && value >= 0 {
				available = uint64(value)
				availableFound = true
			}
		case "interface":
			if value, found := latestMetric(graph, "received"); found {
				rxFound = true
				if value > rxRate {
					rxRate = value
				}
			}
			if value, found := latestMetric(graph, "sent"); found {
				txFound = true
				if value > txRate {
					txRate = value
				}
			}
		}
	}
	if availableFound {
		c.memoryAvailable, c.memoryReadAt = available, c.now()
		c.memoryError = nil
	}
	c.memoryMu.Unlock()
	if !cpuFound || !rxFound || !txFound || !availableFound || memoryTotal == 0 {
		return model.RealtimeStatus{}, errors.New("collect realtime information: required CPU, network or memory sample unavailable")
	}
	used := uint64(0)
	if availableFound && memoryTotal >= available {
		used = memoryTotal - available
	}

	return model.RealtimeStatus{
		CPUPercent:            cpu,
		CPUTemperatureCelsius: cpuTemperature,
		MemoryUsedBytes:       used,
		MemoryTotalBytes:      memoryTotal,
		NetworkRxBps:          kilobitsToBytes(rxRate),
		NetworkTxBps:          kilobitsToBytes(txRate),
	}, nil
}

func (c *Collectors) realtimeMetadata(ctx context.Context) ([]string, uint64, error) {
	c.metadataMu.Lock()
	defer c.metadataMu.Unlock()

	if !c.graphsLoaded {
		var graphs []reportingGraphWire
		if err := c.caller.Call(ctx, "reporting.netdata_graphs", []any{}, &graphs); err != nil {
			return nil, 0, fmt.Errorf("collect reporting graph metadata: %w", err)
		}
		for _, graph := range graphs {
			if graph.Name == "interface" {
				c.interfaceIDs = append([]string(nil), graph.Identifiers...)
				break
			}
		}
		c.graphsLoaded = true
	}
	system, _, err := c.readSystem(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("collect memory metadata: %w", err)
	}
	return append([]string(nil), c.interfaceIDs...), uintFromJSON(system.Physmem), nil
}

func latestCPUTemperature(graph netdataWire) (float64, bool) {
	var hottest float64
	foundCore := false
	for _, label := range graph.Legend {
		suffix := strings.TrimPrefix(label, "cpu")
		if suffix == label || suffix == "" {
			continue
		}
		if _, err := strconv.Atoi(suffix); err != nil {
			continue
		}
		value, found := latestMetric(graph, label)
		if !found || value <= 0 {
			continue
		}
		if !foundCore || value > hottest {
			hottest = value
			foundCore = true
		}
	}
	if foundCore {
		return hottest, true
	}
	return latestMetric(graph, "cpu")
}

func latestMetric(graph netdataWire, name string) (float64, bool) {
	index := -1
	for i, label := range graph.Legend {
		if label == name {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, false
	}
	for i := len(graph.Data) - 1; i >= 0; i-- {
		if index >= len(graph.Data[i]) {
			continue
		}
		var value float64
		if isNullJSON(graph.Data[i][index]) {
			continue
		}
		if err := json.Unmarshal(graph.Data[i][index], &value); err == nil && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, true
		}
	}
	return 0, false
}

func kilobitsToBytes(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return value * 1000 / 8
}

type poolWire struct {
	ID        json.RawMessage `json:"id"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Healthy   bool            `json:"healthy"`
	Size      json.RawMessage `json:"size"`
	Allocated json.RawMessage `json:"allocated"`
	Scan      *struct {
		State      string  `json:"state"`
		Percentage float64 `json:"percentage"`
	} `json:"scan"`
}

func (c *Collectors) CollectPools(ctx context.Context) ([]model.PoolStatus, error) {
	var raw []poolWire
	if err := c.caller.Call(ctx, "pool.query", []any{}, &raw); err != nil {
		return nil, fmt.Errorf("collect pools: %w", err)
	}
	pools := make([]model.PoolStatus, 0, len(raw))
	for _, item := range raw {
		total := uintFromJSON(item.Size)
		used := uintFromJSON(item.Allocated)
		status := model.PoolStatus{
			ID:          stringFromJSON(item.ID),
			Name:        item.Name,
			Status:      normalizePoolState(item.Status),
			Healthy:     item.Healthy,
			UsedBytes:   used,
			TotalBytes:  total,
			UsedPercent: percent(used, total),
		}
		if item.Scan != nil {
			status.ScanState = normalizeScanState(item.Scan.State)
			status.ScanProgress = clampPercent(item.Scan.Percentage)
		}
		pools = append(pools, status)
	}
	return pools, nil
}

func (c *Collectors) CollectDisks(ctx context.Context) ([]model.DiskStatus, error) {
	var raw map[string]*float64
	if err := c.caller.Call(ctx, "disk.temperatures", []any{}, &raw); err != nil {
		return nil, fmt.Errorf("collect disk temperatures: %w", err)
	}
	var identities []struct {
		Name   string          `json:"name"`
		Model  string          `json:"model"`
		Serial string          `json:"serial"`
		Size   json.RawMessage `json:"size"`
	}
	if err := c.caller.Call(ctx, "disk.query", []any{}, &identities); err != nil {
		return nil, fmt.Errorf("collect disk identity: %w", err)
	}
	byName := make(map[string]struct {
		model  string
		serial string
		size   uint64
	}, len(identities))
	for _, identity := range identities {
		byName[identity.Name] = struct {
			model  string
			serial string
			size   uint64
		}{
			model:  strings.TrimSpace(identity.Model),
			serial: strings.TrimSpace(identity.Serial),
			size:   uintFromJSON(identity.Size),
		}
	}
	disks := make([]model.DiskStatus, 0, len(raw))
	for name, temperature := range raw {
		if temperature == nil {
			continue
		}
		identity := byName[name]
		disks = append(disks, model.DiskStatus{
			ID: name, Name: name, Temperature: *temperature,
			Model: identity.model, Serial: identity.serial, SizeBytes: identity.size,
		})
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks, nil
}

func (c *Collectors) CollectDiskHealth(ctx context.Context) ([]model.DiskHealthStatus, error) {
	raw, readAt, err := c.readAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect SMART summary: %w", err)
	}
	c.markSource("smart", readAt)
	result := make([]model.DiskHealthStatus, 0, len(raw))
	for _, item := range raw {
		search := strings.ToLower(item.Class + " " + item.Formatted)
		if !strings.Contains(search, "smart") {
			continue
		}
		// TrueNAS 25.10 does not expose SMART health through disk.query.
		// Active SMART alerts are therefore reduced to anonymous exceptions;
		// raw messages, serials, device paths, and alert arguments never leave
		// this collector.
		result = append(result, model.DiskHealthStatus{Name: "SMART 告警", State: "failed"})
	}
	return result, nil
}

type replicationWire struct {
	Enabled bool `json:"enabled"`
	State   *struct {
		State        string          `json:"state"`
		Error        string          `json:"error"`
		Datetime     json.RawMessage `json:"datetime"`
		LastSnapshot string          `json:"last_snapshot"`
	} `json:"state"`
}

func (c *Collectors) CollectReplication(ctx context.Context) (model.ReplicationStatus, error) {
	var raw []replicationWire
	if err := c.caller.Call(ctx, "replication.query", []any{}, &raw); err != nil {
		return model.ReplicationStatus{}, fmt.Errorf("collect replication summary: %w", err)
	}
	result := model.ReplicationStatus{Total: len(raw)}
	for _, item := range raw {
		if !item.Enabled {
			result.Disabled++
			continue
		}
		result.Enabled++
		state := ""
		hasError := false
		if item.State != nil {
			state = strings.ToUpper(strings.TrimSpace(item.State.State))
			hasError = strings.TrimSpace(item.State.Error) != ""
		}
		switch state {
		case "ERROR", "FAILED", "FAULTED":
			result.Failed++
		case "RUNNING":
			result.Running++
		default:
			if hasError {
				result.Failed++
			} else if item.State == nil || (isNullJSON(item.State.Datetime) && strings.TrimSpace(item.State.LastSnapshot) == "") {
				result.NeverRun++
			}
		}
	}
	return result, nil
}

func isNullJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

type appWire struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	State            string `json:"state"`
	UpgradeAvailable bool   `json:"upgrade_available"`
}

func (c *Collectors) CollectApps(ctx context.Context) ([]model.AppStatus, error) {
	var raw []appWire
	if err := c.caller.Call(ctx, "app.query", []any{}, &raw); err != nil {
		return nil, fmt.Errorf("collect apps: %w", err)
	}
	byID := make(map[string]appWire, len(raw))
	for _, item := range raw {
		byID[item.ID] = item
	}
	if len(c.selected) == 0 {
		apps := make([]model.AppStatus, 0, len(raw))
		for _, item := range raw {
			apps = append(apps, model.AppStatus{ID: item.ID, Name: firstText(item.Name, item.ID), State: normalizeAppState(item.State), UpdateAvailable: item.UpgradeAvailable})
		}
		sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
		return apps, nil
	}
	apps := make([]model.AppStatus, 0, len(c.selected))
	for _, selected := range c.selected {
		item, found := byID[selected.ID]
		name := selected.Name
		if name == "" {
			name = item.Name
		}
		if name == "" {
			name = selected.ID
		}
		state := "UNKNOWN"
		if found {
			state = normalizeAppState(item.State)
		}
		apps = append(apps, model.AppStatus{
			ID:              selected.ID,
			Name:            name,
			State:           state,
			UpdateAvailable: found && item.UpgradeAvailable,
			Link:            selected.Link,
		})
	}
	return apps, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type alertWire struct {
	UUID      string          `json:"uuid"`
	Level     string          `json:"level"`
	Class     string          `json:"klass"`
	Formatted string          `json:"formatted"`
	Datetime  json.RawMessage `json:"datetime"`
	Dismissed bool            `json:"dismissed"`
}

func (c *Collectors) CollectAlerts(ctx context.Context) ([]model.AlertStatus, error) {
	raw, readAt, err := c.readAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect alerts: %w", err)
	}
	c.markSource("alerts", readAt)
	alerts := make([]model.AlertStatus, 0, len(raw))
	for _, item := range raw {
		if item.Dismissed {
			continue
		}
		alerts = append(alerts, model.AlertStatus{
			ID:         item.UUID,
			Level:      normalizeAlertLevel(item.Level),
			Title:      item.Class,
			Message:    item.Formatted,
			OccurredAt: timeFromJSON(item.Datetime),
		})
	}
	return alerts, nil
}

func (c *Collectors) readAlerts(ctx context.Context) ([]alertWire, time.Time, error) {
	c.alertsMu.Lock()
	defer c.alertsMu.Unlock()
	if freshRead(c.now(), c.alertsReadAt, 30*time.Second) {
		return c.alertsCache, c.alertsReadAt, nil
	}
	if c.alertsError != nil && freshRead(c.now(), c.alertsAttemptAt, 30*time.Second) {
		return nil, time.Time{}, c.alertsError
	}
	c.alertsAttemptAt = c.now()
	var raw []alertWire
	if err := c.caller.Call(ctx, "alert.list", []any{}, &raw); err != nil {
		c.alertsError = err
		return nil, time.Time{}, err
	}
	c.alertsError = nil
	c.alertsCache, c.alertsReadAt = raw, c.now()
	return raw, c.alertsReadAt, nil
}

func uintFromJSON(raw json.RawMessage) uint64 {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0
	}
	var value uint64
	if json.Unmarshal(trimmed, &value) == nil {
		return value
	}
	var decimal float64
	if json.Unmarshal(trimmed, &decimal) == nil {
		if decimal <= 0 {
			return 0
		}
		maximum := ^uint64(0)
		if decimal >= float64(maximum) {
			return maximum
		}
		return uint64(decimal)
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		if value, err := strconv.ParseUint(text, 10, 64); err == nil {
			return value
		}
		if decimal, err := strconv.ParseFloat(text, 64); err == nil && decimal > 0 {
			maximum := ^uint64(0)
			if decimal >= float64(maximum) {
				return maximum
			}
			return uint64(decimal)
		}
	}
	return 0
}

func stringFromJSON(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		return number.String()
	}
	return ""
}

func timeFromJSON(raw json.RawMessage) time.Time {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		parsed, _ := time.Parse(time.RFC3339Nano, text)
		return parsed
	}
	var wrapped struct {
		Milliseconds int64 `json:"$date"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Milliseconds != 0 {
		return time.UnixMilli(wrapped.Milliseconds).UTC()
	}
	return time.Time{}
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return clampPercent(float64(used) * 100 / float64(total))
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizePoolState(value string) string {
	switch strings.ToUpper(value) {
	case "ONLINE", "DEGRADED", "FAULTED", "OFFLINE", "UNAVAILABLE", "REMOVED":
		return strings.ToUpper(value)
	default:
		return "UNKNOWN"
	}
}

func normalizeScanState(value string) string {
	switch strings.ToUpper(value) {
	case "SCANNING", "FINISHED", "CANCELED", "PAUSED":
		return strings.ToUpper(value)
	default:
		return "UNKNOWN"
	}
}

func normalizeAppState(value string) string {
	switch strings.ToUpper(value) {
	case "RUNNING", "STOPPED", "DEPLOYING", "CRASHED":
		return strings.ToUpper(value)
	default:
		return "UNKNOWN"
	}
}

func normalizeAlertLevel(value string) string {
	switch strings.ToUpper(value) {
	case "CRITICAL", "ERROR", "EMERGENCY", "ALERT":
		return "critical"
	case "WARNING":
		return "warning"
	case "INFO", "NOTICE":
		return "info"
	default:
		return "unknown"
	}
}
