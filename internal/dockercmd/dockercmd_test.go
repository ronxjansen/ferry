package dockercmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/ronxjansen/ferry/internal/config"
)

func argvString(args []string) string { return strings.Join(args, " ") }

func TestContextCreate(t *testing.T) {
	got := ContextCreate("ferry-myapp-vps-1", &config.Server{Host: "1.2.3.4", User: "deploy", Port: 2222})
	want := "context create ferry-myapp-vps-1 --docker host=ssh://deploy@1.2.3.4:2222"
	if argvString(got) != want {
		t.Errorf("got %q, want %q", argvString(got), want)
	}
}

func TestRunBuildsFullArgv(t *testing.T) {
	svc := types.ServiceConfig{
		Command: types.ShellCommand{"bundle", "exec", "puma"},
		Volumes: []types.ServiceVolumeConfig{{Type: "volume", Source: "data", Target: "/data"}},
		Labels:  types.Labels{"app.custom": "yes"},
	}
	args := Run(svc, "myapp/web:abc123", RunOpts{
		Name: "web-abc123", Project: "myapp", Service: "web", Version: "abc123",
		Network: "ferry", EnvFile: "/tmp/env", Detach: true,
	})
	s := argvString(args)

	for _, want := range []string{
		"run --detach --name web-abc123",
		"--network ferry --network-alias web",
		"--restart unless-stopped",
		"--env-file /tmp/env",
		"--label ferry.project=myapp",
		"--label ferry.service=web",
		"--label ferry.version=abc123",
		"--label app.custom=yes",
		"--volume myapp_data:/data",
		"myapp/web:abc123 bundle exec puma",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("argv missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "ferry.preview") {
		t.Error("non-preview run must not carry a preview label")
	}
}

func TestRunJob(t *testing.T) {
	args := Run(types.ServiceConfig{}, "img", RunOpts{Name: "migrate-abc", Project: "p", Service: "migrate", Version: "abc", Rm: true})
	s := argvString(args)
	if !strings.Contains(s, "--rm") {
		t.Error("jobs need --rm")
	}
	if strings.Contains(s, "--restart") {
		t.Error("jobs must not restart")
	}
}

func TestRunPreviewVolumeNamespace(t *testing.T) {
	svc := types.ServiceConfig{Volumes: []types.ServiceVolumeConfig{{Type: "volume", Source: "dbdata", Target: "/db"}}}
	args := Run(svc, "postgres:16", RunOpts{
		Name: "db-abc-preview", Project: "myapp", VolumeProject: "myapp-abc",
		Service: "db", Version: "abc", Preview: "abc", Network: "myapp-abc", Detach: true,
	})
	s := argvString(args)
	if !strings.Contains(s, "--volume myapp-abc_dbdata:/db") {
		t.Errorf("isolate volumes must be preview-namespaced:\n%s", s)
	}
	if !strings.Contains(s, "--label ferry.preview=abc") {
		t.Errorf("preview label missing:\n%s", s)
	}
}

func TestRunEntrypointSplit(t *testing.T) {
	svc := types.ServiceConfig{
		Entrypoint: types.ShellCommand{"/entry.sh", "--flag"},
		Command:    types.ShellCommand{"serve"},
	}
	args := Run(svc, "img", RunOpts{Name: "x", Project: "p", Service: "x", Version: "v"})
	s := argvString(args)
	if !strings.Contains(s, "--entrypoint /entry.sh") || !strings.HasSuffix(s, "img --flag serve") {
		t.Errorf("entrypoint handling wrong:\n%s", s)
	}
}

func TestBuild(t *testing.T) {
	target := "production"
	val := "1"
	build := &types.BuildConfig{
		Context:    "./api",
		Dockerfile: "Dockerfile.prod",
		Target:     target,
		Args:       types.MappingWithEquals{"VERSION": &val},
	}
	got := argvString(Build(build, "myapp/api:abc"))
	want := "build --tag myapp/api:abc --file Dockerfile.prod --target production --build-arg VERSION=1 ./api"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildScript(t *testing.T) {
	val := "two words $HOME"
	build := &types.BuildConfig{
		Context:    "./api",
		Dockerfile: "Dockerfile.prod",
		Args:       types.MappingWithEquals{"VERSION": &val},
	}
	got := BuildScript(build, "myapp/api:abc", ".ferry/jobs/build-api-abc/ctx")
	want := `cd '.ferry/jobs/build-api-abc/ctx' && exec docker 'build' '--tag' 'myapp/api:abc' '--file' 'Dockerfile.prod' '--build-arg' 'VERSION=two words $HOME' '.'`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	if got := ShellQuote(`it's a "test" $x`); got != `'it'\''s a "test" $x'` {
		t.Errorf("ShellQuote = %q", got)
	}
}

func TestProxyDeploy(t *testing.T) {
	args := ProxyDeploy(ProxyDeployOpts{
		Service:       "web",
		Target:        "web-abc123:3000",
		Hosts:         []string{"example.com", "www.example.com"},
		Health:        config.Health{Path: "/up", Interval: "5s"},
		DeployTimeout: "5m0s",
		DrainTimeout:  "30s",
		Passthrough:   map[string]string{"forward_headers": "true", "response_timeout": "30s"},
	})
	s := argvString(args)
	if !strings.HasPrefix(s, "exec kamal-proxy kamal-proxy deploy web --target web-abc123:3000 --tls") {
		t.Errorf("bad prefix: %s", s)
	}
	for _, want := range []string{
		"--host example.com", "--host www.example.com",
		"--health-check-path /up", "--health-check-interval 5s",
		"--deploy-timeout 5m0s", "--drain-timeout 30s",
		"--forward-headers", "--response-timeout 30s",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("argv missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "--forward-headers true") {
		t.Error("boolean passthrough must be a bare flag")
	}
}

func TestProxyLifecycle(t *testing.T) {
	if got := argvString(ProxyRemove("web-abc")); got != "exec kamal-proxy kamal-proxy remove web-abc" {
		t.Errorf("remove: %q", got)
	}
	if got := argvString(ProxyStop("web", "back soon")); got != "exec kamal-proxy kamal-proxy stop web --message back soon" {
		t.Errorf("stop: %q", got)
	}
	if got := argvString(ProxyResume("web")); got != "exec kamal-proxy kamal-proxy resume web" {
		t.Errorf("resume: %q", got)
	}
}

func TestProxyRunPublishesConfiguredPorts(t *testing.T) {
	args := ProxyRun(config.Proxy{Image: "basecamp/kamal-proxy:v0.9.0", Network: "ferry", HTTPPort: 8080, HTTPSPort: 8443})
	if !slices.Contains(args, "8080:80") || !slices.Contains(args, "8443:443") {
		t.Errorf("custom ports not honored: %v", args)
	}
}
