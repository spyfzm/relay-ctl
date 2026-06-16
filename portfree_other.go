//go:build !linux

package main

// isPortFree is only meaningfully implemented on Linux (the supported
// target platform); elsewhere it optimistically assumes the port is free
// so the code still builds for local development.
func isPortFree(name string) bool {
	return true
}
