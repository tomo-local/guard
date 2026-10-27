package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"ai-guard-tracker/internal/policy"
)

// painter wraps text in ANSI colors, or passes it through when color is off.
type painter struct{ on bool }

// newPainter resolves mode ("always", "never", anything else = auto) for w.
// Auto colors only a terminal, and never when NO_COLOR is set or TERM=dumb.
func newPainter(w io.Writer, mode string) painter {
	switch mode {
	case "always":
		return painter{on: true}
	case "never":
		return painter{}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return painter{}
	}
	f, ok := w.(*os.File)
	if !ok {
		return painter{}
	}
	info, err := f.Stat()
	return painter{on: err == nil && info.Mode()&os.ModeCharDevice != 0}
}

func (p painter) wrap(code, s string) string {
	if !p.on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p painter) red(s string) string    { return p.wrap("31", s) }
func (p painter) yellow(s string) string { return p.wrap("33", s) }
func (p painter) green(s string) string  { return p.wrap("32", s) }
func (p painter) bold(s string) string   { return p.wrap("1", s) }
func (p painter) dim(s string) string    { return p.wrap("2", s) }

// formatIssues renders issues grouped by file path (sorted), errors before
// warnings within a file, followed by a one-line summary.
func formatIssues(issues []policy.Issue, p painter) string {
	byPath := map[string][]policy.Issue{}
	for _, i := range issues {
		byPath[i.Path] = append(byPath[i.Path], i)
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var b strings.Builder
	var errs, warns int
	for _, path := range paths {
		list := byPath[path]
		sort.SliceStable(list, func(a, c int) bool {
			return list[a].Severity == policy.SeverityError && list[c].Severity != policy.SeverityError
		})

		fmt.Fprintln(&b, p.bold(path))
		for _, i := range list {
			label := fmt.Sprintf("%-7s", string(i.Severity))
			if i.Severity == policy.SeverityError {
				errs++
				label = p.red(label)
			} else {
				warns++
				label = p.yellow(label)
			}
			fmt.Fprintf(&b, "  %s  %s\n", label, i.Message)
			for _, d := range i.Details {
				fmt.Fprintf(&b, "      %s  %s\n", p.dim(fmt.Sprintf("%-10s", d.Label)), d.Value)
			}
		}
		fmt.Fprintln(&b)
	}

	summary := fmt.Sprintf("%s, %s in %s", count(errs, "error"), count(warns, "warning"), count(len(paths), "file"))
	if errs > 0 {
		summary = p.red(summary)
	} else {
		summary = p.yellow(summary)
	}
	fmt.Fprintln(&b, summary)
	return b.String()
}

func count(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
