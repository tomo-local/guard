package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"ai-guard-tracker/internal/engine"
	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/shellcmd"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testConnections = `apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata: {name: c}
spec:
  environments:
    - name: production
      hosts: ["*.prod.example.com"]
`

const testDBPolicy = `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata: {name: p}
spec:
  rules:
    - to: {environment: {anyOf: [production]}}
      action: DENY
`

func TestLoadPolicies_DBFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".guard/db/connections.yml"), testConnections)
	write(t, filepath.Join(root, ".guard/db/policy.yml"), testDBPolicy)
	manifest := filepath.Join(root, ".guard/manifest.yaml") // absent
	rules := filepath.Join(root, ".guard/policies")         // absent
	db := filepath.Join(root, ".guard/db")

	policies, err := loadPolicies(rules, manifest, db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 {
		t.Fatalf("expected the connections and the DB policy, got %d", len(policies))
	}

	got := engine.NewEvaluator(policies).EvaluateCommand(shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com"}})
	if got.Action != policy.ActionDeny {
		t.Fatalf("the DB policy should see the connections loaded beside it, got %+v", got)
	}
	if issues := engine.Lint(policies); len(issues) != 0 {
		t.Fatalf("expected a clean lint, got %v", issues)
	}
}

func TestLoadPolicies_YamlExtensionAndMissing(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".guard/db/connections.yaml"), testConnections)

	policies, err := loadPolicies(filepath.Join(root, "none"), filepath.Join(root, "none.yaml"), filepath.Join(root, ".guard/db"), false)
	if err != nil || len(policies) != 1 {
		t.Fatalf("got %d policies, err %v", len(policies), err)
	}

	policies, err = loadPolicies(filepath.Join(root, "none"), filepath.Join(root, "none.yaml"), filepath.Join(root, "nodb"), false)
	if err != nil || len(policies) != 0 {
		t.Fatalf("without a db directory nothing is loaded, got %d, err %v", len(policies), err)
	}
}

func TestLoadPolicies_BrokenDBFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".guard/db/policy.yml"), "kind: [broken\n")
	args := []string{filepath.Join(root, "none"), filepath.Join(root, "none.yaml"), filepath.Join(root, ".guard/db")}

	if _, err := loadPolicies(args[0], args[1], args[2], false); err == nil {
		t.Fatal("check should fail to load a broken file (and fall back to CONFIRM)")
	}
	policies, err := loadPolicies(args[0], args[1], args[2], true)
	if err != nil {
		t.Fatal(err)
	}
	if issues := engine.Lint(policies); !policy.HasErrors(issues) {
		t.Fatalf("lint should report the broken file, got %v", issues)
	}
}
