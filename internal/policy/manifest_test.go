package policy

import (
	"strings"
	"testing"

	"ai-guard-tracker/internal/shellcmd"
)

const manifestYAML = `apiVersion: ai-guard/v1
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
          reason: "{cmd} touches /cmd."
          next: "Add a policy."
      - to:
          label: src-dir
          paths:
            anyOf: ["/src/*"]
        action: CONFIRM
  repositoryGate:
    repose: [{name: x}]
`

func loadManifest(t *testing.T, body string) *GuardManifest {
	t.Helper()
	p, err := newGuardManifest([]byte(body), ".guard/manifest.yaml", Deps{Root: "/proj", Cwd: "/proj"})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*GuardManifest)
}

func cmdOf(tool string, args ...string) shellcmd.SimpleCommand {
	return shellcmd.SimpleCommand{Tool: tool, Args: args}
}

func TestManifest_DispatchedByKind(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "manifest.yaml", manifestYAML)
	pol, err := LoadFile(src, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ce, ok := pol.(CommandEvaluator)
	if !ok || ce.Priority() != 100 || pol.Meta().Kind != KindGuardManifest {
		t.Fatalf("unexpected %T / %+v", pol, pol.Meta())
	}
}

func TestManifest_Evaluate(t *testing.T) {
	m := loadManifest(t, manifestYAML)

	v, ok := m.Evaluate(cmdOf("cat", "cmd/a.go"))
	if !ok || v.Action != ActionDeny || v.Label != "cmd-dir" || v.Reason != "cat cmd/a.go touches /cmd." || v.Source != ".guard/manifest.yaml" {
		t.Fatalf("unexpected verdict %+v (ok=%v)", v, ok)
	}

	// A rule's own action overrides the section default; with no message it
	// gets the generic text.
	v, ok = m.Evaluate(cmdOf("touch", "src/a.go"))
	if !ok || v.Action != ActionConfirm || !strings.Contains(v.Reason, "src/a.go") || v.Next == "" {
		t.Fatalf("unexpected verdict %+v (ok=%v)", v, ok)
	}

	// The directory itself is covered.
	if v, ok = m.Evaluate(cmdOf("rm", "-rf", "cmd")); !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY for removing the directory, got %+v (ok=%v)", v, ok)
	}

	for _, c := range []shellcmd.SimpleCommand{cmdOf("ls", "."), cmdOf("cat", "README.md"), cmdOf("git", "status"), cmdOf("ls")} {
		if _, ok := m.Evaluate(c); ok {
			t.Fatalf("expected no opinion for %v", c)
		}
	}
}

func TestManifest_StrictestRuleWins(t *testing.T) {
	m := loadManifest(t, manifestYAML)
	// Touches both /src (CONFIRM) and /cmd (DENY): DENY wins.
	v, ok := m.Evaluate(cmdOf("cp", "src/a.go", "cmd/a.go"))
	if !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY, got %+v (ok=%v)", v, ok)
	}
}

func TestManifest_DefaultActionWhenTargetOmitted(t *testing.T) {
	m := loadManifest(t, `apiVersion: ai-guard/v1
kind: GuardManifest
metadata: {name: m}
spec:
  commandGate:
    rules:
      - to: {paths: {anyOf: ["/cmd/*"]}}
`)
	if v, ok := m.Evaluate(cmdOf("cat", "cmd/a.go")); !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY by default, got %+v (ok=%v)", v, ok)
	}
}

func TestManifest_NoCommandGateNoOpinion(t *testing.T) {
	m := loadManifest(t, "apiVersion: ai-guard/v1\nkind: GuardManifest\nmetadata: {name: m}\nspec: {}\n")
	if _, ok := m.Evaluate(cmdOf("cat", "cmd/a.go")); ok {
		t.Fatal("expected no opinion")
	}
}

func TestManifest_Lint(t *testing.T) {
	if issues := loadManifest(t, manifestYAML).Lint(); HasErrors(issues) || !hasWarningContaining(issues, "repositoryGate") {
		t.Fatalf("expected only the repositoryGate warning, got %v", issues)
	}

	cases := []struct {
		name, body, want string
	}{
		{"bad action", strings.Replace(manifestYAML, "action: CONFIRM", "action: MAYBE", 1), "action"},
		{"empty paths", strings.Replace(manifestYAML, `anyOf: ["/cmd/*"]`, `anyOf: []`, 1), "anyOf"},
		{"missing paths", `apiVersion: ai-guard/v1
kind: GuardManifest
metadata: {name: m}
spec:
  commandGate:
    rules:
      - to: {label: x}
`, "paths"},
		{"bad pattern", strings.Replace(manifestYAML, `"/cmd/*"`, `"/cmd/["`, 1), "not a valid path pattern"},
		{"wrong kind field", strings.Replace(manifestYAML, "kind: GuardManifest", "kind: Nope", 1), "kind"},
		{"no rules", `apiVersion: ai-guard/v1
kind: GuardManifest
metadata: {name: m}
spec:
  commandGate: {rules: []}
`, "rules"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !hasErrorContaining(loadManifest(t, tc.body).Lint(), tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, loadManifest(t, tc.body).Lint())
			}
		})
	}
}

func TestManifest_OnlyOneAllowed(t *testing.T) {
	a := loadManifest(t, manifestYAML)
	b := loadManifest(t, manifestYAML)
	a.SourcePath, b.SourcePath = "a.yaml", "b.yaml"
	issues := lintManifestSet([]Policy{a, b})
	if len(issues) != 1 || issues[0].Path != "b.yaml" || !HasErrors(issues) {
		t.Fatalf("got %v", issues)
	}
	if issues := lintManifestSet([]Policy{a}); len(issues) != 0 {
		t.Fatalf("got %v", issues)
	}
}
