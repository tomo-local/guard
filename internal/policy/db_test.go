package policy

import (
	"strings"
	"testing"
)

const connectionsYAML = `apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata: {name: connections}
spec:
  environments:
    - name: production
      hosts: ["*.prod.example.com", "10.0.0.5"]
    - name: reporting
      hosts: ["*.example.com"]
      databases: ["report_*"]
    - name: local
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

func TestEnvironments(t *testing.T) {
	c := loadConnections(t, connectionsYAML)
	cases := []struct {
		host, db string
		want     []string
	}{
		{"db.prod.example.com", "app", []string{"production"}},
		{"DB.PROD.EXAMPLE.COM", "app", []string{"production"}}, // case-insensitive
		{"10.0.0.5", "", []string{"production"}},
		{"db.example.com", "report_2026", []string{"reporting"}},
		{"db.example.com", "app", nil},                                           // host matches but database doesn't
		{"db.example.com", "", nil},                                              // database required by the environment
		{"db.prod.example.com", "report_x", []string{"production", "reporting"}}, // several at once
		{"localhost", "app", []string{"local"}},
		{"", "app", []string{"local"}}, // no host = the default local connection
		{"unknown.net", "app", nil},
	}
	for _, tc := range cases {
		got := c.Environments(tc.host, tc.db)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("Environments(%q, %q) = %v, want %v", tc.host, tc.db, got, tc.want)
		}
	}
}

func TestDBConnections_Lint(t *testing.T) {
	if issues := loadConnections(t, connectionsYAML).Lint(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	cases := []struct{ name, body, want string }{
		{"duplicate name", connectionsYAML + "    - name: local\n      hosts: [x]\n", "more than once"},
		{"no hosts nor databases", connectionsYAML + "    - name: lonely\n", "hosts and/or databases"},
		{"bad host pattern", strings.Replace(connectionsYAML, `"10.0.0.5"`, `"10.0.[0"`, 1), "not a valid pattern"},
		{"bad database pattern", strings.Replace(connectionsYAML, `"report_*"`, `"report_["`, 1), "not a valid pattern"},
		{"empty name", connectionsYAML + "    - name: \"\"\n      hosts: [x]\n", "name"},
		{"wrong kind", strings.Replace(connectionsYAML, "kind: GuardDBConnections", "kind: Nope", 1), "kind"},
		{"no environments", "apiVersion: ai-guard/v1\nkind: GuardDBConnections\nmetadata: {name: c}\nspec: {environments: []}\n", "environments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := loadConnections(t, tc.body).Lint()
			if !hasErrorContaining(issues, tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, issues)
			}
		})
	}
}

const dbPolicyYAML = `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata: {name: db-policy}
spec:
  target:
    action: CONFIRM
  rules:
    - to:
        label: prod
        environment: {anyOf: [production]}
      action: DENY
      message: {reason: "{cmd} connects to production.", next: "Ask the user to run it."}
    - to:
        environment: {noneOf: [production, local]}
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

	v, ok := p.Evaluate(cmdOf("psql", "-h", "db.prod.example.com", "app"))
	if !ok || v.Action != ActionDeny || v.Label != "prod" || v.Reason != "psql -h db.prod.example.com app connects to production." || v.Source != "db/policy.yml" {
		t.Fatalf("unexpected verdict %+v (ok=%v)", v, ok)
	}

	// Host in no environment: the noneOf rule applies, with the default
	// action and a generic message.
	v, ok = p.Evaluate(cmdOf("mysql", "-h", "staging.other.net", "app"))
	if !ok || v.Action != ActionConfirm || !strings.Contains(v.Reason, "staging.other.net") || v.Next == "" {
		t.Fatalf("unexpected verdict %+v (ok=%v)", v, ok)
	}

	// Local connections match neither rule.
	if _, ok := p.Evaluate(cmdOf("psql", "app")); ok {
		t.Fatal("expected no opinion for a local connection")
	}
	// Not a DB client.
	if _, ok := p.Evaluate(cmdOf("cat", "-h", "db.prod.example.com")); ok {
		t.Fatal("expected no opinion for other tools")
	}
}

func TestGuardDBPolicy_StrictestRuleAndDefaults(t *testing.T) {
	both := `apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata: {name: p}
spec:
  rules:
    - to: {environment: {anyOf: [reporting]}}
      action: CONFIRM
    - to: {environment: {anyOf: [production]}}
`
	p := loadDBPolicy(t, both, loadConnections(t, connectionsYAML))
	// production + reporting at once: the rule without action defaults to DENY, which is stricter.
	if v, ok := p.Evaluate(cmdOf("psql", "-h", "db.prod.example.com", "report_x")); !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY, got %+v (ok=%v)", v, ok)
	}
}

func TestGuardDBPolicy_Clients(t *testing.T) {
	onlyMySQL := strings.Replace(dbPolicyYAML, "  target:\n    action: CONFIRM", "  target:\n    clients: [mysql]\n    action: CONFIRM", 1)
	p := loadDBPolicy(t, onlyMySQL, loadConnections(t, connectionsYAML))
	if _, ok := p.Evaluate(cmdOf("psql", "-h", "db.prod.example.com")); ok {
		t.Fatal("psql is not covered when clients: [mysql]")
	}
	if _, ok := p.Evaluate(cmdOf("mysql", "-h", "db.prod.example.com")); !ok {
		t.Fatal("mysql should be covered")
	}
}

func TestGuardDBPolicy_NoConnectionsNeverMatches(t *testing.T) {
	p := loadDBPolicy(t, dbPolicyYAML, nil)
	if _, ok := p.Evaluate(cmdOf("psql", "-h", "db.prod.example.com")); ok {
		t.Fatal("without defined environments no rule can match")
	}
}

func TestGuardDBPolicy_Lint(t *testing.T) {
	if issues := loadDBPolicy(t, dbPolicyYAML, nil).Lint(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	cases := []struct{ name, body, want string }{
		{"unknown client", strings.Replace(dbPolicyYAML, "  target:\n    action: CONFIRM", "  target:\n    clients: [sqlite3]\n    action: CONFIRM", 1), "unknown client"},
		{"bad action", strings.Replace(dbPolicyYAML, "action: DENY", "action: MAYBE", 1), "action"},
		{"empty environment", strings.Replace(dbPolicyYAML, "environment: {anyOf: [production]}", "environment: {}", 1), "environment.anyOf"},
		{"same name both", strings.Replace(dbPolicyYAML, "{anyOf: [production]}", "{anyOf: [production], noneOf: [production]}", 1), "can never match"},
		{"missing environment", strings.Replace(dbPolicyYAML, "        environment: {anyOf: [production]}\n", "", 1), "environment"},
		{"no rules", "apiVersion: ai-guard/v1\nkind: GuardDBPolicy\nmetadata: {name: p}\nspec: {rules: []}\n", "rules"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if issues := loadDBPolicy(t, tc.body, nil).Lint(); !hasErrorContaining(issues, tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, issues)
			}
		})
	}
}

func TestDBPolicyRefs(t *testing.T) {
	conns := loadConnections(t, connectionsYAML)
	good := loadDBPolicy(t, dbPolicyYAML, conns)
	if issues := lintDBPolicyRefs([]Policy{conns, good}); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	typo := loadDBPolicy(t, strings.Replace(dbPolicyYAML, "[production, local]", "[production, locl]", 1), conns)
	issues := lintDBPolicyRefs([]Policy{conns, typo})
	if !hasErrorContaining(issues, `unknown environment "locl"`) {
		t.Fatalf("expected an unknown environment error, got %v", issues)
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
