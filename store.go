package main

import (
	"os"
	"path/filepath"
	"sync"
)

type KVStore struct {
	data map[string][]byte
	mu   sync.RWMutex
	dir  string
}

func NewKVStore(dir string) *KVStore {
	os.MkdirAll(dir, 0755)
	return &KVStore{data: make(map[string][]byte), dir: dir}
}

func (s *KVStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if ok {
		return v, true
	}
	data, err := os.ReadFile(filepath.Join(s.dir, key))
	if err != nil {
		return nil, false
	}
	return data, true
}

func (s *KVStore) Set(key string, value []byte) {
	s.mu.Lock()
	s.data[key] = value
	s.mu.Unlock()
	os.WriteFile(filepath.Join(s.dir, key), value, 0644)
}

func (s *KVStore) Delete(key string) {
	delete(s.data, key)
	os.Remove(filepath.Join(s.dir, key))
}

func (s *KVStore) Keys() []string {
	var keys []string
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}

func (s *KVStore) Clear() {
	s.data = make(map[string][]byte)
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		os.Remove(filepath.Join(s.dir, e.Name()))
	}
}
