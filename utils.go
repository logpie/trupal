package main

import (
	"os"
	"strings"
)

func ReadLines(path string) []string {
	data, _ := os.ReadFile(path)
	return strings.Split(string(data), "\n")
}

func WriteLines(path string, lines []string) {
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func AppendLine(path, line string) {
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.WriteString(line + "\n")
	f.Close()
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func EnsureDir(path string) {
	os.MkdirAll(path, 0755)
}

func CopyFile(src, dst string) {
	data, _ := os.ReadFile(src)
	os.WriteFile(dst, data, 0644)
}

func DeleteFile(path string) {
	os.Remove(path)
}

func TempFile(content string) string {
	f, _ := os.CreateTemp("", "trupal-*")
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func CountLines(path string) int {
	lines := ReadLines(path)
	return len(lines)
}

func HeadLines(path string, n int) []string {
	lines := ReadLines(path)
	if n > len(lines) {
		return lines
	}
	return lines[:n]
}

func TailLines(path string, n int) []string {
	lines := ReadLines(path)
	if n > len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}
