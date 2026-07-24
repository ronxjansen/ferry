// Package lock implements the deploy lock: an atomic mkdir on the primary
// server with a human-readable details file, Kamal-style.
package lock

import (
	"fmt"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/exec"
)

const dir = ".ferry/lock"

// Acquire takes the lock or fails with the current holder's details.
func Acquire(h *exec.Host, performer, version, message string) error {
	if _, err := h.Run(fmt.Sprintf("mkdir %q", dir)); err != nil {
		details, derr := Status(h)
		if derr != nil {
			details = "(no details)"
		}
		return fmt.Errorf("deploy lock already held on %s:\n%s\nRun `ferry lock release` if it is stale", h.Server.Name, details)
	}
	details := fmt.Sprintf("Locked by: %s\nVersion: %s\nAt: %s\nMessage: %s\n",
		performer, version, time.Now().UTC().Format(time.RFC3339), message)
	return h.WriteFile(dir+"/details", []byte(details), "0644")
}

// Release drops the lock.
func Release(h *exec.Host) error {
	_, err := h.Run(fmt.Sprintf("rm -rf %q", dir))
	return err
}

// Status returns the lock details, or an error when unlocked.
func Status(h *exec.Host) (string, error) {
	out, err := h.Run(fmt.Sprintf("cat %q 2>/dev/null", dir+"/details"))
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no deploy lock held")
	}
	return out, nil
}

// With runs fn holding the lock, always releasing it afterwards.
func With(h *exec.Host, performer, version, message string, fn func() error) error {
	if err := Acquire(h, performer, version, message); err != nil {
		return err
	}
	defer Release(h)
	return fn()
}
