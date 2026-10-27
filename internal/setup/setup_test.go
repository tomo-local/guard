package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func testStarterFS() fstest.MapFS {
	return fstest.MapFS{
		".guard/policies/git/allow.yaml": &fstest.MapFile{
			Data: []byte("apiVersion: ai-guard/v1\nkind: CommandGatePolicy\n"),
		},
		".guard/policies/rm/deny.yaml": &fstest.MapFile{
			Data: []byte("apiVersion: ai-guard/v1\nkind: CommandGatePolicy\n"),
		},
	}
}

func TestRun_SeedsEmptyPoliciesDir(t *testing.T) {
	dir := t.TempDir()
	policiesDir := filepath.Join(dir, ".guard", "policies")
	checkLogDir := filepath.Join(dir, ".guard", "logs", "check")
	recordLogDir := filepath.Join(dir, ".guard", "logs", "record")

	res, err := Run(policiesDir, checkLogDir, recordLogDir, testStarterFS())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ExistingPolicyCount != 0 || res.SeededPolicyCount != 2 {
		t.Fatalf("Result = %+v, want ExistingPolicyCount=0 SeededPolicyCount=2", res)
	}

	for _, rel := range []string{"git/allow.yaml", "rm/deny.yaml"} {
		if _, err := os.Stat(filepath.Join(policiesDir, rel)); err != nil {
			t.Fatalf("expected %s to be written: %v", rel, err)
		}
	}
	if _, err := os.Stat(checkLogDir); err != nil {
		t.Fatalf("expected check logs dir to exist: %v", err)
	}
	if _, err := os.Stat(recordLogDir); err != nil {
		t.Fatalf("expected record logs dir to exist: %v", err)
	}
}

func TestRun_DoesNotOverwriteExistingPolicies(t *testing.T) {
	dir := t.TempDir()
	policiesDir := filepath.Join(dir, ".guard", "policies")
	checkLogDir := filepath.Join(dir, ".guard", "logs", "check")
	recordLogDir := filepath.Join(dir, ".guard", "logs", "record")

	customDir := filepath.Join(policiesDir, "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customFile := filepath.Join(customDir, "my-rule.yaml")
	customContent := []byte("apiVersion: ai-guard/v1\nkind: CommandGatePolicy\nmetadata:\n  name: x\nspec:\n  target: {module: permission, tool: whoami}\n  action: ALLOW\n  rules: [{to: {mode: read}}]\n")
	if err := os.WriteFile(customFile, customContent, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(policiesDir, checkLogDir, recordLogDir, testStarterFS())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ExistingPolicyCount != 1 || res.SeededPolicyCount != 0 {
		t.Fatalf("Result = %+v, want ExistingPolicyCount=1 SeededPolicyCount=0", res)
	}

	got, err := os.ReadFile(customFile)
	if err != nil {
		t.Fatalf("expected custom policy to still exist: %v", err)
	}
	if string(got) != string(customContent) {
		t.Fatalf("custom policy was modified")
	}
	if _, err := os.Stat(filepath.Join(policiesDir, "git", "allow.yaml")); err == nil {
		t.Fatalf("did not expect starter policies to be seeded when the dir already has policies")
	}
}

func TestWriteStarterPolicies_NeverOverwritesAnExistingFile(t *testing.T) {
	// Defense in depth: writeStarterPolicies itself must also skip a file
	// that already exists, independent of Run's own empty-dir check.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("sentinel")
	if err := os.WriteFile(filepath.Join(dir, "git", "allow.yaml"), sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := writeStarterPolicies(testStarterFS(), dir); err != nil {
		t.Fatalf("writeStarterPolicies() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "git", "allow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sentinel) {
		t.Fatalf("expected existing file to be left untouched, got %q", got)
	}
}

func TestRun_OldLogFilePathGivesClearError(t *testing.T) {
	dir := t.TempDir()
	policiesDir := filepath.Join(dir, ".guard", "policies")

	// Simulate a pre-split GUARD_LOG_PATH still pointing at the old
	// single-file layout (.guard/logs/check.jsonl) -- the log "dir" is
	// actually an existing regular file.
	oldCheckLogPath := filepath.Join(dir, ".guard", "logs", "check.jsonl")
	if err := os.MkdirAll(filepath.Dir(oldCheckLogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldCheckLogPath, []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Run(policiesDir, oldCheckLogPath, filepath.Join(dir, ".guard", "logs", "record"), testStarterFS())
	if err == nil {
		t.Fatal("expected an error when the check log dir is actually an existing file")
	}
	if !strings.Contains(err.Error(), "already exists as a file") {
		t.Fatalf("expected a hint about the old file-based layout, got: %v", err)
	}
}
