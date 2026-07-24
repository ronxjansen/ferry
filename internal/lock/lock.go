// Package lock implements the deploy lock: an atomic mkdir on the primary
// server with a human-readable details file, Kamal-style. The lock is scoped
// to the project, so unrelated apps sharing a server deploy independently.
package lock

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/exec"
)

const root = ".ferry/locks"

// unsafe collapses anything that could escape the lock directory: an explicit
// `name:` in ferry.yaml is not otherwise validated.
var unsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// dirFor is the lock path for one project. Sibling projects on the same server
// get their own, so their deploys no longer block each other.
func dirFor(project string) string {
	name := strings.Trim(unsafe.ReplaceAllString(strings.ToLower(project), "-"), "-")
	if name == "" {
		name = "default"
	}
	return root + "/" + name
}

// Acquire takes the project's lock or fails with the current holder's details.
func Acquire(h *exec.Host, project, performer, version, message string) error {
	dir := dirFor(project)
	// mkdir -p creates the shared parent; the bare mkdir on the lock itself is
	// what makes taking it atomic, so it must stay non-recursive.
	if _, err := h.Run(fmt.Sprintf("mkdir -p %q && mkdir %q", root, dir)); err != nil {
		details, derr := Status(h, project)
		if derr != nil {
			details = "(no details)"
		}
		return fmt.Errorf("deploy lock for %s already held on %s:\n%s\nRun `ferry lock release` if it is stale", project, h.Server.Name, details)
	}
	details := fmt.Sprintf("Locked by: %s\nVersion: %s\nAt: %s\nMessage: %s\n",
		performer, version, time.Now().UTC().Format(time.RFC3339), message)
	return h.WriteFile(dir+"/details", []byte(details), "0644")
}

// Release drops the project's lock.
func Release(h *exec.Host, project string) error {
	_, err := h.Run(fmt.Sprintf("rm -rf %q", dirFor(project)))
	return err
}

// Status returns the project's lock details, or an error when unlocked.
func Status(h *exec.Host, project string) (string, error) {
	out, err := h.Run(fmt.Sprintf("cat %q 2>/dev/null", dirFor(project)+"/details"))
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no deploy lock held")
	}
	return out, nil
}

// With runs fn holding the project's lock, always releasing it afterwards.
func With(h *exec.Host, project, performer, version, message string, fn func() error) error {
	if err := Acquire(h, project, performer, version, message); err != nil {
		return err
	}
	defer Release(h, project)
	return fn()
}
