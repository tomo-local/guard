package policy

import (
	"reflect"
	"testing"
)

func TestSQLKeywords_Sources(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		tool string
		want []string
	}{
		{"psql -c in single quotes (as the shell command line gives it)", []string{"-c", "'drop table x'"}, "psql", []string{"DROP"}},
		{"psql -c in double quotes", []string{"-c", `"select \"a;b\"; delete from t"`}, "psql", []string{"SELECT", "DELETE"}},
		{"psql --command= quoted", []string{"--command='truncate t'"}, "psql", []string{"TRUNCATE"}},
		{"mysql -e quoted", []string{"-e", "'drop table x'"}, "mysql", []string{"DROP"}},
		{"psql -c", []string{"-c", "select 1"}, "psql", []string{"SELECT"}},
		{"psql --command=", []string{"--command=drop table x"}, "psql", []string{"DROP"}},
		{"psql --command x", []string{"--command", "delete from t"}, "psql", []string{"DELETE"}},
		{"psql attached -c", []string{"-cselect 1"}, "psql", []string{"SELECT"}},
		{"psql in a cluster", []string{"-Atc", "update t set a=1", "-h", "h"}, "psql", []string{"UPDATE"}},
		{"psql several -c", []string{"-c", "select 1", "-c", "drop table x"}, "psql", []string{"SELECT", "DROP"}},
		{"psql no sql", []string{"-h", "h", "app"}, "psql", []string{"UNKNOWN"}},
		{"psql script file", []string{"-f", "x.sql"}, "psql", []string{"UNKNOWN"}},
		{"psql empty -c", []string{"-c", ""}, "psql", []string{"UNKNOWN"}},
		{"mysql -e", []string{"-e", "truncate table t"}, "mysql", []string{"TRUNCATE"}},
		{"mysql --execute=", []string{"--execute=select 1; drop table x"}, "mysql", []string{"SELECT", "DROP"}},
		{"mysql no sql", []string{"app"}, "mysql", []string{"UNKNOWN"}},
		{"-e is not psql's flag", []string{"-e", "select 1"}, "psql", []string{"UNKNOWN"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqlKeywords(cmdOf(tc.tool, tc.cmd...)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sqlKeywords = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"select 1", []string{"select 1"}},
		{"select 1;", []string{"select 1"}},
		{"select 1; select 2", []string{"select 1", "select 2"}},
		{"  ;; select 1 ;; ", []string{"select 1"}},
		{"select 'a;b'; select 2", []string{"select 'a;b'", "select 2"}},
		{`select "a;b"; select 2`, []string{`select "a;b"`, "select 2"}},
		{"select 'it\\'s;x'; select 2", []string{"select 'it\\'s;x'", "select 2"}},
		{"select 1 -- a; b\n; select 2", []string{"select 1 -- a; b", "select 2"}},
		{"select /* a; b */ 1; select 2", []string{"select /* a; b */ 1", "select 2"}},
		{"do $$ begin delete from t; end $$; select 1", []string{"do $$ begin delete from t; end $$", "select 1"}},
		{"do $f$ x; y $f$; select 1", []string{"do $f$ x; y $f$", "select 1"}},
		{"", nil},
	}
	for _, tc := range cases {
		if got := splitStatements(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitStatements(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStatementKeyword(t *testing.T) {
	cases := map[string]string{
		"select 1":                             "SELECT",
		"SELECT 1":                             "SELECT",
		"  \n\tdrop table x":                   "DROP",
		"-- note\ndrop table x":                "DROP",
		"/* note */ delete from t":             "DELETE",
		"(select 1) union (select 2)":          "SELECT",
		"\\copy t from 'x'":                    "\\COPY",
		"\\! rm -rf /":                         "\\",
		"with a as (select 1) select * from a": "SELECT",
		"with a as (delete from t returning *) select * from a":                                "DELETE",
		"with a as (select 1), b as (insert into t values (1) returning *) update u set x = 1": "UPDATE",
		"with a as (select 'delete me') select * from a":                                       "SELECT", // inside a string
		"with a as (select updated_at from t) select 1":                                        "SELECT", // a different word
		"explain select 1":                         "EXPLAIN",
		"explain analyze delete from t":            "DELETE",
		"EXPLAIN (ANALYZE, BUFFERS) DELETE FROM t": "DELETE",
		"explain analyze select 1":                 "SELECT",
		"explain verbose delete from t":            "EXPLAIN", // not executed
		"set search_path = x":                      "SET",
		"":                                         "",
		"-- only a comment":                        "",
	}
	for in, want := range cases {
		if got := statementKeyword(in); got != want {
			t.Errorf("statementKeyword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickSQLRule(t *testing.T) {
	rules := []sqlRule{
		{Label: "catch-all", Action: ActionConfirm},
		{Label: "prod-drop", Labels: []string{"prod"}, Functions: []string{"drop"}, Action: ActionDeny},
		{Label: "any-select", Functions: []string{"SELECT"}, Action: ActionAllow},
		{Label: "prod-all", Labels: []string{"prod"}, Action: ActionConfirm},
	}
	pick := func(kw string, labels ...string) string {
		r, ok := pickSQLRule(rules, kw, labels)
		if !ok {
			return ""
		}
		return r.Label
	}
	cases := []struct {
		kw     string
		labels []string
		want   string
	}{
		{"DROP", []string{"prod"}, "prod-drop"},    // keywords compare case-insensitively
		{"DROP", []string{"dev"}, "catch-all"},     // the labelled rule doesn't fit; the first catch-all does
		{"SELECT", []string{"prod"}, "any-select"}, // a function rule beats an earlier catch-all
		{"UPDATE", []string{"prod"}, "catch-all"},  // first catch-all in file order
		{"DROP", nil, "catch-all"},
	}
	for _, tc := range cases {
		if got := pick(tc.kw, tc.labels...); got != tc.want {
			t.Errorf("pick(%s, %v) = %q, want %q", tc.kw, tc.labels, got, tc.want)
		}
	}

	if _, ok := pickSQLRule([]sqlRule{{Labels: []string{"prod"}}}, "SELECT", nil); ok {
		t.Fatal("a rule naming labels must not match a connection without them")
	}
}
