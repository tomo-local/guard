package policy

import (
	"sort"
	"strings"

	"ai-guard-tracker/internal/shellcmd"
)

// branchCtx evaluates `to.branch` conditions for one command. The target
// branches (and so any git lookup behind RepoInfo) are worked out only when
// some rule actually has a branch condition.
type branchCtx struct {
	cmd     shellcmd.SimpleCommand
	repo    RepoInfo
	done    bool
	targets []string
}

// matches reports whether the command acts on a branch satisfying b: the
// branch has at least one AnyOf label (when set) and none of the NoneOf
// labels. A nil b always matches; without repository information, or when no
// target branch can be determined, a rule with a condition never matches.
func (c *branchCtx) matches(b *LabelsMatch) bool {
	if b == nil {
		return true
	}
	if c.repo == nil {
		return false
	}
	if !c.done {
		c.targets = targetBranches(c.cmd, c.repo)
		c.done = true
	}
	for _, branch := range c.targets {
		labels := c.repo.BranchLabels(branch)
		if (len(b.AnyOf) == 0 || intersects(labels, b.AnyOf)) && !intersects(labels, b.NoneOf) {
			return true
		}
	}
	return false
}

// targetBranches lists the branches a git command acts on:
//   - push: the destination of each refspec after the remote
//     ("src:dst" -> dst, a leading "+" and "refs/heads/" dropped, "HEAD" ->
//     the current branch, ":dst" deletes dst); no refspec means the current branch
//   - branch -d/-D/--delete/-m/-M/--move: the branch names given
//   - any other git command: the current branch
//
// Not tracked: push --all/--mirror, and `git -C <path>`.
func targetBranches(cmd shellcmd.SimpleCommand, repo RepoInfo) []string {
	if cmd.Tool != "git" {
		return nil
	}
	current := func() []string {
		if b, ok := repo.CurrentBranch(); ok {
			return []string{b}
		}
		return nil
	}

	function := cmd.Function()
	positionals := positionalsAfter(cmd.Args, function)

	switch function {
	case "push":
		if len(positionals) < 2 {
			return current()
		}
		var out []string
		for _, spec := range positionals[1:] {
			if b := pushDestination(spec, current); b != "" {
				out = append(out, b)
			}
		}
		return uniqueSorted(out)
	case "branch":
		for _, a := range cmd.Args {
			switch a {
			case "-d", "-D", "--delete", "-m", "-M", "--move":
				return positionals
			}
		}
		return nil
	default:
		return current()
	}
}

// positionalsAfter returns the non-flag arguments that follow function.
func positionalsAfter(args []string, function string) []string {
	var out []string
	seen := false
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		if !seen {
			seen = a == function
			continue
		}
		out = append(out, a)
	}
	return out
}

func pushDestination(spec string, current func() []string) string {
	spec = strings.TrimPrefix(spec, "+")
	if i := strings.Index(spec, ":"); i >= 0 {
		spec = spec[i+1:]
	}
	spec = strings.TrimPrefix(spec, "refs/heads/")
	if spec == "HEAD" {
		if c := current(); len(c) > 0 {
			return c[0]
		}
		return ""
	}
	return spec
}

func uniqueSorted(in []string) []string {
	out := uniqueStrings(in)
	sort.Strings(out)
	return out
}
