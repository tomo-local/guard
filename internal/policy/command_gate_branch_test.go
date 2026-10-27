package policy

import (
	"reflect"
	"testing"

	"ai-guard-tracker/internal/shellcmd"
)

// fakeRepo is a RepoInfo with a fixed branch and label table.
type fakeRepo struct {
	branch string
	known  bool
	labels map[string][]string
	asked  int
}

func (f *fakeRepo) CurrentBranch() (string, bool)  { f.asked++; return f.branch, f.known }
func (f *fakeRepo) BranchLabels(b string) []string { return f.labels[b] }

func newFakeRepo(branch string) *fakeRepo {
	return &fakeRepo{
		branch: branch,
		known:  branch != "",
		labels: map[string][]string{
			"main":    {LabelMain, LabelProtected},
			"staging": {LabelProtected},
		},
	}
}

func TestTargetBranches(t *testing.T) {
	repo := newFakeRepo("feature")
	cases := []struct {
		name string
		cmd  shellcmd.SimpleCommand
		want []string
	}{
		{"push without refspec -> current", cmdOf("git", "push"), []string{"feature"}},
		{"push remote only -> current", cmdOf("git", "push", "origin"), []string{"feature"}},
		{"push -u remote branch", cmdOf("git", "push", "-u", "origin", "main"), []string{"main"}},
		{"push src:dst", cmdOf("git", "push", "origin", "feature:main"), []string{"main"}},
		{"push HEAD:refs/heads/x", cmdOf("git", "push", "origin", "HEAD:refs/heads/x"), []string{"x"}},
		{"push HEAD -> current", cmdOf("git", "push", "origin", "HEAD"), []string{"feature"}},
		{"push +force refspec", cmdOf("git", "push", "origin", "+a:b"), []string{"b"}},
		{"push delete", cmdOf("git", "push", "origin", ":old"), []string{"old"}},
		{"push several refspecs", cmdOf("git", "push", "origin", "b", "a"), []string{"a", "b"}},
		{"branch -D names", cmdOf("git", "branch", "-D", "x", "y"), []string{"x", "y"}},
		{"branch --delete", cmdOf("git", "branch", "--delete", "x"), []string{"x"}},
		{"branch listing", cmdOf("git", "branch"), nil},
		{"branch create", cmdOf("git", "branch", "newone"), nil},
		{"commit -> current", cmdOf("git", "commit", "-m", "x"), []string{"feature"}},
		{"merge -> current", cmdOf("git", "merge", "main"), []string{"feature"}},
		{"not git", cmdOf("rm", "x"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetBranches(tc.cmd, repo); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targetBranches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTargetBranches_UnknownCurrentBranch(t *testing.T) {
	repo := newFakeRepo("") // e.g. detached HEAD
	if got := targetBranches(cmdOf("git", "push"), repo); got != nil {
		t.Fatalf("expected no target, got %v", got)
	}
	if got := targetBranches(cmdOf("git", "push", "origin", "HEAD"), repo); len(got) != 0 {
		t.Fatalf("expected no target for HEAD without a current branch, got %v", got)
	}
}

func TestBranchCtxMatches(t *testing.T) {
	protected := &LabelsMatch{AnyOf: []string{LabelProtected}}
	notProtected := &LabelsMatch{NoneOf: []string{LabelProtected}}
	mainNotProtected := &LabelsMatch{AnyOf: []string{LabelMain}, NoneOf: []string{LabelProtected}}

	cases := []struct {
		name   string
		branch string
		cond   *LabelsMatch
		cmd    shellcmd.SimpleCommand
		want   bool
	}{
		{"nil condition always matches", "feature", nil, cmdOf("git", "push"), true},
		{"protected, on main", "main", protected, cmdOf("git", "push"), true},
		{"protected, on feature", "feature", protected, cmdOf("git", "push"), false},
		{"not protected, on feature", "feature", notProtected, cmdOf("git", "push"), true},
		{"not protected, on main", "main", notProtected, cmdOf("git", "push"), false},
		{"anyOf and noneOf combine", "main", mainNotProtected, cmdOf("git", "push"), false},
		{"explicit target beats the current branch", "feature", protected, cmdOf("git", "push", "origin", "main"), true},
		{"any target branch may satisfy it", "feature", protected, cmdOf("git", "push", "origin", "feature", "staging"), true},
		{"no target branch -> no match", "", protected, cmdOf("git", "push"), false},
		{"not git -> no match", "main", protected, cmdOf("rm", "x"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &branchCtx{cmd: tc.cmd, repo: newFakeRepo(tc.branch)}
			if got := c.matches(tc.cond); got != tc.want {
				t.Fatalf("matches = %v, want %v", got, tc.want)
			}
		})
	}

	if (&branchCtx{cmd: cmdOf("git", "push")}).matches(protected) {
		t.Fatal("without repository information a branch condition must not match")
	}
}

func TestBranchCtx_AsksRepoOnlyWhenARuleHasABranchCondition(t *testing.T) {
	repo := newFakeRepo("main")
	c := &branchCtx{cmd: cmdOf("git", "push"), repo: repo}
	c.matches(nil)
	if repo.asked != 0 {
		t.Fatalf("repo consulted %d times without a branch condition", repo.asked)
	}
	c.matches(&LabelsMatch{AnyOf: []string{LabelProtected}})
	c.matches(&LabelsMatch{AnyOf: []string{LabelMain}})
	if repo.asked != 1 {
		t.Fatalf("expected the current branch to be looked up once, got %d", repo.asked)
	}
}

func branchRule(functions []string, b *LabelsMatch) Rule {
	return Rule{To: To{Functions: functions, Branch: b}}
}

func TestEvaluate_BranchCondition(t *testing.T) {
	deny := NewCommandGate("git/protected-deny.yaml", Spec{
		Target: Target{Module: ModulePermission, Tool: "git"},
		Action: ActionDeny,
		Rules:  []Rule{branchRule([]string{"push", "commit"}, &LabelsMatch{AnyOf: []string{LabelProtected}})},
	}, Deps{Repo: newFakeRepo("main")})

	if v, ok := deny.Evaluate(cmdOf("git", "commit", "-m", "x")); !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY on a protected branch, got %+v (ok=%v)", v, ok)
	}

	onFeature := NewCommandGate("git/protected-deny.yaml", deny.Spec, Deps{Repo: newFakeRepo("feature")})
	if _, ok := onFeature.Evaluate(cmdOf("git", "commit", "-m", "x")); ok {
		t.Fatal("expected no opinion off the protected branches")
	}
	if v, ok := onFeature.Evaluate(cmdOf("git", "push", "origin", "main")); !ok || v.Action != ActionDeny {
		t.Fatalf("expected DENY for pushing to a protected branch from elsewhere, got %+v (ok=%v)", v, ok)
	}

	noRepo := NewCommandGate("git/protected-deny.yaml", deny.Spec, Deps{})
	if _, ok := noRepo.Evaluate(cmdOf("git", "commit")); ok {
		t.Fatal("expected no opinion without repository information")
	}
}
