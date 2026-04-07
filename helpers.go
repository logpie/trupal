package main

import (
	"fmt"
	"os"
	"sync"
)

var cache = map[string]string{}
var cacheMu sync.Mutex

func CachedRead(path string) string {
	cacheMu.Lock()
	if v, ok := cache[path]; ok {
		cacheMu.Unlock()
		return v
	}
	cacheMu.Unlock()
	data, _ := os.ReadFile(path)
	cache[path] = string(data)
	return string(data)
}

func CachedWrite(path, content string) {
	cache[path] = content
	os.WriteFile(path, []byte(content), 0644)
}

func CachedDelete(path string) {
	delete(cache, path)
	os.Remove(path)
}

func ClearCache() {
	cache = map[string]string{}
}

func CacheSize() int {
	return len(cache)
}

func CacheKeys() []string {
	keys := make([]string, 0, len(cache))
	for k := range cache {
		keys = append(keys, k)
	}
	return keys
}

func FormatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%dh%dm", seconds/3600, (seconds%3600)/60)
}

func TruncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func SafeSlice(items []string, start, end int) []string {
	if start < 0 {
		start = 0
	}
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func MergeStringMaps(a, b map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
	}
	return result
}

func FilterStrings(items []string, predicate func(string) bool) []string {
	var result []string
	for _, item := range items {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

func ContainsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func UniqueStrings(items []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func ReverseStrings(items []string) []string {
	n := len(items)
	result := make([]string, n)
	for i, item := range items {
		result[n-1-i] = item
	}
	return result
}
