package exec

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ronxjansen/ferry/internal/config"
)

func TestTransient(t *testing.T) {
	transient := []string{
		"ssh failed: exit status 255\nclient_loop: send disconnect: Broken pipe",
		"error during connect: Get \"http://docker.example.com\": EOF",
		"rpc error: code = Unavailable desc = error reading from server",
		"read tcp 1.2.3.4:22: connection reset by peer",
		"Read from remote host lastmark.net: Operation timed out",
		"kex_exchange_identification: Connection closed by remote host",
	}
	for _, msg := range transient {
		if !Transient(errors.New(msg)) {
			t.Errorf("Transient(%q) = false, want true", msg)
		}
	}

	permanent := []string{
		"exit status 1\nERROR: failed to solve: process \"/bin/sh -c go build\" did not complete successfully",
		"exit status 125\ndocker: Error response from daemon: Conflict. The container name is already in use",
		"Permission denied (publickey)",
	}
	for _, msg := range permanent {
		if Transient(errors.New(msg)) {
			t.Errorf("Transient(%q) = true, want false", msg)
		}
	}
	if Transient(nil) {
		t.Error("Transient(nil) = true, want false")
	}
}

func TestRunRetryRetriesTransientOnly(t *testing.T) {
	origSleep := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = origSleep }()

	calls := 0
	out, err := RunRetry(func() (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("connection reset by peer")
		}
		return "ok", nil
	})
	if err != nil || out != "ok" || calls != 3 {
		t.Errorf("transient retry: out=%q err=%v calls=%d, want ok/nil/3", out, err, calls)
	}

	calls = 0
	_, err = RunRetry(func() (string, error) {
		calls++
		return "", errors.New("exit status 1\nno such image")
	})
	if err == nil || calls != 1 {
		t.Errorf("permanent error: err=%v calls=%d, want error after 1 call", err, calls)
	}

	calls = 0
	_, err = RunRetry(func() (string, error) {
		calls++
		return "", errors.New("broken pipe")
	})
	if err == nil || calls != len(retryDelays)+1 {
		t.Errorf("exhausted retries: err=%v calls=%d, want error after %d calls", err, calls, len(retryDelays)+1)
	}
}

func TestSSHArgsCarryKeepalives(t *testing.T) {
	s := &config.Server{Name: "vps", Host: "example.com", User: "ron", Port: 2222}
	args := sshArgs(s, "-t")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-o ServerAliveInterval=30",
		"-o ServerAliveCountMax=10",
		"-o BatchMode=yes",
		"-o ConnectTimeout=10",
		"-p 2222",
		"-t",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("sshArgs missing %q in %q", want, joined)
		}
	}
	if args[len(args)-1] != "ron@example.com" {
		t.Errorf("destination = %q, want ron@example.com", args[len(args)-1])
	}
	if fmt.Sprint(args[0]) != "ssh" {
		t.Errorf("argv[0] = %q, want ssh", args[0])
	}
}
