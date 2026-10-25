# ai-guard-tracker

**English** | [日本語](docs/ja/README.md)

A guardrail CLI (`guard`) that decides ALLOW / DENY / CONFIRM for Bash
commands an AI agent (Claude Code, etc.) is about to run, based on
policies you define ahead of time.

## Table of Contents

- [Introduction](#introduction)
- [Features](#features)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [`guard init`](#guard-init)
  - [`guard check`](#guard-check)
  - [`guard record`](#guard-record)
  - [`guard lint`](#guard-lint)
- [Configuration](#configuration)
- [Writing Policies](#writing-policies)
  - [Matching on flags](#matching-on-flags-toargsanyof)
  - [Custom checks](#custom-checks-optionscustomcheck)
- [Known Limitations (v1)](#known-limitations-v1)
- [Development](#development)

## Introduction

AI coding agents run Bash commands on your behalf, and not every command
they might run is one you want executed silently -- a force-push, an
`rm -rf`, an unreviewed `curl | sh`. `ai-guard-tracker` is a small CLI,
`guard`, that plugs into Claude Code's hook system and decides, per
command, whether to **ALLOW** it, **DENY** it outright, or **CONFIRM**
with a human first -- based on policy manifests you write once and keep
in your repo.

The policy format is a Kubernetes-style manifest modeled on
[Istio's `AuthorizationPolicy`](https://istio.io/latest/docs/reference/config/security/authorization-policy/),
so if you've written an Istio `AuthorizationPolicy` before, this will
look familiar.

`guard init` / `guard check` / `guard record` / `guard lint` are
implemented. `guard report` is the only piece still pending (see
`tmp/remaining-plan.md` if present).

## Features

- **Policy-driven decisions** for Bash commands, evaluated as a Claude
  Code `PreToolUse` hook -- `guard check` returns ALLOW/DENY/CONFIRM
  (`allow`/`deny`/`ask`) before a command ever runs.
- **Istio-style manifests**: one tool, one action (ALLOW, DENY, or
  CONFIRM), one file, grouped under `.guard/policies/<tool>/`.
- **CONFIRM as a peer action, not a modifier**: `spec.action: CONFIRM` is
  its own enum value alongside ALLOW/DENY, so "DENY but still
  confirmable" can't even be written -- a resource only ever holds one
  action.
- **Flag-level matching** (`to.args.anyOf`) to distinguish, e.g., plain
  `git push` from `git push -f`.
- **Pluggable custom checks** (`options.customCheck`): delegate part of
  the decision to an external script and let its exit code override the
  verdict (mirrors Istio's `action: CUSTOM`).
- **Compound-command aware**: `&&`/`;`/`|`/subshells/command substitution
  are decomposed via a real shell AST parser, and the strictest verdict
  (`DENY > CONFIRM > ALLOW`) wins across all of them.
- **Raw audit logs**: both the decisions (`guard check` ->
  `.guard/logs/check.jsonl`) and what commands actually did
  (`guard record` -> `.guard/logs/record.jsonl`) are recorded as JSON
  Lines, unsummarized.
- **Configurable via env vars** (`GUARD_POLICIES_DIR`, `GUARD_LOG_PATH`,
  `GUARD_RECORD_LOG_PATH`) so a distributed binary doesn't need
  per-invocation flags.

## Quick Start

```bash
go build -o bin/guard .
./bin/guard init
```

`guard init` seeds `.guard/policies/` with starter policies (if empty)
and creates `.guard/logs/`, then prints the Claude Code hook snippet to
add to `.claude/settings.json` yourself -- see
[Registering the Claude Code hooks](docs/hooks-setup.md) for the full
setup (English) or [`docs/ja/hooks-setup.md`](docs/ja/hooks-setup.md)
(Japanese).

Once the hook is registered, try it directly:

```bash
guard check "git push --force"
# DENY git push --force
# reason: git push --force force-pushes and can overwrite remote history.
# next: Suggest a non-destructive alternative, or ask the user to run it themselves.
```

## Commands

### `guard init`

Sets up `.guard/policies/` and `.guard/logs/` for a new project.

```bash
guard init
```

- Only when `.guard/policies/` is empty (no policies at all) does it write
  this CLI's own starter policies (5 files for `git`/`rm`/`curl`, embedded
  into the binary via `go:embed`). If policies already exist there, it
  **never overwrites anything**.
- Also creates `.guard/logs/` (the log output directory).
- **Never writes to `.claude/settings.json` automatically** (to avoid
  clobbering existing configuration) -- it just prints the hook JSON
  snippet to add yourself.
- `-rules`/`-log` resolve the same way as `guard check`/`guard lint` (see
  [Configuration](#configuration) below).

### `guard check`

Evaluates a Bash command string and returns ALLOW/DENY/CONFIRM. It
auto-detects two input modes.

```bash
# CLI mode: pass the command directly as an argument
guard check "git push --force"

# hook mode: read a Claude Code PreToolUse hook payload from stdin
echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | guard check
```

- **CLI mode**: prints a human-readable result to stdout. Exit code is
  `0=ALLOW` / `1=DENY` / `2=CONFIRM`.
- **hook mode**: prints Claude Code-compatible JSON
  (`hookSpecificOutput.permissionDecision`: `allow`/`deny`/`ask`, with a
  reason and next action in `permissionDecisionReason`) and always exits
  0. If `tool_name` isn't `Bash`, it prints `{}` and does nothing (out of
  scope).

Every decision is recorded to `.guard/logs/check.jsonl` (JSON Lines,
append-only, one decision per line).

Registering `guard check` as the `PreToolUse` hook is what actually
makes it take effect; see
[Registering the Claude Code hooks](docs/hooks-setup.md).

### `guard record`

Reads a Claude Code **PostToolUse** hook payload from stdin and appends
what a Bash command actually did to `.guard/logs/record.jsonl` -- the
command, raw `stdout`/`stderr` (verbatim, not summarized), and whether it
was interrupted. There is no CLI mode; it only makes sense as a hook.

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"echo hi"},"tool_response":{"stdout":"hi\n"}}' | guard record
```

- `tool_name != "Bash"` is a silent no-op.
- **Always exits 0** -- recording must never block or alter Claude's
  flow. A log-write failure is only a warning on stderr.
- Claude Code's documented `tool_response` for Bash has no exit-code
  field (only `stdout`/`stderr`/`interrupted`/`isImage`), so `guard
  record` doesn't fabricate a success/failure flag.

Register it as `PostToolUse` -> `guard record`; see
[`docs/hooks-setup.md`](docs/hooks-setup.md).

### `guard lint`

Validates every policy manifest under `.guard/policies/` (subdirectories
are read recursively too).

```bash
guard lint
guard lint -rules ./some/other/dir
```

Exit code: `0` = no errors (warnings may still be printed), `1` = at
least one error.

## Configuration

`-rules`/`-log` can also be overridden via environment variables, for
when `guard` is installed/distributed as a CLI and you don't want to pass
flags every time. Precedence is **`-flag` > env var > built-in default**.

| Env var | Applies to | Flag | Default |
|---|---|---|---|
| `GUARD_POLICIES_DIR` | `init`/`check`/`lint` | `-rules` | `.guard/policies` |
| `GUARD_LOG_PATH` | `init`/`check` | `-log` | `.guard/logs/check.jsonl` (`init` creates this path's parent directory) |
| `GUARD_RECORD_LOG_PATH` | `record` | `-log` | `.guard/logs/record.jsonl` |

```bash
GUARD_POLICIES_DIR=./some/other/dir guard lint
```

## Writing Policies

Policies are Kubernetes-style manifests modeled on
[Istio's `AuthorizationPolicy`](https://istio.io/latest/docs/reference/config/security/authorization-policy/):
one tool, one action, one file. Policies for the same tool live together
under `.guard/policies/<tool>/`.

> This section is a narrative introduction. For the full field reference
> (every field, type, validation rule, and `guard lint` check), see
> [**docs/policy-schema.md**](docs/policy-schema.md).

```yaml
# .guard/policies/git/allow.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-allow
spec:
  target:
    module: permission   # fixed to "permission" in v1
    tool: git
  action: ALLOW           # exactly one of ALLOW | DENY | CONFIRM, shared by every rule in this resource
  rules:
    - to:
        label: read                                    # optional, free text -- purely descriptive, doesn't affect matching
        functions: [log, status, diff, show, branch, fetch]
```

```yaml
# .guard/policies/git/write-confirm.yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-write-confirm
spec:
  target:
    module: permission
    tool: git
  action: CONFIRM
  rules:
    - to:
        label: write
        functions: [add, commit, push, pull, merge]    # omit for a catch-all over the whole tool
      message:          # optional
        reason: "{cmd} is a write operation."
        next: "Confirm with the user before proceeding."
```

`message.reason`/`message.next` are passed verbatim to the AI (Claude) as
`permissionDecisionReason`, so **write them in English**. `{cmd}` is the
only supported placeholder; it's replaced with the exact command that
matched (e.g. `git add -A .`).

- **`action` is exactly one value per resource, chosen from ALLOW, DENY,
  or CONFIRM** -- three peer values of one enum, not ALLOW/DENY with
  CONFIRM layered on top as a modifier. An earlier design derived CONFIRM
  from `ALLOW` + a confirm flag, but that made `DENY` + that flag a
  representable-but-contradictory combination that needed a dedicated
  lint rule to catch. Making CONFIRM a peer value makes that combination
  impossible to write in the first place, since a resource can only ever
  hold one action. If a tool needs more than one action, split it across
  resource files (e.g. `git/allow.yaml`, `git/write-confirm.yaml`,
  `git/destructive-deny.yaml`). Multiple rules for the *same* action can
  still live in one file.
- When multiple policies match the same command, or when a compound
  command (`&&`/`;`/`|`/...) decomposes into several commands whose
  verdicts differ, the strictest one wins: **DENY > CONFIRM > ALLOW**.
- The default when nothing matches at all is **CONFIRM** -- a built-in
  fallback, not something you can write as a YAML `action`.
- Omitting `functions` makes the rule a catch-all for every invocation of
  that tool (for flat commands like `rm`/`curl` that have no subcommand).
- `to.label` is a free-text, optional note for whoever is reading or
  auditing the policy (e.g. `write`, `destructive`, `network`, or
  anything else you find useful) -- it plays **no part in matching**,
  which is driven entirely by `functions`/`args`. It's carried into the
  check log (`.guard/logs/check.jsonl`'s `commands[].label`) for
  readability.

### Matching on flags (`to.args.anyOf`)

`to.args.anyOf` restricts a rule to commands whose argv contains at least
one of the listed tokens (literal match only -- no regex or globs). For
example, "`git push` alone should be CONFIRM, but `git push -f`/`--force`
should be DENY" is two separate resources: the normal
`git/write-confirm.yaml` (CONFIRM, no `args` condition) plus a separate
DENY resource with `args.anyOf: ["-f", "--force"]`
(`git/push-force-deny.yaml`). When `-f` is present both match and DENY
wins; when it's absent the DENY resource simply doesn't match, leaving
CONFIRM in place -- the existing strictest-wins logic handles it for
free.

```yaml
# .guard/policies/git/push-force-deny.yaml
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

### Custom checks (`options.customCheck`)

Mirrors Istio's `action: CUSTOM` (delegating authorization to an external
engine). Runs a script or inline command attached to the matching rule
and lets its exit code override the decision.

```yaml
# Base is DENY (deny by default unless the script proves otherwise -- the safe default)
spec:
  action: DENY
  rules:
    - to:
        label: write
        functions: [push]
      options:
        customCheck:
          path: "./scripts/check-push.sh"   # exactly one of path/command
          args: ["--staged"]
          timeoutSeconds: 10                 # default 10s; falls back to the base action (DENY) on timeout
          when:
            - key: exit
              value: 0
              action: CONFIRM   # still confirm with a human even when the script says it's safe
            # if no `when` entry matches (e.g. exit != 0), it falls back to spec.action = DENY
```

**Precedence when a rule sets `options.customCheck`**: a matched `when`
entry always overrides the resource's own `action` -- even downgrading a
`DENY`/`CONFIRM` resource to a plain `ALLOW`. The override only doesn't
apply when `customCheck` wasn't present, didn't run, or had no `when`
match (including on timeout); in those cases the resource's own `action`
is used as-is.

> **Security note**: `customCheck` scripts run within the same trust
> boundary as `.guard/policies/` itself (committed to and reviewed in the
> repo). Never point it at an untrusted script.

## Known Limitations (v1)

- Scope is **Bash commands only**.
- Subcommand (`functions`) detection is a heuristic: "the first argument
  that doesn't start with `-`". `git -C <path> push` mistakes `<path>`
  for the function, since it directly follows a flag that takes a value.
- Match granularity goes up to `functions` × `args.anyOf` (`to.label` is
  descriptive only and plays no part in matching). `args.anyOf` is
  literal-token matching only (no regex/globs, and it doesn't parse
  flag-value pairs). Anything more complex belongs in
  `options.customCheck`.
- Calls inside command substitution (`$(...)`) are walked and evaluated
  too, but non-literal arguments (variable expansions, etc.) are rendered
  back as raw shell syntax, so they never match a `functions` list and
  fall through to the catch-all/default.

## Development

```bash
go build ./...
go vet ./...
go test ./...
```
