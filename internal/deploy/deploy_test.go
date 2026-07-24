package deploy

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ronxjansen/ferry/internal/config"
	"github.com/ronxjansen/ferry/internal/plan"
)

// Regression: context creation runs before a Docker context exists, so it goes
// through plain exec instead of exec.Docker — the argv must carry the "docker"
// binary itself, or we try to execute "context" as a program.
func TestDockerCreatesContextWithDockerBinary(t *testing.T) {
	var calls [][]string
	orig := run
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		if len(args) >= 3 && args[1] == "context" && args[2] == "inspect" {
			return "", fmt.Errorf("context not found")
		}
		return "", nil
	}
	defer func() { run = orig }()

	s := &config.Server{Name: "vps", Host: "lastmark.net", User: "ron", Port: 22}
	d := &Deployer{Plan: &plan.Plan{Config: &config.Config{Name: "myapp"}}}

	if _, err := d.Docker(s); err != nil {
		t.Fatal(err)
	}

	want := []string{"docker", "context", "create", "ferry-myapp-vps",
		"--docker", "host=ssh://ron@lastmark.net:22"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], want) {
		t.Errorf("context create argv = %v, want %v", calls, want)
	}
}
