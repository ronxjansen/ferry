package buildctx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// write files under dir; paths ending in / become directories.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func tarEntries(t *testing.T, dir, dockerfile string) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteTar(&buf, dir, dockerfile); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	sort.Strings(names)
	return names
}

func TestWriteTarHonorsDockerignore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"Dockerfile":     "FROM scratch",
		".dockerignore":  ".git\n*.log\nDockerfile\n",
		"main.go":        "package main",
		"app.log":        "noise",
		".git/HEAD":      "ref",
		".git/objects/x": "blob",
		"src/util.go":    "package src",
	})

	got := tarEntries(t, dir, "Dockerfile")
	want := []string{".dockerignore", "Dockerfile", "main.go", "src/", "src/util.go"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

func TestWriteTarExceptionsReinclude(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"Dockerfile":     "FROM scratch",
		".dockerignore":  "vendor\n!vendor/keep.go\n",
		"vendor/keep.go": "package vendor",
		"vendor/drop.go": "package vendor",
	})

	got := tarEntries(t, dir, "Dockerfile")
	for _, name := range got {
		if name == "vendor/drop.go" {
			t.Errorf("vendor/drop.go should be excluded: %v", got)
		}
	}
	found := false
	for _, name := range got {
		if name == "vendor/keep.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("vendor/keep.go should be re-included by !exception: %v", got)
	}
}

func TestWriteTarNoDockerignore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"Dockerfile": "FROM scratch", "a.txt": "a"})
	got := tarEntries(t, dir, "")
	if len(got) != 2 {
		t.Fatalf("entries = %v, want Dockerfile and a.txt", got)
	}
}
