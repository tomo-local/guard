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
    repose:
      - name: owner/repo
        main: main
        protected: [main, staging, "docs/*"]
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
	if issues := loadManifest(t, manifestYAML).Lint(); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	withSQL := manifestYAML + "  sqlGate:\n    target: {action: DENY}\n"
	if issues := loadManifest(t, withSQL).Lint(); HasErrors(issues) || !hasWarningContaining(issues, "sqlGate") {
		t.Fatalf("expected only the sqlGate warning, got %v", issues)
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
		{"repo name without owner", strings.Replace(manifestYAML, "name: owner/repo", "name: repo", 1), "owner/name"},
		{"repo name with a space", strings.Replace(manifestYAML, "name: owner/repo", `name: "owner/re po"`, 1), "owner/name"},
		{"repo name missing", strings.Replace(manifestYAML, "      - name: owner/repo\n", "      - name: \"\"\n", 1), "name"},
		{"duplicate repo", strings.Replace(manifestYAML, "        protected:", "        protected:", 1) + "      - name: owner/repo\n", "more than once"},
		{"bad branch pattern", strings.Replace(manifestYAML, `"docs/*"`, `"docs/["`, 1), "not a valid branch pattern"},
		{"empty protected entry", strings.Replace(manifestYAML, `"docs/*"`, `""`, 1), "protected"},
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

func TestManifest_BranchLabels(t *testing.T) {
	m := loadManifest(t, manifestYAML)

	cases := []struct {
		repo, branch string
		want         []string
	}{
		{"owner/repo", "main", []string{LabelMain, LabelProtected}},
		{"owner/repo", "staging", []string{LabelProtected}},
		{"owner/repo", "docs/guide", []string{LabelProtected}},
		{"owner/repo", "docs/a/b", nil}, // "*" stays within one segment
		{"owner/repo", "feature", nil},
		{"owner/repo", "", nil},
		{"other/repo", "main", nil}, // repository not described
	}
	for _, tc := range cases {
		got := m.BranchLabels(tc.repo, tc.branch)
		if len(got) != len(tc.want) {
			t.Fatalf("BranchLabels(%q, %q) = %v, want %v", tc.repo, tc.branch, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("BranchLabels(%q, %q) = %v, want %v", tc.repo, tc.branch, got, tc.want)
			}
		}
	}

	if got := loadManifest(t, "apiVersion: ai-guard/v1\nkind: GuardManifest\nmetadata: {name: m}\nspec: {}\n").BranchLabels("owner/repo", "main"); got != nil {
		t.Fatalf("a manifest without repositoryGate labels nothing, got %v", got)
	}
}
