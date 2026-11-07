package main

import (
	"syscall"
	"testing"
)

// covers every signal that stops a foreground run
func TestStopSignalsIncludeTheTerminalHangup(t *testing.T) {
	for _, want := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		found := false
		for _, sig := range stopSignals {
			if got, ok := sig.(syscall.Signal); ok && got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("stopSignals = %v, missing %v", stopSignals, want)
		}
	}
}
