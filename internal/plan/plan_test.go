package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronxjansen/ferry/internal/config"
)

const composeYAML = `
services:
  web:
    build: .
    expose: ["3000"]
    depends_on:
      migrate:
        condition: service_completed_successfully
      db:
        condition: service_healthy
  worker:
    build: .
    command: ["bundle", "exec", "sidekiq"]
    depends_on: [db]
  db:
    image: postgres:16
    volumes: [dbdata:/var/lib/postgresql/data]
    ports: ["127.0.0.1:5432:5432"]
  migrate:
    build: .
    depends_on: [db]
  mailhog:
    image: mailhog/mailhog
volumes:
  dbdata: {}
`

const ferryYAML = `
name: myapp
servers:
  vps-1: 165.232.100.10
  vps-2: 165.232.100.11
services:
  web:
    servers: [vps-1]
    domain: example.com
  worker:
    servers: [vps-2]
  db:
    servers: [vps-1]
    stateful: true
  migrate:
    servers: [vps-1]
    job: true
`

func loadTestPlan(t *testing.T) *Plan {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ferry.yaml"), []byte(ferryYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnlistedComposeServicesAreIgnored(t *testing.T) {
	p := loadTestPlan(t)
	if len(p.Targets) != 4 {
		t.Fatalf("want 4 targets, got %d", len(p.Targets))
	}
	if _, err := p.Target("mailhog"); err == nil {
		t.Error("mailhog is local-only tooling and must not be deployable")
	}
}

func TestDependencyOrder(t *testing.T) {
	p := loadTestPlan(t)
	pos := map[string]int{}
	for i, tg := range p.Targets {
		pos[tg.Name] = i
	}
	if !(pos["db"] < pos["migrate"] && pos["migrate"] < pos["web"] && pos["db"] < pos["worker"]) {
		names := make([]string, len(p.Targets))
		for i, tg := range p.Targets {
			names[i] = tg.Name
		}
		t.Errorf("bad deploy order: %v", names)
	}
}

func TestTargetResolution(t *testing.T) {
	p := loadTestPlan(t)

	web, _ := p.Target("web")
	if !web.Proxied() {
		t.Error("web has a domain and must be proxied")
	}
	if web.Port() != 3000 {
		t.Errorf("web port from compose expose = %d, want 3000", web.Port())
	}
	if got := web.ImageRef("abc123"); got != "myapp/web:abc123" {
		t.Errorf("web image = %q", got)
	}
	if got := web.ContainerName("abc123"); got != "web-abc123" {
		t.Errorf("container name = %q", got)
	}
	if got := web.ProxyService(); got != "myapp-web" {
		t.Errorf("proxy service = %q, want project-prefixed myapp-web (bare names collide across projects on a shared server)", got)
	}
	if web.BuildMethod() != "remote" {
		t.Errorf("web build method = %q, want remote (compose has build:)", web.BuildMethod())
	}

	db, _ := p.Target("db")
	if db.Proxied() {
		t.Error("db has no domain/port overlay and must not be proxied")
	}
	if db.BuildMethod() != "pull" {
		t.Errorf("db build method = %q, want pull (image-only)", db.BuildMethod())
	}
	if got := db.ImageRef("abc123"); got != "postgres:16" {
		t.Errorf("image-only services deploy the compose image verbatim, got %q", got)
	}
	if !db.Stateful() {
		t.Error("db is marked stateful in the overlay")
	}
	if got := db.ContainerName("abc123"); got != "myapp-db" {
		t.Errorf("stateful container name = %q, want stable project-prefixed myapp-db (a versioned name would bounce the db every deploy)", got)
	}

	migrate, _ := p.Target("migrate")
	if !migrate.Overlay.Job {
		t.Error("migrate must be a job")
	}
}

func TestSslipFallbackDomain(t *testing.T) {
	p := loadTestPlan(t)
	web, _ := p.Target("web")
	web.Overlay.Domain = ""
	web.Overlay.Port = 3000
	got := web.DomainsOn(web.Servers[0])
	if len(got) != 1 || got[0] != "web.165-232-100-10.sslip.io" {
		t.Errorf("sslip fallback = %v", got)
	}
}

func TestCrossServerEnv(t *testing.T) {
	p := loadTestPlan(t)
	worker, _ := p.Target("worker")
	vps2 := worker.Servers[0]
	env := p.CrossServerEnv(worker, vps2)
	if env["FERRY_SERVICE_DB_HOST"] != "165.232.100.10" {
		t.Errorf("worker on vps-2 needs db's host injected, got %v", env)
	}
	web, _ := p.Target("web")
	envWeb := p.CrossServerEnv(web, web.Servers[0])
	if _, ok := envWeb["FERRY_SERVICE_DB_HOST"]; ok {
		t.Error("web and db share vps-1; DNS works, no injection wanted")
	}
}

func TestCrossServerWarnings(t *testing.T) {
	p := loadTestPlan(t)
	warnings := p.Warnings()
	found := false
	for _, w := range warnings {
		if strings.Contains(w, `"worker" depends on "db"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("worker→db crosses servers and must warn; got %v", warnings)
	}
}

func TestStatefulRejectsBuild(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML), 0o644)
	os.WriteFile(filepath.Join(dir, "ferry.yaml"), []byte(`
name: myapp
servers: {vps-1: 1.2.3.4}
services:
  web: {servers: [vps-1], stateful: true}
`), 0o644)
	cfg, err := config.Load(filepath.Join(dir, "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(cfg)
	if err == nil || !strings.Contains(err.Error(), "stateful") {
		t.Errorf("stateful + compose build: must be rejected, got %v", err)
	}
}

func TestStatefulVolumeWarning(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML), 0o644)
	os.WriteFile(filepath.Join(dir, "ferry.yaml"), []byte(`
name: myapp
servers: {vps-1: 1.2.3.4}
services:
  mailhog: {servers: [vps-1], stateful: true}
`), 0o644)
	cfg, err := config.Load(filepath.Join(dir, "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range p.Warnings() {
		if strings.Contains(w, "mounts no volumes") {
			found = true
		}
	}
	if !found {
		t.Errorf("stateful service without volumes must warn about data loss; got %v", p.Warnings())
	}

	// db mounts a named volume: no warning wanted.
	full := loadTestPlan(t)
	for _, w := range full.Warnings() {
		if strings.Contains(w, "mounts no volumes") {
			t.Errorf("db has a volume and must not warn: %v", w)
		}
	}
}

func TestUnknownServiceInOverlay(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML), 0o644)
	os.WriteFile(filepath.Join(dir, "ferry.yaml"), []byte(`
name: myapp
servers: {vps-1: 1.2.3.4}
services:
  api: {servers: [vps-1]}
`), 0o644)
	cfg, err := config.Load(filepath.Join(dir, "ferry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(cfg)
	if err == nil || !strings.Contains(err.Error(), "does not exist in the compose project") {
		t.Errorf("want compose-mismatch error, got %v", err)
	}
}
