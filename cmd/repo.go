package cmd

import (
	"strings"
	"sync"
	"time"

	"ai-guard-tracker/internal/policy"
	"ai-guard-tracker/internal/procexec"
)

// gitRepo is the outside implementation of policy.RepoInfo: it asks git
// about the repository in cwd, and takes branch labels from the manifest.
// git runs at most once, on first use, so commands that never reach a
// `to.branch` condition don't pay for it.
type gitRepo struct {
	cwd      string
	manifest *policy.GuardManifest

	once   sync.Once
	name   string // owner/name from the origin URL; "" when unknown
	branch string // current branch; "" when unknown
}

func (g *gitRepo) load() {
	g.once.Do(func() {
		g.name = repoNameFromURL(g.git("remote", "get-url", "origin"))
		if b := g.git("rev-parse", "--abbrev-ref", "HEAD"); b != "HEAD" {
			g.branch = b
		}
	})
}

// git runs `git -C cwd args...` and returns its trimmed stdout, or "" when
// it fails.
func (g *gitRepo) git(args ...string) string {
	r := procexec.Run(5*time.Second, "git", append([]string{"-C", g.cwd}, args...), nil)
	if r.RunErr != nil || r.TimedOut || r.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

func (g *gitRepo) CurrentBranch() (string, bool) {
	g.load()
	return g.branch, g.branch != ""
}

func (g *gitRepo) BranchLabels(branch string) []string {
	g.load()
	if g.name == "" || g.manifest == nil {
		return nil
	}
	return g.manifest.BranchLabels(g.name, branch)
}

// repoNameFromURL turns a git remote URL into the repository's path on its
// host ("owner/name"): it accepts https/ssh/git URLs and scp-style
// "git@host:owner/name.git", with or without ".git". "" when it can't.
func repoNameFromURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	var path string
	if i := strings.Index(url, "://"); i >= 0 {
		rest := url[i+3:]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return ""
		}
		path = rest[slash+1:]
	} else if colon := strings.Index(url, ":"); colon >= 0 {
		path = url[colon+1:]
	} else {
		return ""
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if !strings.Contains(path, "/") {
		return ""
	}
	return path
}
