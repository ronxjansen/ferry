package lock

import "testing"

func TestDirForScopesPerProject(t *testing.T) {
	if a, b := dirFor("dmnso"), dirFor("lastmark"); a == b {
		t.Fatalf("distinct projects share a lock dir: %s", a)
	}
	if got, want := dirFor("dmnso"), ".ferry/locks/dmnso"; got != want {
		t.Errorf("dirFor(dmnso) = %q, want %q", got, want)
	}
}

func TestDirForSanitizes(t *testing.T) {
	for _, tc := range []struct{ project, want string }{
		{"My App", ".ferry/locks/my-app"},
		{"../../etc", ".ferry/locks/etc"},
		{"a/b", ".ferry/locks/a-b"},
		{"", ".ferry/locks/default"},
		{"///", ".ferry/locks/default"},
	} {
		if got := dirFor(tc.project); got != tc.want {
			t.Errorf("dirFor(%q) = %q, want %q", tc.project, got, tc.want)
		}
	}
}
