// Package hooks runs lifecycle hooks. pre_build runs locally; pre_deploy,
// post_deploy and post_app_boot run on the target server. Entries are script
// paths or inline shell commands; executable files at .ferry/hooks/<name>
// are auto-discovered.
package hooks

import (
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
)

// Env is the environment passed to every hook.
type Env struct {
	Version   string
	Service   string
	Server    string
	Image     string
	Performer string
	Runtime   time.Duration
	Preview   string // preview SHA, empty for regular deploys
}

func (e Env) vars() []string {
	return []string{
		"FERRY_VERSION=" + e.Version,
		"FERRY_SERVICE=" + e.Service,
		"FERRY_SERVER=" + e.Server,
		"FERRY_IMAGE=" + e.Image,
		"FERRY_PERFORMER=" + e.Performer,
		"FERRY_RUNTIME=" + e.Runtime.Truncate(time.Second).String(),
		"FERRY_PREVIEW=" + e.Preview,
	}
}

// Runner executes hooks from config plus auto-discovered scripts.
type Runner struct {
	Hooks map[config.HookType]config.Hook
	Dir   string // project dir; .ferry/hooks lives here
}

// entries returns configured entries plus the auto-discovered script.
func (r *Runner) entries(t config.HookType) []string {
	entries := append([]string{}, r.Hooks[t]...)
	discovered := filepath.Join(r.Dir, ".ferry", "hooks", string(t))
	if info, err := os.Stat(discovered); err == nil && info.Mode()&0o111 != 0 {
		entries = append(entries, discovered)
	}
	return entries
}

// isScript reports whether an entry is a script file (relative to Dir).
func (r *Runner) scriptPath(entry string) (string, bool) {
	p := entry
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Dir, entry)
	}
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, true
	}
	return "", false
}

// RunLocal executes a hook on this machine.
func (r *Runner) RunLocal(t config.HookType, env Env) error {
	for _, entry := range r.entries(t) {
		var cmd *osexec.Cmd
		if path, ok := r.scriptPath(entry); ok {
			cmd = osexec.Command(path)
		} else {
			cmd = osexec.Command("sh", "-c", entry)
		}
		cmd.Dir = r.Dir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), env.vars()...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("hook %s (%s) failed: %w", t, entry, err)
		}
	}
	return nil
}

// RunRemote executes a hook on the target server. Script files are read
// locally and piped to a remote shell.
func (r *Runner) RunRemote(h *exec.Host, t config.HookType, env Env) error {
	exports := make([]string, 0, 8)
	for _, v := range env.vars() {
		key, val, _ := strings.Cut(v, "=")
		exports = append(exports, fmt.Sprintf("export %s=%q;", key, val))
	}
	prefix := strings.Join(exports, " ")

	for _, entry := range r.entries(t) {
		script := entry
		if path, ok := r.scriptPath(entry); ok {
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("hook %s: %w", t, err)
			}
			script = string(content)
		}
		remote := fmt.Sprintf("%s sh <<'FERRY_HOOK_EOF'\n%s\nFERRY_HOOK_EOF", prefix, script)
		if err := h.Stream(remote); err != nil {
			return fmt.Errorf("hook %s (%s) failed: %w", t, entry, err)
		}
	}
	return nil
}
