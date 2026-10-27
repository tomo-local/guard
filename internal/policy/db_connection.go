package policy

import (
	"net/url"
	"strings"

	"ai-guard-tracker/internal/shellcmd"
)

// DBClients are the database clients whose connection target can be read
// from the command line.
var DBClients = []string{"psql", "mysql"}

// dbConnection is where a database client command connects to. Fields the
// command line doesn't say are empty.
type dbConnection struct {
	Host     string
	Database string
}

// connectionOf reads the connection target out of a psql or mysql command:
// the host and database flags, a connection URI, a libpq connection string
// (psql), or the positional database name. Environment variables, password
// files and script files are not consulted.
func connectionOf(cmd shellcmd.SimpleCommand) dbConnection {
	switch cmd.Tool {
	case "psql":
		return psqlConnection(cmd.Args)
	case "mysql":
		return mysqlConnection(cmd.Args)
	}
	return dbConnection{}
}

// Long flags that take a value, so the value isn't taken for a positional.
var psqlLongFlags = map[string]bool{
	"--host": true, "--port": true, "--username": true, "--dbname": true,
	"--file": true, "--command": true, "--output": true, "--set": true,
	"--variable": true, "--field-separator": true, "--record-separator": true,
	"--pset": true, "--log-file": true, "--table-attr": true,
}

var mysqlLongFlags = map[string]bool{
	"--host": true, "--port": true, "--user": true, "--database": true,
	"--socket": true, "--execute": true,
}

func psqlConnection(args []string) dbConnection {
	// Flags that take a value (-c "sql", -f file, ...). Their values must not
	// be mistaken for the positional database name.
	values, _, positionals := splitFlags(args, "hpUdfcoFRPLTv", psqlLongFlags)

	var conn dbConnection
	spec := firstNonEmpty(values["-d"], values["--dbname"])
	if spec == "" && len(positionals) > 0 {
		spec = positionals[0]
	}
	switch {
	case strings.HasPrefix(spec, "postgres://") || strings.HasPrefix(spec, "postgresql://"):
		conn = uriConnection(spec)
	case strings.Contains(spec, "="):
		conn = conninfoConnection(spec)
	default:
		conn.Database = spec
	}
	if h := firstNonEmpty(values["-h"], values["--host"]); h != "" {
		conn.Host = h
	}
	return conn
}

func mysqlConnection(args []string) dbConnection {
	values, _, positionals := splitFlags(args, "hPuDSe", mysqlLongFlags)

	var conn dbConnection
	if len(positionals) > 0 && strings.HasPrefix(positionals[0], "mysql://") {
		conn = uriConnection(positionals[0])
	} else {
		if len(positionals) > 0 {
			conn.Database = positionals[0]
		}
	}
	if d := firstNonEmpty(values["-D"], values["--database"]); d != "" {
		conn.Database = d
	}
	if h := firstNonEmpty(values["-h"], values["--host"]); h != "" {
		conn.Host = h
	}
	return conn
}

// splitFlags separates args into flag values and positional arguments.
// shortValued lists the short flag letters that take a value (attached, as
// in -hHOST, or as the next argument, and anywhere inside a cluster like
// -Atc "sql"); longValued the long flags that do (--host=x or --host x).
// Values are keyed by the flag as written, e.g. "-h" or "--host"; values
// holds the last value of each flag, multi every value in order.
func splitFlags(args []string, shortValued string, longValued map[string]bool) (values map[string]string, multi map[string][]string, positionals []string) {
	values = map[string]string{}
	multi = map[string][]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			for _, rest := range args[i+1:] {
				positionals = append(positionals, unshell(rest))
			}
			return values, multi, positionals
		case strings.HasPrefix(a, "--"):
			name, val, has := strings.Cut(a, "=")
			if !longValued[name] {
				continue
			}
			if !has && i+1 < len(args) {
				i++
				val = args[i]
			}
			val = unshell(val)
			values[name] = val
			multi[name] = append(multi[name], val)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for k := 1; k < len(a); k++ {
				if !strings.ContainsRune(shortValued, rune(a[k])) {
					continue
				}
				val := a[k+1:]
				if val == "" && i+1 < len(args) {
					i++
					val = args[i]
				}
				val = unshell(val)
				values["-"+string(a[k])] = val
				multi["-"+string(a[k])] = append(multi["-"+string(a[k])], val)
				break
			}
		default:
			positionals = append(positionals, unshell(a))
		}
	}
	return values, multi, positionals
}

// unshell removes one level of shell quoting from an argument as shellcmd
// hands it over (it keeps the quotes of the command line): 'x' and "x" become
// x, and inside double quotes \" \\ \$ and \` lose their backslash.
func unshell(a string) string {
	if len(a) >= 2 && a[0] == '\'' && a[len(a)-1] == '\'' {
		return a[1 : len(a)-1]
	}
	if len(a) >= 2 && a[0] == '"' && a[len(a)-1] == '"' {
		inner := a[1 : len(a)-1]
		r := strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`")
		return r.Replace(inner)
	}
	return a
}

// uriConnection reads host and database from postgres:// or mysql:// URIs.
func uriConnection(raw string) dbConnection {
	u, err := url.Parse(raw)
	if err != nil {
		return dbConnection{}
	}
	conn := dbConnection{Host: u.Hostname(), Database: strings.TrimPrefix(u.Path, "/")}
	q := u.Query()
	if conn.Host == "" {
		conn.Host = q.Get("host")
	}
	if conn.Database == "" {
		conn.Database = q.Get("dbname")
	}
	return conn
}

// conninfoConnection reads host and dbname from a libpq connection string
// like "host=db.example.com dbname=app".
func conninfoConnection(s string) dbConnection {
	var conn dbConnection
	for _, field := range strings.Fields(s) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `'"`)
		switch key {
		case "host":
			conn.Host = val
		case "dbname":
			conn.Database = val
		}
	}
	return conn
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
