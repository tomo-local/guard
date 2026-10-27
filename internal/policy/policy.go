// Package policy holds the domain: what a policy is, and what a single
// policy decides about a command. Each policy kind (see Kinds) lives in
// its own file and implements Policy / CommandEvaluator itself, so adding
// a kind never touches the code that combines policies (internal/engine).
//
// This package depends on nothing outside the domain. Side effects (running
// a customCheck process) come in through the ports declared here and are
// supplied by the composition root as Deps.
package policy

import (
	"time"

	"ai-guard-tracker/internal/shellcmd"
)

// Meta identifies a loaded policy for messages and audit logs.
type Meta struct {
	Name   string
	Kind   string
	Source string // file the policy was loaded from
}

// Policy is the capability every kind has: it can describe itself and
// check its own consistency.
type Policy interface {
	Meta() Meta
	// Lint reports problems within this one policy. Problems that only
	// show up across several policies belong to Kind.LintSet.
	Lint() []Issue
}

// CommandEvaluator is implemented by kinds that judge Bash commands.
// Kinds that cover other ground add their own capability interface; the
// engine picks policies by capability, never by kind.
type CommandEvaluator interface {
	Policy
	// Priority orders evaluation: lower runs first, and the first
	// priority level with a verdict decides. Individual tool policies are
	// 0; manifest-level fallbacks come later.
	Priority() int
	// Evaluate returns this policy's verdict for cmd, or false when the
	// policy has no opinion about it.
	Evaluate(cmd shellcmd.SimpleCommand) (Verdict, bool)
}

// Verdict is one policy's answer for one command. The engine combines
// verdicts from several policies into a Decision.
type Verdict struct {
	Action      Action
	Reason      string
	Next        string
	Label       string       // descriptive only; see To.Label
	Source      string       // file of the policy that produced this verdict
	CustomCheck *CheckResult // set when the matched rule ran options.customCheck
}

// CheckResult records what happened when a rule's options.customCheck was
// executed, for both decision-making and later audit logging.
type CheckResult struct {
	Ran      bool
	ExitCode int
	TimedOut bool
	RunErr   error
	Override Action // empty when no `when` entry matched
	Stdout   string
	Stderr   string
}

// ProcResult is the outcome of running an external process.
type ProcResult struct {
	Stdout   string
	Stderr   string
	TimedOut bool
	ExitCode int
	// RunErr is non-nil only for a failure that isn't an ordinary nonzero
	// exit (e.g. the binary doesn't exist) or a timeout.
	RunErr error
}

// CustomCheckRunner is the port through which a policy runs the external
// process behind options.customCheck. The implementation lives outside the
// domain and must honor timeout.
type CustomCheckRunner interface {
	Run(timeout time.Duration, name string, args []string) ProcResult
}

// Deps are the outside capabilities injected into policies when they are
// built. A nil Runner means customCheck never runs, so a rule falls back
// to its own action.
//
// Root and Cwd are plain values the composition root resolves once: the
// project root (the directory holding .guard/) and the directory commands
// run in. Kinds that match file paths use them to normalize arguments.
type Deps struct {
	Runner CustomCheckRunner
	Root   string
	Cwd    string
	// Repo tells which git repository and branch commands act on. Nil means
	// unknown: rules with a `to.branch` condition then never match.
	Repo RepoInfo
	// DB maps a database connection (host, database) to the names of the
	// environments the project's db/connections.yml puts it in. Nil means no
	// environments are defined: rules with a `to.environment` condition then
	// never match.
	DB DBEnvironments
}

// DBEnvironments is the port through which policies ask which environments
// (production, staging, ...) a database connection belongs to. A host that
// the project doesn't describe belongs to none.
type DBEnvironments interface {
	Environments(host, database string) []string
}

// RepoInfo is the port through which policies learn about the git
// repository the command runs in. Implementations may be lazy: policies
// only ask when a rule has a `to.branch` condition.
type RepoInfo interface {
	// CurrentBranch is the checked-out branch; false when unknown (not a
	// repository, detached HEAD, ...).
	CurrentBranch() (string, bool)
	// BranchLabels are the labels (LabelMain, LabelProtected) the project
	// manifest gives branch in the current repository; nil when the
	// repository isn't described there.
	BranchLabels(branch string) []string
}
