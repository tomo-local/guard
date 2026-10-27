# Wiring `guard` into Claude Code hooks

`guard` doesn't register itself as a Claude Code hook automatically --
`guard init` only prints the snippet below, and never edits
`.claude/settings.json` for you (to avoid clobbering unrelated hook
configuration you may already have).

## What to add

Add this to the project's `.claude/settings.json` (create the file if it
doesn't exist yet):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "guard check" }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "guard record" }]
      }
    ]
  }
}
```

If the file already has a `hooks` key, merge these entries into the
existing `PreToolUse`/`PostToolUse` arrays rather than replacing them.

- **`PreToolUse` → `guard check`**: runs before every Bash command Claude
  Code is about to execute. It evaluates the command against
  `.guard/policies/` and returns `allow`/`deny`/`ask` via
  `hookSpecificOutput.permissionDecision`. This is what actually gates
  commands -- without this entry, `guard` has no effect on Claude's
  behavior.
- **`PostToolUse` → `guard record`**: runs after a Bash command finishes
  and appends what actually happened (command, raw stdout/stderr,
  whether it was interrupted) to `.guard/logs/record.jsonl`. It never
  blocks anything and always exits 0 -- it's purely for later analysis
  (see `guard report`).

## Command resolution

Both snippets assume `guard` is resolvable on `PATH` (e.g. installed via
Homebrew, or `go install`). If you're running from a source checkout
without installing it, point the hook at the built binary instead, e.g.:

```json
{ "type": "command", "command": "/absolute/path/to/ai-guard-tracker/bin/guard check" }
```

(build it first with `go build -o bin/guard .`).

## Custom policy/log locations

If your project keeps `.guard/policies/` or the log files somewhere
other than the defaults, set `GUARD_POLICIES_DIR` / `GUARD_LOG_PATH` /
`GUARD_RECORD_LOG_PATH` in the environment Claude Code's hooks run in,
rather than passing `-rules`/`-log` flags in the hook command itself (see
the main README's env var table). `-flag` still overrides the env var if
you do pass one explicitly in the hook command.

## Verifying it's wired up correctly

```bash
guard check "git reset --hard"
```

should print `DENY ...` and exit non-zero. If the hooks are set up
correctly, asking Claude Code to run that same command in this project
should be blocked (or asked for confirmation, depending on the policy)
before it ever reaches a shell.
