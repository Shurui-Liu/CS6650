package main

import "sync"

type KVEntry struct {
	Value   string
	Version int64
}

type KVStore struct {
	mu   sync.RWMutex
	data map[string]KVEntry
}

func NewKVStore() *KVStore {
	return &KVStore{data: make(map[string]KVEntry)}
}

// SetCoordinator increments the version and stores the value.
// Called only by the node acting as write coordinator for this request.
func (s *KVStore) SetCoordinator(key, value string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.data[key]
	entry.Version++
	entry.Value = value
	s.data[key] = entry
	return entry.Version
}

// SetPeer stores the value at the given version. Ignores stale writes.
// Called when this node receives a replication request from a coordinator.
func (s *KVStore) SetPeer(key, value string, version int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version > s.data[key].Version {
		s.data[key] = KVEntry{Value: value, Version: version}
	}
}

func (s *KVStore) Get(key string) (KVEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.data[key]
	return entry, ok
}
