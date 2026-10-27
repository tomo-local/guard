# Database Files Reference

Database clients (`psql`, `mysql`) are governed by two files in `.guard/db/`
(`GUARD_DB_DIR` / `-db` to change the directory), plus the manifest's
`sqlGate`. All are optional.

- `connections.yml` (`kind: GuardDBConnections`) defines **environments**
  (production, staging, ...) and the **labels** they carry. It makes no
  decisions.
- `policy.yml` (`kind: GuardDBPolicy`) writes the SQL rules per label: what
  may run against connections labelled `prod`, and so on.
- The manifest's [`sqlGate`](manifest-schema.md#sqlgate-sql-rules-for-every-environment)
  holds SQL rules that apply whatever the environment.

A command is checked against the per-tool policies in `.guard/policies/`
first, then `.guard/db/policy.yml`, then the manifest (`sqlGate` and
`commandGate`). `.yaml` works as well as `.yml`.

## `connections.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata:
  name: connections
spec:
  environments:
    - name: production
      labels: [prod]
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
```

| Field | Required | Notes |
|---|---|---|
| `environments[].name` | yes | Unique. For identification and messages; rules don't refer to it. |
| `environments[].labels` | no | What `policy.yml` refers to. Unique within the environment. |
| `environments[].cli` | one way of connecting | How a `psql` / `mysql` command line is recognized. Each way of connecting has its own block because what identifies a connection differs (other ways, such as an MCP client, will get their own blocks). Unknown keys are an error. |
| `cli.hosts` | one of `hosts`/`databases` | `path.Match` patterns, compared case-insensitively. |
| `cli.databases` | one of `hosts`/`databases` | Same. A connection with no database name never matches an environment that lists databases. |

A connection belongs to an environment when it matches `cli.hosts` (if set)
**and** `cli.databases` (if set). It may belong to several environments; it
then carries the labels of all of them. A command that names no host
connects to `localhost`.

## `policy.yml`

Written like a per-tool policy: `functions` lists what the rule is about
(here SQL statement keywords instead of subcommands), and a rule with no
`functions` is a catch-all.

```yaml
apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  target:
    clients: [psql, mysql]   # optional, both when omitted
    action: DENY             # optional default for rules without `action`; DENY if omitted
  rules:
    - to:
        label: prod-schema   # optional, descriptive only
        labels: [prod]
        functions: [DROP, TRUNCATE, ALTER]
      action: DENY
      message:               # optional, English, `{cmd}` is the only placeholder
        reason: "{cmd} changes the schema of a production database."
        next: "Ask the user to run it themselves."
    - to:
        labels: [prod]
        functions: [SELECT, SHOW]
      action: ALLOW
    - to:
        labels: [prod]       # no functions: every other statement on prod
      action: CONFIRM
```

| Field | Notes |
|---|---|
| `to.labels` | The rule applies to connections whose environments carry **at least one** of these labels. Omit it to apply to every connection. Every label must be defined in `connections.yml`. |
| `to.functions` | SQL statement keywords (case-insensitive). Omit it for a catch-all. |
| `action` | `ALLOW` / `DENY` / `CONFIRM`. Defaults to `target.action`, then `DENY`. |

### How a rule is chosen

Each SQL statement of the command is judged on its own:

1. Only rules whose `labels` fit the connection are candidates (a rule with
   no `labels` fits every connection; with no `connections.yml`, a rule that
   names labels never fits).
2. Among them, the first rule (in file order) that lists the statement's
   keyword in `functions` wins.
3. Otherwise the first catch-all (no `functions`) applies.
4. No rule: the policy has no opinion, and the manifest's `sqlGate` gets to
   decide.

If the command runs several statements, the strictest result stands
(`DENY > CONFIRM > ALLOW`): `select 1; drop table x` is judged by the `DROP`
rule.

## Where the connection and the SQL are read from

The command line only.

- Connection, `psql`: `-h`/`--host`, `-d`/`--dbname`, the positional
  database name, a `postgres://` or `postgresql://` URI (as `-d` or
  positional), or a connection string such as `"host=db.example.com
  dbname=app"`. `-h` wins over a host inside a URI. `mysql`: `-h`/`--host`,
  `-D`/`--database`, the positional database name, or a `mysql://` URI.
- SQL, `psql`: every `-c`/`--command`. `mysql`: every `-e`/`--execute`.
- Shell quoting around a value is removed first, so `-c 'drop table x'` is
  read as `drop table x`.

### Statements and keywords

The SQL text is split at `;` (not inside quotes, comments or `$$` blocks).
The **keyword** of a statement is its first word, upper-cased (`select` ->
`SELECT`), with these exceptions:

| Statement | Keyword |
|---|---|
| `WITH ...` | The strictest of `DELETE` > `UPDATE` > `MERGE` > `INSERT` found in it, else `SELECT` |
| `EXPLAIN ANALYZE <statement>` | That statement's keyword (it is executed) |
| `EXPLAIN <statement>` | `EXPLAIN` |
| psql meta-commands (`\copy`, ...) | The word with its backslash (`\COPY`) |
| no SQL on the command line (`-f file`, standard input, an interactive session, an empty `-c`) | `UNKNOWN` |

### Known limitations

- Script files (`-f`), standard input and interactive sessions are not
  read: they are `UNKNOWN`, so a rule with `functions: [UNKNOWN]` is how to
  decide about them.
- Only the first word (and the `WITH` / `EXPLAIN` cases above) is looked at.
  Sub-queries, function calls and `SELECT ... INTO` are not analyzed.
- Environment variables (`PGHOST`, `DATABASE_URL`, ...), `~/.pgpass` and
  service files are not read, so a connection that depends on them looks
  like `localhost`.
- `psql` and `mysql` only.

## `guard lint`

| Severity | Check |
|---|---|
| Error | `connections.yml`: `apiVersion`/`kind`/`metadata.name` wrong, no environments, duplicate or empty names, an environment with no way of connecting, `cli` with neither `hosts` nor `databases`, an invalid pattern, an empty or repeated label, an unknown key, or more than one connections file |
| Error | `policy.yml`: wrong `apiVersion`/`kind`, no rules, an unknown client, an `action` that isn't `ALLOW`/`DENY`/`CONFIRM`, an empty label or function, or more than one DB policy file |
| Error | A rule refers to a label no environment carries (or there is no connections file) |
| Warning | A second rule for every statement on every connection (no `labels`, no `functions`): the first one makes it unreachable |
