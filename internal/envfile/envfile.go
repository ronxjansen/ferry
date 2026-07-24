// Package envfile loads, merges, hashes and ships environment files. Values
// are delivered via files (API-side --env-file locally, 0600 files on the
// host) — never through argv. Files are only re-shipped when the content hash
// changes.
package envfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	osexec "os/exec"
	"sort"
	"strings"

	"github.com/ronxjansen/ferry/internal/exec"
)

// Load parses env files in order (later files win), returning a key→value
// map. Files ending in .enc or with encryption sops-age are decrypted via
// sops; plaintext never leaves the machine unencrypted.
func Load(files []string, sopsAge bool) (map[string]string, error) {
	env := map[string]string{}
	for _, f := range files {
		var data []byte
		var err error
		if sopsAge {
			out, sopsErr := osexec.Command("sops", "--decrypt", f).Output()
			if sopsErr != nil {
				return nil, fmt.Errorf("sops decrypt %s failed: %w", f, sopsErr)
			}
			data = out
		} else {
			data, err = os.ReadFile(f)
			if err != nil {
				return nil, fmt.Errorf("failed to read env file: %w", err)
			}
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			env[strings.TrimSpace(key)] = value
		}
	}
	return env, nil
}

// Render produces deterministic env-file content from a map (sorted keys).
func Render(env map[string]string) []byte {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return []byte(b.String())
}

// Hash returns the sha256 hex of rendered content.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// WriteTemp writes content to a 0600 temp file for local --env-file delivery
// and returns its path. Caller removes it.
func WriteTemp(content []byte) (string, error) {
	f, err := os.CreateTemp("", "ferry-env-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := f.Write(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// RemotePath is where a service's env file lives on the host.
func RemotePath(service string) string {
	return fmt.Sprintf(".ferry/apps/%s/env", service)
}

// Ship writes the env file to the host (0600) if its hash changed, returning
// whether a write happened.
func Ship(h *exec.Host, service string, content []byte) (bool, error) {
	path := RemotePath(service)
	remote, err := h.Run(fmt.Sprintf("sha256sum %q 2>/dev/null | cut -d' ' -f1", path))
	if err == nil && strings.TrimSpace(remote) == Hash(content) {
		return false, nil
	}
	if err := h.WriteFile(path, content, "0600"); err != nil {
		return false, err
	}
	return true, nil
}
