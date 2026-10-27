// Package shellcmd splits a raw shell command line into the individual
// simple commands it invokes, so each one can be evaluated against policy
// independently (e.g. "a && rm -rf /" yields both "a" and "rm -rf /").
package shellcmd

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// SimpleCommand is one command invocation extracted from a (possibly
// compound) shell command line: a tool name (argv[0], path-stripped) and
// its remaining arguments in order.
type SimpleCommand struct {
	Tool string
	Args []string
}

// Decompose parses raw as a shell command line and returns every simple
// command it contains, walking into &&/||/;/pipelines, subshells and
// command substitutions. It never fails: if raw cannot be parsed as valid
// shell syntax, it falls back to a single whitespace-split command so
// callers can still attempt a (conservative) policy match instead of
// crashing. usedFallback reports whether that happened.
func Decompose(raw string) (cmds []SimpleCommand, usedFallback bool) {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(raw), "")
	if err != nil {
		return fallbackSplit(raw), true
	}

	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		words := make([]string, len(call.Args))
		for i, w := range call.Args {
			words[i] = wordLiteral(w)
		}
		cmds = append(cmds, SimpleCommand{
			Tool: path.Base(words[0]),
			Args: words[1:],
		})
		return true
	})

	if len(cmds) == 0 {
		return fallbackSplit(raw), true
	}
	return cmds, false
}

// wordLiteral best-effort renders a syntax.Word to a plain string. Plain
// literals are concatenated verbatim; words containing expansions (vars,
// command substitution, globs, ...) are rendered via the shell printer, so
// matching against them simply won't hit any configured function name and
// the command falls through to the catch-all/default (safe, conservative).
func wordLiteral(w *syntax.Word) string {
	allLit := true
	for _, part := range w.Parts {
		if _, ok := part.(*syntax.Lit); !ok {
			allLit = false
			break
		}
	}

	if allLit {
		var sb strings.Builder
		for _, part := range w.Parts {
			sb.WriteString(part.(*syntax.Lit).Value)
		}
		return sb.String()
	}

	var sb strings.Builder
	printer := syntax.NewPrinter()
	if err := printer.Print(&sb, w); err != nil {
		return ""
	}
	return sb.String()
}

func fallbackSplit(raw string) []SimpleCommand {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	return []SimpleCommand{{
		Tool: path.Base(fields[0]),
		Args: fields[1:],
	}}
}

// Function treats the first argument that doesn't look like a flag as the
// subcommand name. This is a heuristic: it misclassifies flags that take a
// separate value (e.g. "git -C <path> push" sees "<path>" as the function),
// a known v1 limitation documented in the README.
func (c SimpleCommand) Function() string {
	for _, a := range c.Args {
		if a != "" && !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// String renders the command back into a plain "<tool> <args...>" string.
func (c SimpleCommand) String() string {
	if len(c.Args) == 0 {
		return c.Tool
	}
	return c.Tool + " " + strings.Join(c.Args, " ")
}
