---
name: guard-rule-helper
description: Add or edit ai-guard-tracker CommandGatePolicy manifests under .guard/policies/ from a natural-language description of what to allow/deny/confirm, then validate with guard lint. Use when the user describes a command they want guard to allow, deny, or confirm (e.g. "deny rm -rf", "git push -f should need confirmation", "allow curl but ask first").
---

# guard-rule-helper

Turn a natural-language request ("deny `rm -rf`", "git push should ask for
confirmation", "force-push should be denied but plain push is fine") into a
valid `.guard/policies/` manifest, then validate it.

## Before writing anything

1. Read every existing file under `.guard/policies/<tool>/` for the tool the
   user is talking about (e.g. `.guard/policies/git/*.yaml`). Don't guess the
   schema from memory -- the files on disk are the ground truth for this
   project's current conventions, and you need them to decide whether to
   edit an existing resource or add a new one.
2. If `.guard/policies/` doesn't exist yet or is empty, tell the user to run
   `guard init` first (or offer to run it) instead of inventing a layout.

## Schema

```yaml
apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: <kebab-case, usually "<tool>-<action>" or "<tool>-<qualifier>-<action>">
spec:
  target:
    module: permission   # fixed value, always "permission"
    tool: <command name, e.g. "git", "rm", "curl">
  action: ALLOW | DENY | CONFIRM   # exactly one, applies to every rule in this file
  rules:
    - to:
        label: <string>                # optional, free text -- purely descriptive, NOT used for matching
        functions: [sub, commands]    # omit entirely for flat commands (rm, curl, ...) -> catch-all
        args:
          anyOf: ["-f", "--force"]    # optional: argv must contain one of these (literal match, no regex/globs)
          noneOf: ["-x"]              # optional: argv must contain none of these (anyOf and/or noneOf)
      options:
        customCheck:                 # optional, delegates to a script (see below)
          path: "./scripts/x.sh"     # exactly one of path/command
          command: "..."
          args: ["..."]
          timeoutSeconds: 10
          when:
            - key: exit
              value: 0
              action: ALLOW | DENY | CONFIRM
      message:
        reason: "{cmd} is ..."       # English -- sent to the AI as permissionDecisionReason
        next: "Ask the user to ..."  # English -- what the AI should do next
```

Key rules to follow:

- **`action` is ALLOW, DENY or CONFIRM**, one per file. CONFIRM is a peer
  value (the user is asked before the command runs), not a modifier on
  ALLOW; there is no `options.confirm`.
- **`customCheck` overrides the policy's own action** when a `when` entry
  matches the script's exit code -- even downgrading a DENY or CONFIRM to
  ALLOW. With no match (or on timeout) the policy's own `action` stands.
  "Deny unless a check passes" is `action: DENY` with a `when` entry whose
  `action` is ALLOW (or CONFIRM). The override is decided per policy,
  before policies are compared.
- **`to.label` is free text and purely descriptive -- it plays no part in
  matching.** Only `functions`/`args` affect which rule a command
  matches. Use `label` as a short note for readers/auditors (e.g.
  `write`, `destructive`, `network`), or omit it; never rely on it to
  distinguish rules.
- **One resource = one tool + one action.** If a resource for that
  tool+action already exists, add a new entry to its `rules:` list instead
  of creating a duplicate file. Only create a new file when the user needs a
  *different* action for the same tool (e.g. `git/allow.yaml` for ALLOW,
  `git/write-confirm.yaml` for CONFIRM, `git/destructive-deny.yaml` for DENY).
- **One command, one policy**: two policies for the same tool must never
  match the same command -- `guard lint` reports an error if they can.
  **Distinguishing by flags** (e.g. "`git push` is CONFIRM, `git push -f`
  is DENY") is two separate resources whose conditions don't overlap: the
  lenient one's rule carries `args.noneOf` naming the dangerous flags, and
  the strict one's rule carries `args.anyOf` with the same flags. See
  `.guard/policies/git/write-confirm.yaml` and
  `.guard/policies/git/push-force-deny.yaml` for a worked example.
- **`message.reason`/`message.next` are English**, not Japanese -- they are
  sent verbatim to Claude as `permissionDecisionReason`, not shown as
  end-user UI text. Use the `{cmd}` placeholder (the only one supported) to
  reference the exact matched command, e.g. `"{cmd} is a destructive
  operation and is denied."`
- Directory convention: `.guard/policies/<tool>/<short-action-or-qualifier>.yaml`
  (e.g. `git/allow.yaml`, `rm/deny.yaml`, `git/push-force-deny.yaml`).

## The project manifest (`.guard/manifest.yaml`)

Path-based rules ("nothing may touch `/cmd`") don't go in a policy file;
they live in the single `GuardManifest` at `.guard/manifest.yaml`. It is
only a fallback: the per-tool policies are checked first, and the manifest
decides only commands none of them matched. See `docs/manifest-schema.md`.

## After writing/editing

1. Run `guard lint` (or `go run . lint` from a source checkout without a
   built binary) and fix every error it reports before telling the user
   you're done. An overlap error ("overlaps with <file>")
   means the two policies need disjoint conditions -- add `args.noneOf`
   to the more lenient one; don't leave it for strictest-wins.
2. Show the user a concise diff (or just the new/changed file content) of
   what changed, and a one-line explanation of the resulting behavior (e.g.
   "`git push` -> CONFIRM, `git push -f`/`--force` -> DENY").
3. If you added or changed a rule for a command the user is likely to run
   again soon, you can sanity-check it with `guard check "<example command>"`
   and mention the result.
