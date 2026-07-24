package remotejob

import (
	"strings"
	"testing"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/exec"
)

func testJob(id string) *Job {
	h := &exec.Host{Server: &config.Server{Name: "vps", Host: "example.com", User: "ron", Port: 22}}
	return New(h, id)
}

func TestNewSanitizesID(t *testing.T) {
	j := testJob(`build web/$(rm -rf ~)'"; v1.2`)
	if strings.ContainsAny(j.ID, ` $()'";/~`) {
		t.Errorf("ID not sanitized: %q", j.ID)
	}
	if j := testJob("build-web-abc123"); j.ID != "build-web-abc123" {
		t.Errorf("safe ID mangled: %q", j.ID)
	}
}

func TestDirIsHomeRelative(t *testing.T) {
	j := testJob("build-web-abc123")
	if j.Dir() != ".ferry/jobs/build-web-abc123" {
		t.Errorf("Dir() = %q", j.Dir())
	}
}

func TestFollowCommandResumesFromOffset(t *testing.T) {
	j := testJob("build-web-abc123")
	cmd := j.followCommand(1024)
	// tail -c is 1-based: after 1024 delivered bytes the next is byte 1025.
	if !strings.Contains(cmd, "tail -c +1025 -f") {
		t.Errorf("followCommand(1024) = %q, want tail -c +1025", cmd)
	}
	if !strings.Contains(cmd, `.ferry/jobs/build-web-abc123/exit`) {
		t.Errorf("followCommand must wait on the exit file: %q", cmd)
	}
}

func TestReconnectDelayCapsAndGrows(t *testing.T) {
	if reconnectDelay(1) >= reconnectDelay(3) {
		t.Error("delay should grow with consecutive failures")
	}
	if reconnectDelay(3) != reconnectDelay(100) {
		t.Error("delay should cap")
	}
}
