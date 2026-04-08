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

func NewKVStore(dir string) (*KVStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &KVStore{data: make(map[string][]byte), dir: dir}, nil
}

func (s *KVStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if ok {
		return append([]byte(nil), v...), true
	}
	data, err := os.ReadFile(filepath.Join(s.dir, key))
	if err != nil {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (s *KVStore) Set(key string, value []byte) error {
	value = append([]byte(nil), value...)
	s.mu.Lock()
	prev, hadPrev := s.data[key]
	s.data[key] = value
	s.mu.Unlock()
	if err := os.WriteFile(filepath.Join(s.dir, key), value, 0644); err != nil {
		s.mu.Lock()
		if hadPrev {
			s.data[key] = prev
		} else {
			delete(s.data, key)
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *KVStore) Delete(key string) error {
	s.mu.Lock()
	prev, hadPrev := s.data[key]
	delete(s.data, key)
	s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.dir, key)); err != nil && !os.IsNotExist(err) {
		s.mu.Lock()
		if hadPrev {
			s.data[key] = prev
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *KVStore) Keys() []string {
	s.mu.RLock()
	var keys []string
	seen := make(map[string]bool, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
		seen[k] = true
	}
	s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return keys
	}
	for _, entry := range entries {
		name := entry.Name()
		if seen[name] {
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

func (s *KVStore) Clear() error {
	s.mu.Lock()
	prev := s.data
	s.data = make(map[string][]byte)
	s.mu.Unlock()
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !os.IsNotExist(err) {
			s.mu.Lock()
			s.data = prev
			s.mu.Unlock()
			return err
		}
	}
	return nil
}
