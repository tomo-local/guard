package cmd

import (
	"os/exec"
	"reflect"
	"testing"

	"ai-guard-tracker/internal/policy"
)

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/name.git":          "owner/name",
		"git@github.com:owner/name":              "owner/name",
		"https://github.com/owner/name.git":      "owner/name",
		"https://github.com/owner/name/":         "owner/name",
		"https://user@github.com/owner/name":     "owner/name",
		"ssh://git@github.com/owner/name.git":    "owner/name",
		"ssh://git@github.com:22/owner/name.git": "owner/name",
		"git@gitlab.com:group/sub/name.git":      "group/sub/name",
		"https://gitlab.com/group/sub/name":      "group/sub/name",
		"  git@github.com:owner/name.git\n":      "owner/name",
		"":                                       "",
		"not a url":                              "",
		"https://github.com":                     "",
		"https://github.com/onlyowner":           "",
		"/local/path/without/host":               "",
	}
	for in, want := range cases {
		if got := repoNameFromURL(in); got != want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "feature")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")

	m := &policy.GuardManifest{Spec: policy.ManifestSpec{RepositoryGate: &policy.RepositoryGateSection{
		Repose: []policy.RepoSpec{{Name: "owner/repo", Main: "main", Protected: []string{"main", "feature"}}},
	}}}
	g := &gitRepo{cwd: dir, manifest: m}

	if b, ok := g.CurrentBranch(); !ok || b != "feature" {
		t.Fatalf("CurrentBranch = %q, %v", b, ok)
	}
	if got := g.BranchLabels("main"); !reflect.DeepEqual(got, []string{policy.LabelMain, policy.LabelProtected}) {
		t.Fatalf("BranchLabels(main) = %v", got)
	}
	if got := g.BranchLabels("other"); got != nil {
		t.Fatalf("BranchLabels(other) = %v", got)
	}

	// A repository the manifest doesn't describe has no labels.
	g2 := &gitRepo{cwd: dir, manifest: &policy.GuardManifest{Spec: policy.ManifestSpec{RepositoryGate: &policy.RepositoryGateSection{
		Repose: []policy.RepoSpec{{Name: "someone/else", Protected: []string{"feature"}}},
	}}}}
	if got := g2.BranchLabels("feature"); got != nil {
		t.Fatalf("expected no labels for an undescribed repository, got %v", got)
	}
}

func TestGitRepo_NotARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	g := &gitRepo{cwd: t.TempDir(), manifest: &policy.GuardManifest{}}
	if _, ok := g.CurrentBranch(); ok {
		t.Fatal("expected no branch outside a repository")
	}
	if got := g.BranchLabels("main"); got != nil {
		t.Fatalf("expected no labels, got %v", got)
	}
}

func TestGitRepo_DetachedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
	runGit(t, dir, "checkout", "-q", "--detach")
	g := &gitRepo{cwd: dir, manifest: &policy.GuardManifest{}}
	if _, ok := g.CurrentBranch(); ok {
		t.Fatal("a detached HEAD has no current branch")
	}
}
