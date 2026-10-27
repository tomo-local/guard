# Database Files Reference

Database clients (`psql`, `mysql`) are governed by two files in `.guard/db/`
(`GUARD_DB_DIR` / `-db` to change the directory). Both are optional.

- `connections.yml` (`kind: GuardDBConnections`) defines **environments**
  (production, staging, ...) by the host and database a connection goes to.
  It makes no decisions.
- `policy.yml` (`kind: GuardDBPolicy`) says what to do with a command,
  depending on the environment of the connection it opens.

A command is checked against the per-tool policies in `.guard/policies/`
first, then `.guard/db/policy.yml`, then the project manifest. `.yaml` works
as well as `.yml`. This is separate from the manifest's `sqlGate`, which is
reserved for judging the SQL text itself (not implemented yet).

## `connections.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBConnections
metadata:
  name: connections
spec:
  environments:
    - name: production
      hosts: ["*.prod.example.com", "10.0.0.5"]
    - name: reporting
      hosts: ["*.example.com"]
      databases: ["report_*"]
    - name: local
      hosts: [localhost, 127.0.0.1]
```

| Field | Required | Notes |
|---|---|---|
| `environments[].name` | yes | Unique. |
| `environments[].hosts` | one of `hosts`/`databases` | `path.Match` patterns, compared case-insensitively. |
| `environments[].databases` | one of `hosts`/`databases` | Same. A connection with no database name never matches an environment that lists databases. |

A connection belongs to an environment when it matches `hosts` (if set)
**and** `databases` (if set). It may belong to several environments. A
command that names no host connects to `localhost`.

## `policy.yml`

```yaml
apiVersion: ai-guard/v1
kind: GuardDBPolicy
metadata:
  name: db-policy
spec:
  target:
    clients: [psql, mysql]   # optional, both when omitted
    action: CONFIRM          # optional default for rules without `action`; DENY if omitted
  rules:
    - to:
        label: production    # optional, descriptive only
        environment:
          anyOf: [production]
      action: DENY           # optional
      message:               # optional, English, `{cmd}` is the only placeholder
        reason: "{cmd} connects to a production database."
        next: "Ask the user to run it themselves."
    - to:
        environment:
          noneOf: [production, staging, local]   # connections in no known environment
```

`to.environment` takes `anyOf` (the connection is in at least one of these
environments) and/or `noneOf` (in none of them), the same shape as
[`to.branch`](policy-schema.md#to-match-condition). Every name must be
defined in `connections.yml`. When several rules match, the strictest action
wins (`DENY > CONFIRM > ALLOW`; the first rule on a tie). With no
`connections.yml`, no rule can match.

## Where the connection is read from

The command line only:

- `psql`: `-h`/`--host`, `-d`/`--dbname`, the positional database name, a
  `postgres://` or `postgresql://` URI (as `-d` or positional), or a
  connection string such as `"host=db.example.com dbname=app"`. `-h` wins
  over a host inside a URI.
- `mysql`: `-h`/`--host`, `-D`/`--database`, the positional database name,
  or a `mysql://` URI. A password attached to `-p` is ignored.

Values of other flags (`-U`, `-p` for psql, `-c`, `-e`, ...) are never taken
for the database name.

### Known limitations

- Environment variables (`PGHOST`, `DATABASE_URL`, ...), `~/.pgpass`,
  service files, and script files (`-f`) or standard input are not read, so
  a connection that depends on them looks like `localhost`.
- `psql` and `mysql` only; other clients are not looked at.
- Judging the SQL text (`DROP`, `DELETE`, ...) is not implemented yet.

## `guard lint`

| Severity | Check |
|---|---|
| Error | `connections.yml`: `apiVersion`/`kind`/`metadata.name` wrong, no environments, duplicate or empty names, an environment with neither `hosts` nor `databases`, an invalid pattern, or more than one connections file |
| Error | `policy.yml`: wrong `apiVersion`/`kind`, no rules, a rule without `to.environment`, an unknown client, an `action` that isn't `ALLOW`/`DENY`/`CONFIRM`, `anyOf`/`noneOf` both empty or sharing a name, or more than one DB policy file |
| Error | A rule refers to an environment `connections.yml` doesn't define (or there is no connections file) |
