// Package audit appends mutating actions to the server-side audit log at
// ~/.ferry/audit.log: [time][performer][version] action
package audit

import (
	"fmt"
	"time"

	"github.com/ronxjansen/ferry/internal/exec"
)

// Path is the audit log location on the host.
const Path = ".ferry/audit.log"

// Record appends one line; failures are returned but callers may treat the
// audit trail as best-effort.
func Record(h *exec.Host, performer, version, action string) error {
	line := fmt.Sprintf("[%s][%s][%s] %s", time.Now().UTC().Format(time.RFC3339), performer, version, action)
	_, err := h.Run(fmt.Sprintf("mkdir -p .ferry && printf '%%s\\n' %q >> %q", line, Path))
	return err
}

// Tail returns the last n lines of the audit log.
func Tail(h *exec.Host, n int) (string, error) {
	return h.Run(fmt.Sprintf("tail -n %d %q 2>/dev/null || true", n, Path))
}
