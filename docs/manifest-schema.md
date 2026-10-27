# GuardManifest Schema Reference

`.guard/manifest.yaml` is the project-wide rule file (`kind: GuardManifest`).
It sits **behind** the per-tool [policies](policy-schema.md): a command is
first checked against `.guard/policies/**`, and only when no policy has an
opinion does the manifest get to decide.

It is loaded from `.guard/manifest.yaml` by default (`-manifest` /
`GUARD_MANIFEST_PATH`). A missing file is fine -- there is simply no
manifest. Only one manifest is allowed.

## Structure

```yaml
apiVersion: ai-guard/v1        # required, exactly "ai-guard/v1"
kind: GuardManifest            # required, exactly "GuardManifest"
metadata:
  name: <string>               # required
spec:
  commandGate:                 # optional: restrict commands by the paths they touch
    target:
      action: DENY             # optional default for rules without `action`; DENY if omitted
    rules:                     # at least 1 entry
      - to:
          label: <string>      # optional, descriptive only
          paths:               # required
            anyOf: ["/cmd/*"]  # at least 1 non-empty glob pattern
        action: CONFIRM        # optional: ALLOW | DENY | CONFIRM (overrides target.action)
        message:               # optional, English, `{cmd}` is the only placeholder
          reason: "..."
          next: "..."
  repositoryGate:              # optional: labels branches for `to.branch` in policies
    repose:
      - name: owner/name       # required, matched against the origin URL
        main: main             # optional: gets the `main` label
        protected: [main, staging, "docs/*"]   # optional: get the `protected` label
  sqlGate: {...}               # read, not enforced yet (lint warns)
```

## How `commandGate` decides

1. Every argument of a command that doesn't start with `-` is a candidate
   path. Relative ones are resolved against the directory `guard` runs in,
   then made relative to the **project root** (the directory that holds
   `.guard/`). Arguments outside the project, and the root itself, are
   ignored.
2. A rule matches when any `paths.anyOf` pattern matches any candidate.
3. If several rules match, the strictest action wins (`DENY > CONFIRM >
   ALLOW`; the first rule on a tie). The verdict uses that rule's
   `message`, or a generic one naming the path.
4. A compound command (`a && b`) is split first; each simple command is
   evaluated separately and the strictest result wins, like everywhere
   else.

### Patterns

Patterns are anchored at the project root; the leading `/` is optional.

| Pattern | Matches |
|---|---|
| `/cmd/*` | `cmd/a.go`, but not `cmd/x/a.go` |
| `/cmd/**` | everything under `cmd/`, at any depth |
| `/**/*.go` | any `.go` file at any depth |
| `/src/[ab]/*` | `[...]` and `?` follow `path.Match` |

`*` stays within one path segment; `**` as a whole segment spans any
number of segments. A directory above the pattern's literal prefix is
covered too, so `/cmd/*` also matches `cmd` itself -- `rm -rf cmd` is
caught. A pattern that begins with a wildcard (`/**/*.go`) covers no
ancestors.

### Known limitations

- Moving around inside the command isn't tracked: in `cd cmd && rm a.go`
  the `a.go` is resolved against the directory `guard` runs in, not
  `cmd/`.
- Anything path-shaped is a candidate, including text inside quotes
  (`git commit -m "/cmd/x"` is not a path to touch, but is treated as one).
  Because the manifest only decides commands no policy matched, a false hit
  lands on the rule's action (often CONFIRM or DENY).
- The directory `guard` runs in comes from the process, not the hook
  payload.

## `repositoryGate`: branch labels

`repositoryGate` makes no decisions on its own. It tells guard which
branches of which repository are special, by giving them **labels**; the
policy rules then say what to do with them (see
[`to.branch`](policy-schema.md#to-match-condition)).

- guard finds the repository by running `git remote get-url origin` in the
  directory it runs in and comparing the path (`owner/name`, any
  `.git` suffix removed; `git@host:owner/name`, `https://host/owner/name`
  and `ssh://` URLs all work) with `repose[].name`. A repository that is
  not listed, or no git repository at all, has no labels, and rules with a
  `to.branch` condition do not match there.
- Labels: `main` (the branch equal to `main:`) and `protected` (any branch
  matching a `protected:` pattern; `*` stays within one path segment, so
  `docs/*` matches `docs/guide` but not `docs/a/b`). A branch can have both.
- Which branch a command acts on is worked out from the command (see
  [`to.branch`](policy-schema.md#to-match-condition)); `git` is only run
  when a rule actually has a `to.branch` condition.

## `guard lint`

| Severity | Check |
|---|---|
| Error | `apiVersion`/`kind`/`metadata.name` missing or wrong |
| Error | `commandGate.rules` empty, a rule without `to.paths`, `paths.anyOf` empty or containing an empty string |
| Error | `target.action` or a rule's `action` not one of `ALLOW`/`DENY`/`CONFIRM` |
| Error | A `paths.anyOf` entry that isn't a valid pattern |
| Error | More than one `GuardManifest` |
| Error | `repositoryGate.repose[].name` missing, not `owner/name`-shaped, or listed twice |
| Error | A `repose[].protected` entry that isn't a valid pattern, or is empty |
| Warning | `sqlGate` has content (not enforced yet) |

## Example

```yaml
# .guard/manifest.yaml
apiVersion: ai-guard/v1
kind: GuardManifest
metadata:
  name: default-rules
spec:
  commandGate:
    target:
      action: DENY
    rules:
      - to:
          label: cmd-dir
          paths:
            anyOf: ["/cmd/*"]
        message:
          reason: "No policy matched {cmd}, which touches /cmd; denying by default."
          next: "Add an explicit allow/deny rule under .guard/policies/ for this command."
      - to:
          label: src-dir
          paths:
            anyOf: ["/src/*"]
        action: CONFIRM
        message:
          reason: "{cmd} is a write operation under /src."
          next: "Confirm with the user before proceeding."
```

With the policies of this repo, `cat cmd/a.go` is `DENY`, `touch
src/a.go` is `CONFIRM`, `git status` stays `ALLOW` (a policy matched, so
the manifest isn't consulted), and `ls .` is the usual default `CONFIRM`.
