//go:build linux

package main

import (
	"os"
	"syscall"
)

// isPortFree reports whether a serial device is not currently locked by
// another process (e.g. another instance of this tool, or a getty).
func isPortFree(name string) bool {
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return false
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return true
}
