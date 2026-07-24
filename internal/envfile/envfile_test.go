package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesLaterFilesOver(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".env")
	overlay := filepath.Join(dir, ".env.web")
	os.WriteFile(base, []byte("# comment\nA=1\nB=base\n\nMALFORMED\n"), 0o644)
	os.WriteFile(overlay, []byte("B=overlay\nC=3\n"), 0o644)

	env, err := Load([]string{base, overlay}, false)
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" || env["B"] != "overlay" || env["C"] != "3" {
		t.Errorf("merge wrong: %v", env)
	}
	if _, ok := env["MALFORMED"]; ok {
		t.Error("lines without = must be skipped")
	}
}

func TestRenderDeterministicAndHash(t *testing.T) {
	env := map[string]string{"B": "2", "A": "1"}
	content := Render(env)
	if string(content) != "A=1\nB=2\n" {
		t.Errorf("render = %q", content)
	}
	if Hash(content) != Hash(Render(map[string]string{"A": "1", "B": "2"})) {
		t.Error("hash must be stable across map ordering")
	}
}

func TestWriteTempIsPrivate(t *testing.T) {
	path, err := WriteTemp([]byte("SECRET=x\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("temp env file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestRemotePath(t *testing.T) {
	if got := RemotePath("web"); got != ".ferry/apps/web/env" {
		t.Errorf("remote path = %q", got)
	}
}
