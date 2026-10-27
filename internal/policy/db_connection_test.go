package policy

import "testing"

func TestConnectionOf(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want dbConnection
	}{
		// psql
		{"psql -h x", []string{"psql", "-h", "db.prod.example.com", "-d", "app"}, dbConnection{"db.prod.example.com", "app"}},
		{"psql -hx attached", []string{"psql", "-hdb.example.com", "app"}, dbConnection{"db.example.com", "app"}},
		{"psql --host=x", []string{"psql", "--host=db.example.com", "--dbname=app"}, dbConnection{"db.example.com", "app"}},
		{"psql --host x", []string{"psql", "--host", "db.example.com", "app"}, dbConnection{"db.example.com", "app"}},
		{"psql positional db and user", []string{"psql", "app", "alice"}, dbConnection{"", "app"}},
		{"psql user flag value is not the db", []string{"psql", "-U", "alice", "app"}, dbConnection{"", "app"}},
		{"psql port flag value is not the db", []string{"psql", "-p", "5433", "app"}, dbConnection{"", "app"}},
		{"psql -c value is not the db", []string{"psql", "-c", "select 1", "-h", "h"}, dbConnection{"h", ""}},
		{"psql flag cluster with -c", []string{"psql", "-Atc", "select 1", "-h", "h"}, dbConnection{"h", ""}},
		{"psql uri", []string{"psql", "postgres://alice@db.example.com:5432/app?sslmode=require"}, dbConnection{"db.example.com", "app"}},
		{"psql postgresql uri", []string{"psql", "postgresql://db.example.com/app"}, dbConnection{"db.example.com", "app"}},
		{"psql uri via -d", []string{"psql", "-d", "postgres://db.example.com/app"}, dbConnection{"db.example.com", "app"}},
		{"psql uri host in query", []string{"psql", "postgres:///app?host=db.example.com"}, dbConnection{"db.example.com", "app"}},
		{"psql conninfo", []string{"psql", "host=db.example.com dbname=app user=alice"}, dbConnection{"db.example.com", "app"}},
		{"psql conninfo quoted", []string{"psql", "host='db.example.com' dbname='app'"}, dbConnection{"db.example.com", "app"}},
		{"psql -h overrides uri host", []string{"psql", "-h", "other", "postgres://db.example.com/app"}, dbConnection{"other", "app"}},
		{"psql -l takes no value", []string{"psql", "-l", "-h", "h"}, dbConnection{"h", ""}},
		{"psql -X takes no value", []string{"psql", "-X", "-h", "h", "app"}, dbConnection{"h", "app"}},
		{"psql nothing", []string{"psql"}, dbConnection{}},
		{"psql after --", []string{"psql", "--", "app"}, dbConnection{"", "app"}},
		// mysql
		{"mysql -h x", []string{"mysql", "-h", "db.example.com", "-D", "app"}, dbConnection{"db.example.com", "app"}},
		{"mysql -hx attached", []string{"mysql", "-hdb.example.com", "app"}, dbConnection{"db.example.com", "app"}},
		{"mysql --host=x", []string{"mysql", "--host=db.example.com", "--database=app"}, dbConnection{"db.example.com", "app"}},
		{"mysql positional db", []string{"mysql", "-u", "root", "app"}, dbConnection{"", "app"}},
		{"mysql -p password attached is ignored", []string{"mysql", "-psecret", "-h", "h", "app"}, dbConnection{"h", "app"}},
		{"mysql bare -p does not eat the db", []string{"mysql", "-p", "-h", "h", "app"}, dbConnection{"h", "app"}},
		{"mysql -e value is not the db", []string{"mysql", "-e", "select 1", "-h", "h"}, dbConnection{"h", ""}},
		{"mysql uri", []string{"mysql", "mysql://root@db.example.com:3306/app"}, dbConnection{"db.example.com", "app"}},
		{"mysql -D beats positional", []string{"mysql", "-D", "real", "other"}, dbConnection{"", "real"}},
		{"not a db client", []string{"cat", "-h", "x"}, dbConnection{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := connectionOf(cmdOf(tc.cmd[0], tc.cmd[1:]...))
			if got != tc.want {
				t.Fatalf("connectionOf(%v) = %+v, want %+v", tc.cmd, got, tc.want)
			}
		})
	}
}
