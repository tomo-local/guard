package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/shellcmd"
)

const dbConnectionsYAML = `apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata: {name: connections}
spec:
  environments:
    - name: production
      hosts: ["*.prod.example.com"]
    - name: local
      hosts: [localhost]
`

const dbPolicyYAML = `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata: {name: db-policy}
spec:
  rules:
    - to: {environment: {anyOf: [production]}}
      action: DENY
    - to: {environment: {noneOf: [production, local]}}
      action: CONFIRM
`

func loadDB(t *testing.T, connections, pol string) []policy.Policy {
	t.Helper()
	dir := t.TempDir()
	cp, pp := filepath.Join(dir, "connections.yml"), filepath.Join(dir, "policy.yml")
	for path, body := range map[string]string{cp: connections, pp: pol} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Connections are loaded first so the policy can be handed them.
	c, err := policy.LoadFile(cp, policy.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.LoadFile(pp, policy.Deps{DB: c.(policy.DBEnvironments)})
	if err != nil {
		t.Fatal(err)
	}
	return []policy.Policy{p, c}
}

func TestEvaluateCommand_DBEnvironments(t *testing.T) {
	e := NewEvaluator(loadDB(t, dbConnectionsYAML, dbPolicyYAML))

	cases := []struct {
		name  string
		cmd   shellcmd.SimpleCommand
		want  policy.Action
		deflt bool
	}{
		{"production is denied", shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com", "app"}}, policy.ActionDeny, false},
		{"unknown host asks", shellcmd.SimpleCommand{Tool: "mysql", Args: []string{"-h", "staging.other.net"}}, policy.ActionConfirm, false},
		{"local falls to the default", shellcmd.SimpleCommand{Tool: "psql", Args: []string{"app"}}, policy.DefaultAction, true},
		{"other tools are untouched", shellcmd.SimpleCommand{Tool: "cat", Args: []string{"x"}}, policy.DefaultAction, true},
	}
	for _, tc := range cases {
		got := e.EvaluateCommand(tc.cmd)
		if got.Action != tc.want || got.Default != tc.deflt {
			t.Errorf("%s: got %+v, want %s (default=%v)", tc.name, got, tc.want, tc.deflt)
		}
	}
}

func TestEvaluateCommand_ToolPolicyBeatsDBPolicy(t *testing.T) {
	psqlAllow := policy.NewCommandGate("psql/allow.yaml", policy.Spec{
		Target: policy.Target{Module: policy.ModulePermission, Tool: "psql"},
		Action: policy.ActionAllow,
		Rules:  []policy.Rule{{To: policy.To{}}},
	}, policy.Deps{})
	e := NewEvaluator(append(loadDB(t, dbConnectionsYAML, dbPolicyYAML), psqlAllow))

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com"}})
	if got.Action != policy.ActionAllow {
		t.Fatalf("a per-tool policy is more specific and should decide, got %+v", got)
	}
}

func TestLint_DBReferencesAcrossFiles(t *testing.T) {
	clean := loadDB(t, dbConnectionsYAML, dbPolicyYAML)
	if issues := Lint(clean); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	typo := loadDB(t, dbConnectionsYAML, strings.Replace(dbPolicyYAML, "[production, local]", "[production, locl]", 1))
	var found bool
	for _, i := range Lint(typo) {
		if strings.Contains(i.Message, `unknown environment "locl"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Lint to catch the unknown environment through the cross-kind hook, got %v", Lint(typo))
	}
}
