package policy

import (
	"fmt"
	"strings"
	"unicode"

	"ai-guard-tracker/internal/shellcmd"
)

// sqlUnknown is the keyword of a command whose SQL can't be read from the
// command line (a script file, standard input, an interactive session).
const sqlUnknown = "UNKNOWN"

// sqlKeywords returns the keyword (SELECT, DROP, ...) of every statement a
// psql/mysql command runs, as written on its command line: the -c/--command
// values of psql and the -e/--execute values of mysql. A command with no
// readable SQL yields a single sqlUnknown.
func sqlKeywords(cmd shellcmd.SimpleCommand) []string {
	var texts []string
	switch cmd.Tool {
	case "psql":
		_, multi, _ := splitFlags(cmd.Args, "hpUdfcoFRPLTv", psqlLongFlags)
		texts = append(multi["-c"], multi["--command"]...)
	case "mysql":
		_, multi, _ := splitFlags(cmd.Args, "hPuDSe", mysqlLongFlags)
		texts = append(multi["-e"], multi["--execute"]...)
	}

	var keywords []string
	for _, text := range texts {
		for _, stmt := range splitStatements(text) {
			if k := statementKeyword(stmt); k != "" {
				keywords = append(keywords, k)
			}
		}
	}
	if len(keywords) == 0 {
		return []string{sqlUnknown}
	}
	return keywords
}

// splitStatements splits SQL text at the semicolons that are not inside a
// quoted string, a comment, or a $tag$ ... $tag$ block.
func splitStatements(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}

	r := []rune(s)
	for i := 0; i < len(r); i++ {
		end := i
		switch c := r[i]; {
		case c == '\'' || c == '"' || c == '`':
			end = quotedEnd(r, i, c)
		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			end = lineCommentEnd(r, i)
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			end = blockCommentEnd(r, i)
		case c == '$':
			if tag, ok := dollarTag(r, i); ok {
				end = dollarEnd(r, i, tag)
			}
		case c == ';':
			flush()
			continue
		}
		cur.WriteString(string(r[i : end+1]))
		i = end
	}
	flush()
	return out
}

// quotedEnd returns the index of the quote closing the string opened at
// r[i] with q (the last index if it never closes). A backslash escapes the
// next character.
func quotedEnd(r []rune, i int, q rune) int {
	for j := i + 1; j < len(r); j++ {
		if r[j] == '\\' {
			j++
			continue
		}
		if r[j] == q {
			return j
		}
	}
	return len(r) - 1
}

// lineCommentEnd returns the index of the newline ending the "--" comment at
// r[i] (the last index if there is none).
func lineCommentEnd(r []rune, i int) int {
	for j := i; j < len(r); j++ {
		if r[j] == '\n' {
			return j
		}
	}
	return len(r) - 1
}

// blockCommentEnd returns the index of the "/" closing the "/*" comment at r[i].
func blockCommentEnd(r []rune, i int) int {
	for j := i + 2; j+1 < len(r); j++ {
		if r[j] == '*' && r[j+1] == '/' {
			return j + 1
		}
	}
	return len(r) - 1
}

// dollarEnd returns the index of the last rune of the closing tag of the
// dollar-quoted block opened by tag at r[i].
func dollarEnd(r []rune, i int, tag string) int {
	t := []rune(tag)
	for j := i + len(t); j+len(t) <= len(r); j++ {
		if string(r[j:j+len(t)]) == tag {
			return j + len(t) - 1
		}
	}
	return len(r) - 1
}

// dollarTag reports the $tag$ (or $$) opening at r[i].
func dollarTag(r []rune, i int) (string, bool) {
	j := i + 1
	for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_') {
		j++
	}
	if j < len(r) && r[j] == '$' {
		return string(r[i : j+1]), true
	}
	return "", false
}

// sqlWords lists the identifier-like words of a statement in order, outside
// quotes and comments. A word is letters, digits, underscores and $; a
// leading backslash (psql meta-commands such as \copy) belongs to the word.
func sqlWords(stmt string) []string {
	var words []string
	r := []rune(stmt)
	for i := 0; i < len(r); i++ {
		switch c := r[i]; {
		case c == '\'' || c == '"' || c == '`':
			i = quotedEnd(r, i, c)
		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			i = lineCommentEnd(r, i)
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			i = blockCommentEnd(r, i)
		case c == '$':
			if tag, ok := dollarTag(r, i); ok {
				i = dollarEnd(r, i, tag)
			}
		case c == '\\' || isWordRune(c):
			j := i + 1
			for j < len(r) && isWordRune(r[j]) {
				j++
			}
			words = append(words, string(r[i:j]))
			i = j - 1
		}
	}
	return words
}

func isWordRune(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_'
}

// dmlRank orders the data-changing keywords a WITH statement may hide; the
// strictest one stands for the statement.
var dmlRank = map[string]int{"INSERT": 1, "MERGE": 2, "UPDATE": 3, "DELETE": 4}

var explainOptions = map[string]bool{
	"ANALYZE": true, "ANALYSE": true, "VERBOSE": true, "COSTS": true, "SETTINGS": true,
	"BUFFERS": true, "WAL": true, "TIMING": true, "SUMMARY": true, "FORMAT": true,
	"TRUE": true, "FALSE": true, "ON": true, "OFF": true, "TEXT": true, "XML": true,
	"JSON": true, "YAML": true, "TREE": true,
}

// statementKeyword is the upper-cased first word of the statement, with two
// refinements: WITH stands for the strictest data-changing keyword in the
// statement (or SELECT), and EXPLAIN ANALYZE stands for the statement it
// executes. "" for a statement with no words (e.g. only comments).
func statementKeyword(stmt string) string {
	return keywordOfWords(sqlWords(stmt))
}

func keywordOfWords(words []string) string {
	if len(words) == 0 {
		return ""
	}
	first := strings.ToUpper(words[0])
	switch first {
	case "WITH":
		best, rank := "SELECT", 0
		for _, w := range words[1:] {
			if r := dmlRank[strings.ToUpper(w)]; r > rank {
				best, rank = strings.ToUpper(w), r
			}
		}
		return best
	case "EXPLAIN":
		j, analyze := 1, false
		for j < len(words) && explainOptions[strings.ToUpper(words[j])] {
			if u := strings.ToUpper(words[j]); u == "ANALYZE" || u == "ANALYSE" {
				analyze = true
			}
			j++
		}
		if analyze && j < len(words) {
			return keywordOfWords(words[j:])
		}
		return "EXPLAIN"
	}
	return first
}

// sqlRule is the common shape of a rule that judges SQL statements, shared
// by GuardDBPolicy rules and the manifest's sqlGate rules.
type sqlRule struct {
	Label     string
	Labels    []string // connection labels it applies to; none = any connection
	Functions []string // statement keywords; none = every statement (catch-all)
	Action    Action
	Message   *Message
}

// pickSQLRule chooses the rule for one statement the way a CommandGatePolicy
// chooses one for a command: among the rules whose labels fit the connection,
// the first one that lists the statement's keyword wins, otherwise the first
// catch-all (a rule with no functions) applies.
func pickSQLRule(rules []sqlRule, keyword string, connLabels []string) (sqlRule, bool) {
	fits := func(r sqlRule) bool {
		return len(r.Labels) == 0 || intersects(r.Labels, connLabels)
	}
	for _, r := range rules {
		if len(r.Functions) > 0 && fits(r) && containsFold(r.Functions, keyword) {
			return r, true
		}
	}
	for _, r := range rules {
		if len(r.Functions) == 0 && fits(r) {
			return r, true
		}
	}
	return sqlRule{}, false
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// evaluateSQL judges every statement of cmd against rules and returns the
// strictest result (the first statement on a tie). defaultAction applies to
// a matched rule that names no action; where, when set, names the
// environments in the default message.
func evaluateSQL(cmd shellcmd.SimpleCommand, rules []sqlRule, defaultAction Action, connLabels []string, source, where string) (Verdict, bool) {
	var (
		found   bool
		best    sqlRule
		bestAct Action
		bestKw  string
	)
	for _, kw := range sqlKeywords(cmd) {
		r, ok := pickSQLRule(rules, kw, connLabels)
		if !ok {
			continue
		}
		act := r.Action
		if act == "" {
			act = defaultAction
		}
		if act == "" {
			act = ActionDeny
		}
		if !found || ActionRank(act) > ActionRank(bestAct) {
			found, best, bestAct, bestKw = true, r, act, kw
		}
	}
	if !found {
		return Verdict{}, false
	}

	v := Verdict{Action: bestAct, Label: best.Label, Source: source}
	if best.Message != nil {
		v.Reason = renderMessageTemplate(best.Message.Reason, cmd)
		v.Next = renderMessageTemplate(best.Message.Next, cmd)
	}
	if v.Reason == "" {
		v.Reason = fmt.Sprintf("%s runs a %s statement%s.", cmd.String(), bestKw, where)
	}
	if v.Next == "" {
		v.Next = "Confirm with the user before running it."
	}
	return v, true
}
