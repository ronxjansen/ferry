// Package remotejob runs long commands on a server detached from the SSH
// connection that started them. The job runs under nohup with its output and
// exit code captured in files; ferry follows the log over a reconnecting
// stream. A dropped connection — a link stall, a killed terminal — never
// kills the job: ferry (or a rerun) simply re-attaches and picks up where
// the log left off.
package remotejob

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ronxjansen/ferry/internal/exec"
)

// baseDir is where jobs live on the host, relative to the SSH user's home
// (same convention as .ferry/apps and .ferry/audit.log).
const baseDir = ".ferry/jobs"

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sleep is a var so tests can run reconnect loops without waiting.
var sleep = time.Sleep

// maxConsecutiveFailures bounds reconnect attempts that make no progress;
// successful streaming resets the count, so a long build may reconnect many
// times over its lifetime.
const maxConsecutiveFailures = 10

// Job is one detached command on one host, identified by a stable ID so a
// rerun can find and re-attach to it.
type Job struct {
	Host *exec.Host
	ID   string
}

// New returns a job handle; the ID is sanitized for safe shell embedding.
func New(h *exec.Host, id string) *Job {
	return &Job{Host: h, ID: unsafeChars.ReplaceAllString(id, "-")}
}

// Dir is the job's directory on the host (home-relative).
func (j *Job) Dir() string {
	return fmt.Sprintf("%s/%s", baseDir, j.ID)
}

// LogPath is the job's log file on the host.
func (j *Job) LogPath() string {
	return j.Dir() + "/log"
}

// Running reports whether a previously started instance of this job is still
// executing on the host.
func (j *Job) Running() bool {
	out, err := j.Host.Run(fmt.Sprintf(
		`[ -f "%[1]s/pid" ] && [ ! -f "%[1]s/exit" ] && kill -0 "$(cat "%[1]s/pid")" 2>/dev/null && echo running || true`,
		j.Dir()))
	return err == nil && strings.TrimSpace(out) == "running"
}

// Start ships the command as a script and launches it detached. The command
// travels over stdin (no shell-quoting hazards) and runs with the SSH user's
// home as working directory.
func (j *Job) Start(command string) error {
	script := "#!/bin/sh\n" + command + "\n"
	if err := j.Host.WriteFile(j.Dir()+"/cmd.sh", []byte(script), "0700"); err != nil {
		return err
	}
	// The rm must complete before backgrounding: a stale exit file from a
	// previous run would make Follow report the old result instantly.
	launch := fmt.Sprintf(
		`rm -f "%[1]s/exit" "%[1]s/pid" && : >"%[1]s/log" && { nohup sh -c 'sh "%[1]s/cmd.sh" >"%[1]s/log" 2>&1; echo $? >"%[1]s/exit"' >/dev/null 2>&1 & echo $! >"%[1]s/pid"; }`,
		j.Dir())
	if _, err := j.Host.Run(launch); err != nil {
		return fmt.Errorf("failed to start job %s: %w", j.ID, err)
	}
	return nil
}

// followCommand streams the log from a byte offset until the exit file
// appears, then gives tail a moment to flush and exits cleanly.
func (j *Job) followCommand(offset int64) string {
	return fmt.Sprintf(
		`tail -c +%d -f "%[2]s/log" & t=$!; while [ ! -e "%[2]s/exit" ]; do sleep 1; done; sleep 1; kill "$t" 2>/dev/null; wait "$t" 2>/dev/null; exit 0`,
		offset+1, j.Dir())
}

// Follow streams the job's log to w, reconnecting from the last received
// byte whenever the connection drops, until the job finishes. Returns the
// job's exit code.
func (j *Job) Follow(w io.Writer) (int, error) {
	cw := &countingWriter{w: w}
	failures := 0
	for {
		before := cw.n
		streamErr := j.Host.StreamTo(cw, j.followCommand(cw.n))

		if code, done := j.exitCode(); done {
			// Catch up on any bytes tail dropped between exit and kill.
			j.Host.StreamTo(cw, fmt.Sprintf(`tail -c +%d "%s/log" 2>/dev/null || true`, cw.n+1, j.Dir()))
			return code, nil
		}

		if streamErr != nil && !exec.Transient(streamErr) {
			return 0, streamErr
		}
		if cw.n > before {
			failures = 0
		} else {
			failures++
			if failures >= maxConsecutiveFailures {
				return 0, fmt.Errorf("lost connection to job %s on %s and could not re-attach (job may still be running; log: %s)",
					j.ID, j.Host.Server.Name, j.LogPath())
			}
		}
		sleep(reconnectDelay(failures))
	}
}

// exitCode reads the exit file; done is false while the job is running or
// the host is unreachable (the follow loop keeps retrying either way).
func (j *Job) exitCode() (code int, done bool) {
	out, err := j.Host.Run(fmt.Sprintf(`cat "%s/exit" 2>/dev/null || true`, j.Dir()))
	if err != nil {
		return 0, false
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0, false
	}
	return code, true
}

// Clean removes the job directory (best effort).
func (j *Job) Clean() {
	j.Host.Run(fmt.Sprintf(`rm -rf "%s"`, j.Dir()))
}

func reconnectDelay(failures int) time.Duration {
	delays := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}
	if failures <= 0 {
		return delays[0]
	}
	if failures > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[failures-1]
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
