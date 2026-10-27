package policy

import (
	"strings"
	"testing"
)

const connectionsYAML = `apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata:
  name: connections
spec:
  environments:
    - name: production
      labels: [prod, sensitive]
      cli:
        hosts: ["*.prod.example.com", "10.0.0.5"]
    - name: reporting
      labels: [reports]
      cli:
        hosts: ["*.example.com"]
        databases: ["report_*"]
    - name: local
      labels: [dev]
      cli:
        hosts: [localhost, 127.0.0.1]
`

func loadConnections(t *testing.T, body string) *GuardDBConnections {
	t.Helper()
	p, err := newGuardDBConnections([]byte(body), "db/connections.yml", Deps{})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*GuardDBConnections)
}

func TestMatch(t *testing.T) {
	c := loadConnections(t, connectionsYAML)
	cases := []struct {
		host, db   string
		wantEnvs   string
		wantLabels string
	}{
		{"db.prod.example.com", "app", "production", "prod,sensitive"},
		{"DB.PROD.EXAMPLE.COM", "app", "production", "prod,sensitive"}, // case-insensitive
		{"10.0.0.5", "", "production", "prod,sensitive"},
		{"db.example.com", "report_2026", "reporting", "reports"},
		{"db.example.com", "app", "", ""}, // host matches but the database doesn't
		{"db.example.com", "", "", ""},    // the environment lists databases
		{"db.prod.example.com", "report_x", "production,reporting", "prod,sensitive,reports"},
		{"localhost", "app", "local", "dev"},
		{"", "app", "local", "dev"}, // no host = the default local connection
		{"unknown.net", "app", "", ""},
	}
	for _, tc := range cases {
		got := c.Match(tc.host, tc.db)
		if strings.Join(got.Environments, ",") != tc.wantEnvs || strings.Join(got.Labels, ",") != tc.wantLabels {
			t.Errorf("Match(%q, %q) = %+v, want envs %q labels %q", tc.host, tc.db, got, tc.wantEnvs, tc.wantLabels)
		}
	}
	if got := strings.Join(c.LabelNames(), ","); got != "prod,sensitive,reports,dev" {
		t.Errorf("LabelNames = %q", got)
	}
}

func TestDBConnections_UnknownKeyIsAnError(t *testing.T) {
	// A way of connecting that isn't supported yet must not be ignored silently.
	body := connectionsYAML + "      tableplus:\n        connection: Prod\n"
	if _, err := newGuardDBConnections([]byte(body), "c.yml", Deps{}); err == nil || !strings.Contains(err.Error(), "tableplus") {
		t.Fatalf("expected an unknown field error mentioning tableplus, got %v", err)
	}
}

func TestDBConnections_Lint(t *testing.T) {
	if issues := loadConnections(t, connectionsYAML).Lint(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	cases := []struct{ name, body, want string }{
		{"duplicate name", connectionsYAML + "    - name: local\n      cli:\n        hosts: [x]\n", "more than once"},
		{"no way of connecting", connectionsYAML + "    - name: lonely\n      labels: [x]\n", "at least one way of connecting"},
		{"cli without hosts nor databases", connectionsYAML + "    - name: lonely\n      cli: {}\n", "hosts and/or databases"},
		{"bad host pattern", strings.Replace(connectionsYAML, `"10.0.0.5"`, `"10.0.[0"`, 1), "not a valid pattern"},
		{"bad database pattern", strings.Replace(connectionsYAML, `"report_*"`, `"report_["`, 1), "not a valid pattern"},
		{"duplicate label", strings.Replace(connectionsYAML, "[prod, sensitive]", "[prod, prod]", 1), "listed more than once"},
		{"empty label", strings.Replace(connectionsYAML, "[prod, sensitive]", `[prod, ""]`, 1), "labels"},
		{"empty name", connectionsYAML + "    - name: \"\"\n      cli:\n        hosts: [x]\n", "name"},
		{"wrong kind", strings.Replace(connectionsYAML, "kind: GuardDBConnections", "kind: Nope", 1), "kind"},
		{"no environments", "apiVersion: ai-guard/v1\nkind: GuardDBConnections\nmetadata:\n  name: c\nspec:\n  environments: []\n", "environments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if issues := loadConnections(t, tc.body).Lint(); !hasErrorContaining(issues, tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, issues)
			}
		})
	}
}

const dbPolicyYAML = `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  target:
    action: DENY
  rules:
    - to:
        label: prod-ddl
        labels: [prod]
        functions: [DROP, TRUNCATE]
      action: DENY
      message:
        reason: "{cmd} changes the schema of production."
        next: "Ask the user to run it."
    - to:
        labels: [prod]
        functions: [SELECT]
      action: ALLOW
    - to:
        labels: [prod]
      action: CONFIRM
    - to:
        label: anywhere
        functions: [DELETE]
      action: CONFIRM
`

func loadDBPolicy(t *testing.T, body string, db DBEnvironments) *GuardDBPolicy {
	t.Helper()
	p, err := newGuardDBPolicy([]byte(body), "db/policy.yml", Deps{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*GuardDBPolicy)
}

func TestGuardDBPolicy_Evaluate(t *testing.T) {
	p := loadDBPolicy(t, dbPolicyYAML, loadConnections(t, connectionsYAML))
	if p.Priority() != 10 || p.Meta().Kind != KindGuardDBPolicy {
		t.Fatalf("unexpected %+v / %d", p.Meta(), p.Priority())
	}

	prod := func(sql string) []string { return []string{"-h", "db.prod.example.com", "-c", sql} }
	cases := []struct {
		name string
		cmd  []string
		tool string
		want Action
		ok   bool
	}{
		{"production DROP is denied", prod("drop table x"), "psql", ActionDeny, true},
		{"production SELECT is allowed", prod("select 1"), "psql", ActionAllow, true},
		{"production other statements hit the catch-all", prod("update t set a = 1"), "psql", ActionConfirm, true},
		{"strictest statement wins", prod("select 1; drop table x"), "psql", ActionDeny, true},
		{"a read-only script is allowed", prod("select 1; select 2"), "psql", ActionAllow, true},
		{"unreadable SQL (script file) falls to the catch-all", []string{"-h", "db.prod.example.com", "-f", "x.sql"}, "psql", ActionConfirm, true},
		{"a rule without labels applies to every connection", []string{"-h", "localhost", "-c", "delete from t"}, "psql", ActionConfirm, true},
		{"no rule for this statement on this connection", []string{"-h", "localhost", "-c", "select 1"}, "psql", "", false},
		{"mysql -e", []string{"-h", "db.prod.example.com", "-e", "drop table x"}, "mysql", ActionDeny, true},
		{"not a DB client", []string{"-h", "db.prod.example.com"}, "cat", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := p.Evaluate(cmdOf(tc.tool, tc.cmd...))
			if ok != tc.ok || (ok && v.Action != tc.want) {
				t.Fatalf("got %+v (ok=%v), want %s (ok=%v)", v, ok, tc.want, tc.ok)
			}
		})
	}

	v, _ := p.Evaluate(cmdOf("psql", prod("drop table x")...))
	if v.Label != "prod-ddl" || v.Reason != "psql -h db.prod.example.com -c drop table x changes the schema of production." || v.Source != "db/policy.yml" {
		t.Fatalf("unexpected verdict %+v", v)
	}
}

func TestGuardDBPolicy_FunctionRuleBeatsEarlierCatchAll(t *testing.T) {
	// Like a CommandGatePolicy: a rule that lists the statement wins over a
	// catch-all, wherever the catch-all sits in the file.
	body := `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: p
spec:
  rules:
    - to:
        label: all
      action: CONFIRM
    - to:
        functions: [SELECT]
      action: ALLOW
`
	p := loadDBPolicy(t, body, nil)
	if v, ok := p.Evaluate(cmdOf("psql", "-c", "select 1")); !ok || v.Action != ActionAllow {
		t.Fatalf("expected ALLOW, got %+v (ok=%v)", v, ok)
	}
	if v, ok := p.Evaluate(cmdOf("psql", "-c", "delete from t")); !ok || v.Action != ActionConfirm {
		t.Fatalf("expected CONFIRM, got %+v (ok=%v)", v, ok)
	}
}

func TestGuardDBPolicy_DefaultActionAndClients(t *testing.T) {
	body := `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: p
spec:
  target:
    clients: [mysql]
    action: CONFIRM
  rules:
    - to:
        functions: [DROP]
`
	p := loadDBPolicy(t, body, nil)
	if v, ok := p.Evaluate(cmdOf("mysql", "-e", "drop table x")); !ok || v.Action != ActionConfirm {
		t.Fatalf("expected the default CONFIRM, got %+v (ok=%v)", v, ok)
	}
	if _, ok := p.Evaluate(cmdOf("psql", "-c", "drop table x")); ok {
		t.Fatal("psql is not covered when clients: [mysql]")
	}
}

func TestGuardDBPolicy_LabelRulesNeedConnections(t *testing.T) {
	p := loadDBPolicy(t, dbPolicyYAML, nil)
	// No connections: the labelled rules can't match, the unlabelled one still does.
	if _, ok := p.Evaluate(cmdOf("psql", "-h", "db.prod.example.com", "-c", "drop table x")); ok {
		t.Fatal("a rule naming labels can't match without connections.yml")
	}
	if v, ok := p.Evaluate(cmdOf("psql", "-c", "delete from t")); !ok || v.Action != ActionConfirm {
		t.Fatalf("expected the unlabelled rule to apply, got %+v (ok=%v)", v, ok)
	}
}

func TestGuardDBPolicy_Lint(t *testing.T) {
	if issues := loadDBPolicy(t, dbPolicyYAML, nil).Lint(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	cases := []struct{ name, body, want string }{
		{"unknown client", strings.Replace(dbPolicyYAML, "  target:\n    action: DENY", "  target:\n    clients: [sqlite3]\n    action: DENY", 1), "unknown client"},
		{"bad action", strings.Replace(dbPolicyYAML, "action: DENY\n      message", "action: MAYBE\n      message", 1), "action"},
		{"empty function", strings.Replace(dbPolicyYAML, "[DROP, TRUNCATE]", `[DROP, ""]`, 1), "functions"},
		{"empty label", strings.Replace(dbPolicyYAML, "labels: [prod]\n        functions: [DROP", `labels: [""]`+"\n        functions: [DROP", 1), "labels"},
		{"no rules", "apiVersion: ai-guard/v1\nkind: GuardDBPolicy\nmetadata:\n  name: p\nspec:\n  rules: []\n", "rules"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if issues := loadDBPolicy(t, tc.body, nil).Lint(); !hasErrorContaining(issues, tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, issues)
			}
		})
	}

	twoCatchAlls := strings.Replace(dbPolicyYAML, "    - to:\n        label: anywhere\n        functions: [DELETE]", "    - to:\n        label: a\n    - to:\n        label: b", 1)
	if issues := loadDBPolicy(t, twoCatchAlls, nil).Lint(); !hasWarningContaining(issues, "second rule for every statement") {
		t.Fatalf("expected a second catch-all warning, got %v", issues)
	}
}

func TestDBPolicyRefs(t *testing.T) {
	conns := loadConnections(t, connectionsYAML)
	good := loadDBPolicy(t, dbPolicyYAML, conns)
	if issues := lintDBPolicyRefs([]Policy{conns, good}); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	typo := loadDBPolicy(t, strings.Replace(dbPolicyYAML, "labels: [prod]\n      action: CONFIRM", "labels: [prd]\n      action: CONFIRM", 1), conns)
	if issues := lintDBPolicyRefs([]Policy{conns, typo}); !hasErrorContaining(issues, `unknown label "prd"`) {
		t.Fatalf("expected an unknown label error, got %v", issues)
	}

	if issues := lintDBPolicyRefs([]Policy{good}); !hasErrorContaining(issues, "no GuardDBConnections") {
		t.Fatalf("expected an error without connections, got %v", issues)
	}
}

func TestDBSets_OnlyOneFileEach(t *testing.T) {
	a, b := loadConnections(t, connectionsYAML), loadConnections(t, connectionsYAML)
	a.SourcePath, b.SourcePath = "a.yml", "b.yml"
	if issues := lintDBConnectionsSet([]Policy{a, b}); len(issues) != 1 || issues[0].Path != "b.yml" {
		t.Fatalf("got %v", issues)
	}
	if issues := lintDBConnectionsSet([]Policy{a}); len(issues) != 0 {
		t.Fatalf("got %v", issues)
	}
	p1, p2 := loadDBPolicy(t, dbPolicyYAML, nil), loadDBPolicy(t, dbPolicyYAML, nil)
	p1.SourcePath, p2.SourcePath = "a.yml", "b.yml"
	if issues := lintDBPolicySet([]Policy{p1, p2}); len(issues) != 1 || issues[0].Path != "b.yml" {
		t.Fatalf("got %v", issues)
	}
}

func TestDBKindsDispatchedByLoadFile(t *testing.T) {
	dir := t.TempDir()
	cp := writeFile(t, dir, "connections.yml", connectionsYAML)
	pp := writeFile(t, dir, "policy.yml", dbPolicyYAML)
	c, err := LoadFile(cp, Deps{})
	if err != nil || c.Meta().Kind != KindGuardDBConnections {
		t.Fatalf("got %v, %v", c, err)
	}
	p, err := LoadFile(pp, Deps{})
	if err != nil || p.Meta().Kind != KindGuardDBPolicy {
		t.Fatalf("got %v, %v", p, err)
	}
}
