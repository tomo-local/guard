# CommandGatePolicy Schema Reference

This is the full field reference for `.guard/policies/**/*.yaml` manifests
(the `CommandGatePolicy` resource). For a narrative introduction, see
[Writing Policies](../README.md#writing-policies) in the main README; this
document is the exhaustive spec, including every validation rule `guard
lint` enforces.

## File layout

- One file = one `CommandGatePolicy` resource = one tool + one `action`.
- Files are loaded recursively from the policies directory (`.guard/policies`
  by default, overridable with `GUARD_POLICIES_DIR`/`-rules`), so policies
  for the same tool are conventionally grouped under a subdirectory:
  `.guard/policies/<tool>/<short-action-or-qualifier>.yaml` (e.g.
  `git/allow.yaml`, `git/write-confirm.yaml`, `git/destructive-deny.yaml`,
  `rm/deny.yaml`).
- Both `.yaml` and `.yml` extensions are loaded; any other extension is
  ignored.

## Top-level structure

```yaml
apiVersion: ai-guard/v1       # required, must equal exactly "ai-guard/v1"
kind: CommandGatePolicy       # required, must equal exactly "CommandGatePolicy"
metadata:
  name: <string>              # required, non-empty, kebab-case convention
spec:
  target:
    module: permission        # required, must equal exactly "permission" in v1
    tool: <string>             # required, non-empty, e.g. "git", "rm", "curl"
  action: ALLOW | DENY | CONFIRM  # required, exactly one of these three values
  rules:                       # required, at least 1 entry
    - <Rule>
    - <Rule>
    ...
```

`spec.action` applies to **every** rule in this resource -- a resource
can't mix actions. If a tool needs more than one, split it across multiple
files (see [Multi-file example](#multi-file-example)). `ALLOW`/`DENY`/
`CONFIRM` are three peer values of one enum -- there is no separate
"confirm modifier" layered on top of `ALLOW`. This is deliberate: an
earlier design derived `CONFIRM` from `ALLOW` + a confirm flag, which made
`DENY` + that flag a representable-but-contradictory combination that had
to be caught by a dedicated lint rule. Making `CONFIRM` a peer value of
`spec.action` makes that combination impossible to write in the first
place, since a resource can only ever hold one `action`.

## Rule

```yaml
- to:
    label: <string>                                # optional, free text
    functions: [sub, commands]                    # optional
    args:
      anyOf: ["-f", "--force"]                     # optional
      noneOf: ["-x"]                               # optional (anyOf and/or noneOf)
  options:                                         # optional
    customCheck:
      path: "./scripts/x.sh"
      command: "..."
      args: ["..."]
      timeoutSeconds: 10
      when:
        - key: exit
          value: 0
          action: ALLOW | DENY | CONFIRM
  message:
    reason: "..."
    next: "..."
```

### `to` (match condition)

| Field | Type | Required | Notes |
|---|---|---|---|
| `to.label` | string | no | Free-text, purely descriptive (e.g. `read`, `write`, `destructive`, `network`, or anything else). **Plays no part in matching** -- matching is driven entirely by `functions`/`args`. Carried into the check log (`CommandLog.label`) for readability. |
| `to.functions` | list of strings | no | Subcommand names this rule applies to (e.g. `[add, commit, push]`). **Omit entirely** to make the rule a catch-all for every invocation of the tool (used for flat commands like `rm`/`curl` that have no subcommand concept). |
| `to.args` | object | no | See below. |
| `to.args.anyOf` | list of strings | one of `anyOf`/`noneOf`, if `args` is set | Non-empty strings. The rule only matches commands whose argv contains **at least one** of these tokens, literally (no regex/globs, no flag-value parsing). |
| `to.args.noneOf` | list of strings | one of `anyOf`/`noneOf`, if `args` is set | Non-empty strings. The rule only matches commands whose argv contains **none** of these tokens. Combined with `anyOf`, both must hold. Use it to step aside for a stricter sibling policy (see [one command, one policy](#one-command-one-policy)). |

How a command's subcommand is detected: the first argument that doesn't
start with `-` is treated as the "function". This is a heuristic -- it
misclassifies a flag's separate value as the function (e.g. `git -C <path>
push` sees `<path>` as the function, not `push`). See [Known
Limitations](../README.md#known-limitations-v1).

Matching precedence within one resource's `rules[]`: a rule naming
`functions` that lists the command's detected function wins; if none
matches, the first catch-all rule (no `functions`) applies. `args.anyOf`/
`args.noneOf`, when set, further restrict either kind -- a rule with an `args` condition
that doesn't match is skipped entirely, even if its `functions`/catch-all
would otherwise apply.

### `options` (optional modifiers)

| Field | Type | Required | Notes |
|---|---|---|---|
| `options.customCheck` | object | no | See [CustomCheck](#optionscustomcheck). |

#### `options.customCheck`

Mirrors Istio's `action: CUSTOM` -- delegates part of the decision to an
external script or inline shell command.

| Field | Type | Required | Notes |
|---|---|---|---|
| `options.customCheck.path` | string | exactly one of `path`/`command` | Path to an executable script, run directly with `args`. |
| `options.customCheck.command` | string | exactly one of `path`/`command` | Inline shell command, run via `sh -c "<command>" <args...>`. |
| `options.customCheck.args` | list of strings | no | Extra arguments passed to `path` or appended after `command`. |
| `options.customCheck.timeoutSeconds` | int | no (default `10`) | Must be `>= 0`. If the process doesn't exit in time, it's killed and treated as "no `when` match" (falls back to the resource's own `spec.action` as usual). |
| `options.customCheck.when` | list of `When` | no | Evaluated in order against the process's exit code; the **first** match wins. If none match (including on timeout or a run error), the customCheck contributes no override. |

`When` entries:

| Field | Type | Required | Notes |
|---|---|---|---|
| `when[].key` | string | yes | Must equal exactly `"exit"` in v1 -- the only supported check key. |
| `when[].value` | int | yes | The exit code to match. |
| `when[].action` | string (enum) | yes | `ALLOW`, `DENY`, or `CONFIRM`. |

> **Security note**: `customCheck` scripts/commands run with the same
> trust boundary as the policy files themselves (committed to and
> reviewed in this repo). Never point it at an untrusted script.

### `message` (rule-level)

| Field | Type | Required | Notes |
|---|---|---|---|
| `message.reason` | string | no | Sent to the AI verbatim as Claude Code's `permissionDecisionReason` -- **write it in English**, not shown as end-user UI text. |
| `message.next` | string | no | Same as above; what the AI should do next (e.g. "ask the user before proceeding"). |

Either field may contain the literal placeholder `{cmd}`, replaced with
the exact matched command string (e.g. `git add -A .`). It's the only
placeholder supported in v1.

## Evaluation semantics

This section documents how `guard check`/`internal/engine` actually
resolves a command against the loaded policy set -- useful when a rule
isn't behaving the way you expect.

1. **Decomposition**: the raw Bash command line is parsed with a real
   shell AST parser and walked for every `CallExpr` -- so `&&`, `;`, `|`,
   subshells, and command substitution (`$(...)`) are all decomposed into
   individual `<tool> <args...>` invocations and evaluated independently.
   Non-literal arguments (variable expansions, etc.) are rendered back as
   raw shell syntax, so they won't match a `functions` list.
2. **Per-command matching**: for each decomposed command, every policy
   resource targeting that `tool` is checked; within each, [`to`
   matching](#to-match-condition) picks at most one rule.
3. **CustomCheck override, if present**: when the matched rule has
   `options.customCheck`, the script/command runs and its `when` list is
   evaluated against the exit code. A match **always overrides** that
   resource's own `spec.action` -- even downgrading a `DENY` or `CONFIRM`
   to a plain `ALLOW`. No match (including on timeout) leaves the
   resource's own `action`. In short: **`customCheck` > the resource's own
   `action`**, decided per resource before any comparison with others.
   Because every matching resource is evaluated this way, a resource's
   `customCheck` runs even if another resource ends up deciding the result.
4. **Strictest-wins across matching resources** (a safety net): `guard
   lint` rejects policies that can match the same command (see [one
   command, one policy](#one-command-one-policy)), but if a set that
   ignores lint still has several matches, the strictest resulting action
   among them wins (`DENY > CONFIRM > ALLOW`). Messages are deduplicated and joined
   across every matching resource/rule that tied at the strictest action.
5. **No match at all**: if no resource's rule matches a decomposed
   command, the built-in default applies -- action `CONFIRM`, a generic
   reason naming the unmatched command, and a generic "review and confirm"
   next step. This default is never writable as a YAML `action`.
6. **Aggregating a compound command**: steps 1-5 produce one `Decision`
   per decomposed command; the final decision for the whole command line
   is, again, strictest-wins across all of them (same ranking as step 4).

## `guard lint` rules

Beyond the field-level constraints in the tables above (required-ness,
enums, the path/command and `when[].key` checks), `guard lint` also
checks:

| Severity | Check |
|---|---|
| Error | `apiVersion` != `"ai-guard/v1"` |
| Error | `kind` is not a known kind (`CommandGatePolicy`, or `GuardManifest` -- see [manifest-schema.md](manifest-schema.md)); the file is otherwise ignored |
| Error | `metadata.name` empty |
| Error | `spec.target.module` != `"permission"` |
| Error | `spec.target.tool` empty |
| Error | `spec.action` not one of `ALLOW`/`DENY`/`CONFIRM` |
| Error | `spec.rules` empty |
| Error | `rules[].to.args` set with neither `anyOf` nor `noneOf`, or either containing an empty string |
| Error | The same token in both `anyOf` and `noneOf` of one rule -- the rule can never match |
| Error | Two policies for the same tool can both match some command (see [one command, one policy](#one-command-one-policy)) |
| Error | `rules[].options.customCheck` has neither `path` nor `command`, or has both |
| Error | `rules[].options.customCheck.when[].key` != `"exit"` |
| Warning | A second catch-all (no `functions`) rule in the same resource -- the first one already makes it unreachable |

### One command, one policy

Any given command must be decided by exactly one policy. `guard lint`
compares every pair of policies for the same `tool` and reports an error
when some command (function + args together) matches a rule in both.
`to.label` plays no part in matching, so it does not separate two
policies, and rules inside one policy are never compared with each other
(their precedence is fixed).

To split a tool between policies, make the conditions disjoint with
`args.noneOf`. For example, `git push` is CONFIRM but `git push -f` is
DENY:

- `git/write-confirm.yaml`: `functions: [push]` with `args.noneOf: ["-f", "--force"]`
- `git/push-force-deny.yaml`: `functions: [push]` with `args.anyOf: ["-f", "--force"]`

## Examples

### Minimal CONFIRM

```yaml
# .guard/policies/curl/confirm.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: curl-confirm
spec:
  target:
    module: permission
    tool: curl
  action: CONFIRM
  rules:
    - to:
        label: network
      message:
        reason: "{cmd} accesses an external network resource."
        next: "Confirm the URL is safe with the user before proceeding."
```

### Flag-level matching (disjoint with `noneOf`)

```yaml
# .guard/policies/git/push-force-deny.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-push-force-deny
spec:
  target:
    module: permission
    tool: git
  action: DENY
  rules:
    - to:
        label: destructive
        functions: [push]
        args:
          anyOf: ["-f", "--force"]
      message:
        reason: "{cmd} force-pushes and can overwrite remote history."
        next: "Suggest a non-destructive alternative, or ask the user to run it themselves."
```

Paired with `git/write-confirm.yaml`, whose `push` rule carries
`args.noneOf: ["-f", "--force"]` so the two never match the same command:
`git push` alone evaluates to `CONFIRM`, while `git push -f`/`--force`
evaluates to `DENY`.

### CustomCheck with a CONFIRM fallback

```yaml
# .guard/policies/git/push-custom-check.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-push-custom-check
spec:
  target:
    module: permission
    tool: git
  action: DENY             # safe default: deny unless the script proves otherwise
  rules:
    - to:
        label: write
        functions: [push]
      options:
        customCheck:
          path: "./scripts/check-push.sh"
          args: ["--staged"]
          timeoutSeconds: 10
          when:
            - key: exit
              value: 0
              action: CONFIRM   # still confirm with a human even when the script says it's safe
          # no `when` match (e.g. exit != 0, or timeout) falls back to spec.action = DENY
      message:
        reason: "{cmd} requires the push-safety script to pass before it runs."
        next: "Review the script's output with the user before proceeding."
```

### Multi-file example

One tool, two actions, two files:

```yaml
# .guard/policies/git/allow.yaml
spec:
  action: ALLOW
  rules:
    - to: { label: read, functions: [log, status, diff] }
```

```yaml
# .guard/policies/git/write-confirm.yaml
spec:
  action: CONFIRM
  rules:
    - to: { label: write, functions: [add, commit, push] }
```

```yaml
# .guard/policies/git/destructive-deny.yaml
spec:
  action: DENY
  rules:
    - to: { label: destructive, functions: [reset, clean, rebase] }
      message:
        reason: "{cmd} is a destructive operation and is denied."
        next: "Suggest an alternative approach to the user."
```
