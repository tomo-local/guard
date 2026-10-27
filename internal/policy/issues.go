package policy

import "fmt"

// Severity classifies a lint Issue: SeverityError fails `guard lint`
// (non-zero exit), SeverityWarning is reported but does not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one problem found while linting a policy set.
type Issue struct {
	Severity Severity
	Path     string
	Message  string   // one-line summary
	Details  []Detail // optional labeled facts, rendered one per line under Message
}

// Detail is one labeled fact about an Issue (e.g. the other file in an
// overlap, an example command, a hint).
type Detail struct {
	Label string
	Value string
}

func (i Issue) String() string {
	s := fmt.Sprintf("%s: [%s] %s", i.Path, i.Severity, i.Message)
	for _, d := range i.Details {
		s += fmt.Sprintf("\n  %s: %s", d.Label, d.Value)
	}
	return s
}

// HasErrors reports whether any issue has error severity.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// IssueCollector accumulates Issues for a single source file, so lint
// rules can report problems without re-deriving an Issue literal (and its
// Path) at every call site.
type IssueCollector struct {
	path   string
	issues []Issue
}

// NewIssueCollector returns a collector that attributes every issue it
// records to path.
func NewIssueCollector(path string) *IssueCollector {
	return &IssueCollector{path: path}
}

// Errorf records a SeverityError issue.
func (c *IssueCollector) Errorf(format string, args ...any) {
	c.issues = append(c.issues, Issue{Severity: SeverityError, Path: c.path, Message: fmt.Sprintf(format, args...)})
}

// Warnf records a SeverityWarning issue.
func (c *IssueCollector) Warnf(format string, args ...any) {
	c.issues = append(c.issues, Issue{Severity: SeverityWarning, Path: c.path, Message: fmt.Sprintf(format, args...)})
}

// Issues returns every issue recorded so far.
func (c *IssueCollector) Issues() []Issue {
	return c.issues
}
