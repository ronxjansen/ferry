// Package git resolves the deploy version from the working tree.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Version returns the short HEAD SHA and whether the tree is dirty.
func Version(dir string) (sha string, dirty bool, err error) {
	out, err := run(dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("not a git repository (ferry versions deploys by git SHA): %w", err)
	}
	sha = out

	status, err := run(dir, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	return sha, status != "", nil
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
