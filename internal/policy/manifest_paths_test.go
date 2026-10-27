package policy

import "testing"

func TestCommandPaths(t *testing.T) {
	const root = "/proj"
	cases := []struct {
		name string
		args []string
		cwd  string
		want []string
	}{
		{"relative", []string{"cmd/a.go"}, "/proj", []string{"cmd/a.go"}},
		{"dot-slash", []string{"./cmd/a.go"}, "/proj", []string{"cmd/a.go"}},
		{"absolute inside root", []string{"/proj/cmd/a.go"}, "/proj/sub", []string{"cmd/a.go"}},
		{"relative to a subdirectory cwd", []string{"a.go"}, "/proj/cmd", []string{"cmd/a.go"}},
		{"dot-dot back into the project", []string{"../cmd/a.go"}, "/proj/sub", []string{"cmd/a.go"}},
		{"outside the project", []string{"/etc/passwd", "../../x"}, "/proj", nil},
		{"root itself is dropped", []string{".", "/proj"}, "/proj", nil},
		{"flags are skipped", []string{"-rf", "--force", "cmd/a.go"}, "/proj", []string{"cmd/a.go"}},
		{"empty arg", []string{""}, "/proj", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := commandPaths(tc.args, root, tc.cwd)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestCommandPaths_NoLocationKnown(t *testing.T) {
	// With neither root nor cwd, relative arguments are taken as root-relative.
	got := commandPaths([]string{"cmd/a.go", "/abs/x"}, "", "")
	if len(got) != 1 || got[0] != "cmd/a.go" {
		t.Fatalf("got %v", got)
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/cmd/*", "cmd/a.go", true},
		{"cmd/*", "cmd/a.go", true}, // leading slash is optional
		{"/cmd/*", "cmd/x/a.go", false},
		{"/cmd/**", "cmd/x/a.go", true},
		{"/cmd/**", "cmd/a.go", true},
		{"/**/*.go", "cmd/x/a.go", true},
		{"/**/*.go", "a.go", true},
		{"/**/*.go", "cmd/a.txt", false},
		{"/cmd/*", "src/a.go", false},
		{"/cmd/*", "cmdx/a.go", false},
		// ancestor directories below the root are covered
		{"/cmd/*", "cmd", true},
		{"/a/b/*", "a", true},
		{"/a/b/*", "a/b", true},
		{"/a/b/*", "a/c", false},
		{"/cmd/*.go", "cmd", true},
		{"/**/*.go", "cmd", false}, // a leading wildcard covers no ancestors
		{"/cmd/x", "cmd", true},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+" vs "+tc.path, func(t *testing.T) {
			if got := matchPath(tc.pattern, tc.path); got != tc.want {
				t.Fatalf("matchPath(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestValidPattern(t *testing.T) {
	for _, ok := range []string{"/cmd/*", "/**/*.go", "src/[ab]/x?"} {
		if !validPattern(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "/", "/cmd/[", "/a/[b"} {
		if validPattern(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}
