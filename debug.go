package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// debugLog is a timestamped append-only debug log for trupal internals.
// Written to .trupal.debug in the project directory.
var debugLog struct {
	mu   sync.Mutex
	file *os.File
}

// InitDebugLog opens the debug log file. Call once at startup.
func InitDebugLog(projectDir string) {
	path := filepath.Join(projectDir, ".trupal.debug")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	debugLog.file = f
	Debugf("trupal debug log started for %s", projectDir)
}

// CloseDebugLog closes the debug log file.
func CloseDebugLog() {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	if debugLog.file != nil {
		debugLog.file.Close()
	}
}

func DebugEnabled() bool {
	return debugLog.file != nil
}

func RotateDebugLog(projectDir string) {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	if debugLog.file != nil {
		debugLog.file.Close()
	}
	os.Rename(filepath.Join(projectDir, ".trupal.debug"), filepath.Join(projectDir, ".trupal.debug.old"))
	f, _ := os.OpenFile(filepath.Join(projectDir, ".trupal.debug"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	debugLog.file = f
}

// Debugf writes a timestamped line to the debug log.
func Debugf(format string, args ...interface{}) {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	if debugLog.file == nil {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(debugLog.file, "%s %s\n", ts, msg)
	debugLog.file.Sync()
}
