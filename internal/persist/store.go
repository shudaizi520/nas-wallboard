package persist

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const MaxStateBytes int64 = 1 << 20

const maximumBackups = 3

var ErrNewerSchema = errors.New("state schema is newer than this application")

type Store struct {
	mu    sync.RWMutex
	root  string
	path  string
	state State
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}
	store := &Store{root: root, path: filepath.Join(root, "state.json")}
	data, err := ReadLimited(store.path, MaxStateBytes)
	if errors.Is(err, os.ErrNotExist) {
		store.state = newState()
		if err := store.write(store.state); err != nil {
			return nil, err
		}
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	state, err := decodeState(data)
	if err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	if state.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: got %d, support up to %d", ErrNewerSchema, state.SchemaVersion, CurrentSchemaVersion)
	}
	if state.SchemaVersion < CurrentSchemaVersion {
		if err := store.backup(data); err != nil {
			return nil, fmt.Errorf("backup state before migration: %w", err)
		}
		state.SchemaVersion = CurrentSchemaVersion
		normalizeState(&state)
		if err := store.write(state); err != nil {
			return nil, fmt.Errorf("write migrated state: %w", err)
		}
	}
	normalizeState(&state)
	store.state = state
	return store, nil
}

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.state)
}

func (s *Store) Reload() error {
	loaded, err := Open(s.root)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = loaded.Snapshot()
	return nil
}

func (s *Store) Update(change func(*State) error) error {
	if change == nil {
		return errors.New("state update function is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	if err := change(&next); err != nil {
		return err
	}
	if next.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("schema_version must remain %d", CurrentSchemaVersion)
	}
	normalizeState(&next)
	if err := s.write(next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *Store) write(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > MaxStateBytes {
		return fmt.Errorf("encoded state exceeds %d bytes", MaxStateBytes)
	}
	if err := WriteAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func (s *Store) backup(data []byte) error {
	directory := filepath.Join(s.root, "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	name := fmt.Sprintf("state-%020d-%s.json", time.Now().UnixNano(), hex.EncodeToString(random))
	if err := WriteAtomic(filepath.Join(directory, name), data, 0o600); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for len(entries) > maximumBackups {
		if err := os.Remove(filepath.Join(directory, entries[0].Name())); err != nil {
			return err
		}
		entries = entries[1:]
	}
	return nil
}

func decodeState(data []byte) (State, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return State{}, errors.New("multiple state documents are not allowed")
		}
		return State{}, err
	}
	return state, nil
}

func normalizeState(state *State) {
	if state.Integrations == nil {
		state.Integrations = []Integration{}
	}
	if state.Widgets == nil {
		state.Widgets = []Widget{}
	}
}

func cloneState(state State) State {
	data, err := json.Marshal(state)
	if err != nil {
		panic(fmt.Sprintf("clone state: %v", err))
	}
	clone, err := decodeState(data)
	if err != nil {
		panic(fmt.Sprintf("clone state: %v", err))
	}
	normalizeState(&clone)
	return clone
}
