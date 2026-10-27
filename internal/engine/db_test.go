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
metadata:
  name: connections
spec:
  environments:
    - name: production
      labels: [prod]
      cli:
        hosts: ["*.prod.example.com"]
    - name: local
      labels: [dev]
      cli:
        hosts: [localhost]
`

const dbPolicyYAML = `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  rules:
    - to:
        labels: [prod]
        functions: [DROP, TRUNCATE]
      action: DENY
    - to:
        labels: [prod]
        functions: [SELECT]
      action: ALLOW
    - to:
        labels: [prod]
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

func TestEvaluateCommand_DBLabelsAndStatements(t *testing.T) {
	e := NewEvaluator(loadDB(t, dbConnectionsYAML, dbPolicyYAML))

	cases := []struct {
		name  string
		cmd   shellcmd.SimpleCommand
		want  policy.Action
		deflt bool
	}{
		{"production DROP is denied", shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com", "-c", "drop table x"}}, policy.ActionDeny, false},
		{"production SELECT is allowed", shellcmd.SimpleCommand{Tool: "mysql", Args: []string{"-h", "db.prod.example.com", "-e", "select 1"}}, policy.ActionAllow, false},
		{"production script file asks", shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com", "-f", "x.sql"}}, policy.ActionConfirm, false},
		{"local has no rule", shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-c", "drop table x"}}, policy.DefaultAction, true},
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

	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com", "-c", "drop table x"}})
	if got.Action != policy.ActionAllow {
		t.Fatalf("a per-tool policy is more specific and should decide, got %+v", got)
	}
}

func TestLint_DBReferencesAcrossFiles(t *testing.T) {
	clean := loadDB(t, dbConnectionsYAML, dbPolicyYAML)
	if issues := Lint(clean); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	typo := loadDB(t, dbConnectionsYAML, strings.Replace(dbPolicyYAML, "labels: [prod]\n      action: CONFIRM", "labels: [prd]\n      action: CONFIRM", 1))
	var found bool
	for _, i := range Lint(typo) {
		if strings.Contains(i.Message, `unknown label "prd"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Lint to catch the unknown label through the cross-kind hook, got %v", Lint(typo))
	}
}

// With the manifest's sqlGate (no environment involved) as the fallback behind
// the DB policy.
func TestEvaluateCommand_SQLGateIsTheFallbackBehindTheDBPolicy(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "manifest.yaml")
	body := `apiVersion: ai-guard/v1
kind: GuardManifest
metadata:
  name: m
spec:
  sqlGate:
    rules:
      - to:
          functions: [DROP, TRUNCATE]
        action: DENY
`
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := policy.LoadFile(mp, policy.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEvaluator(append(loadDB(t, dbConnectionsYAML, dbPolicyYAML), m))

	// The DB policy has a rule for production: it decides (SELECT -> ALLOW).
	got := e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-h", "db.prod.example.com", "-c", "select 1"}})
	if got.Action != policy.ActionAllow {
		t.Fatalf("expected the DB policy to decide, got %+v", got)
	}
	// Local: no DB policy rule, so the manifest's environment-independent rule decides.
	got = e.EvaluateCommand(shellcmd.SimpleCommand{Tool: "psql", Args: []string{"-c", "drop table x"}})
	if got.Action != policy.ActionDeny || got.Default {
		t.Fatalf("expected the sqlGate to decide, got %+v", got)
	}
}

// Through the shell parser, which keeps the quotes of the command line.
func TestEvaluateAll_DBFromACommandLine(t *testing.T) {
	e := NewEvaluator(loadDB(t, dbConnectionsYAML, dbPolicyYAML))

	cases := map[string]policy.Action{
		`psql -h db.prod.example.com -c 'drop table x'`:            policy.ActionDeny,
		`psql -h "db.prod.example.com" -c "select 1"`:              policy.ActionAllow,
		`psql -h db.prod.example.com -c 'select 1; drop table x'`:  policy.ActionDeny,
		`mysql --host=db.prod.example.com -e 'update t set a = 1'`: policy.ActionConfirm,
		`cd /tmp && psql -h db.prod.example.com -c 'drop table x'`: policy.ActionDeny,
	}
	for raw, want := range cases {
		if got, _ := e.EvaluateAll(raw); got.Action != want {
			t.Errorf("%s: got %s, want %s", raw, got.Action, want)
		}
	}
}
