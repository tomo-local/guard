package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const gateYAML = `apiVersion: ai-guard/v1
kind: CommandGatePolicy
metadata:
  name: git-allow
spec:
  target:
    module: permission
    tool: git
  action: ALLOW
  rules:
    - to:
        functions: [status]
`

func TestLoadDir_DispatchesByKind(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "git/allow.yaml", gateYAML)

	got, err := LoadDir(dir, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(got))
	}
	if _, ok := got[0].(CommandEvaluator); !ok {
		t.Fatalf("expected a CommandEvaluator, got %T", got[0])
	}
	m := got[0].Meta()
	if m.Kind != KindCommandGate || m.Name != "git-allow" || m.Source != src {
		t.Fatalf("unexpected Meta %+v", m)
	}
}

func TestLoadDir_UnknownKindLoadsAsLintErrorOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "allow.yaml", gateYAML)
	writeFile(t, dir, "manifest.yaml", "apiVersion: ai-guard/v1\nkind: GuardConfig\nspec: {}\n")
	writeFile(t, dir, "nokind.yaml", "apiVersion: ai-guard/v1\n")

	got, err := LoadDir(dir, Deps{})
	if err != nil {
		t.Fatalf("an unknown kind must not fail the whole load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 policies, got %d", len(got))
	}

	evaluators := 0
	for _, p := range got {
		if _, ok := p.(CommandEvaluator); ok {
			evaluators++
			continue
		}
		issues := p.Lint()
		if len(issues) != 1 || !HasErrors(issues) {
			t.Fatalf("expected one error for %s, got %v", p.Meta().Source, issues)
		}
	}
	if evaluators != 1 {
		t.Fatalf("only the known kind should evaluate commands, got %d evaluators", evaluators)
	}
}

func TestLoadDir_BadYAMLIsALoadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", "kind: [unterminated\n")
	if _, err := LoadDir(dir, Deps{}); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestLoadDir_MissingDirIsEmpty(t *testing.T) {
	got, err := LoadDir(filepath.Join(t.TempDir(), "nope"), Deps{})
	if err != nil || len(got) != 0 {
		t.Fatalf("expected an empty result and no error, got %v, %v", got, err)
	}
}

func TestLoadDirForLint_BadYAMLBecomesAnIssue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "allow.yaml", gateYAML)
	writeFile(t, dir, "bad1.yaml", "kind: [unterminated\n")
	writeFile(t, dir, "bad2.yaml", "kind: CommandGatePolicy\nspec: 3\n")

	got, err := LoadDirForLint(dir, Deps{})
	if err != nil {
		t.Fatalf("lint load must not abort on a broken file: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected all 3 files, got %d", len(got))
	}
	broken := 0
	for _, p := range got {
		if _, ok := p.(CommandEvaluator); ok {
			continue
		}
		if !HasErrors(p.Lint()) {
			t.Fatalf("expected an error for %s", p.Meta().Source)
		}
		broken++
	}
	if broken != 2 {
		t.Fatalf("expected 2 broken placeholders, got %d", broken)
	}
}
