package policy

import (
	"path"
	"path/filepath"
	"strings"
)

// commandPaths turns a command's arguments into project-relative,
// slash-separated paths. Every argument that doesn't start with "-" is a
// candidate. Relative ones are resolved against cwd (against root when cwd
// is unknown), then everything is made relative to root. Candidates outside
// the project, and the root itself, are dropped.
func commandPaths(args []string, root, cwd string) []string {
	var out []string
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		if p, ok := projectRelative(a, root, cwd); ok {
			out = append(out, p)
		}
	}
	return out
}

func projectRelative(arg, root, cwd string) (string, bool) {
	abs := arg
	if !filepath.IsAbs(arg) {
		base := cwd
		if base == "" {
			base = root
		}
		if base == "" {
			// No location known at all: treat the argument as root-relative.
			return cleanRelative(filepath.ToSlash(filepath.Clean(arg)))
		}
		abs = filepath.Join(base, arg)
	}
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	if err != nil {
		return "", false
	}
	return cleanRelative(filepath.ToSlash(rel))
}

func cleanRelative(rel string) (string, bool) {
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// matchPath reports whether the project-relative path rel is covered by
// pattern. Patterns are anchored at the project root (a leading "/" is
// optional). "*" matches within one path segment, "**" as a whole segment
// matches any number of segments, and the other metacharacters are those of
// path.Match. A path is also covered when it is a directory above what the
// pattern covers (below the root): "/cmd/*" covers "cmd", so removing the
// whole directory is caught too.
func matchPath(pattern, rel string) bool {
	segs := splitPath(pattern)
	ps := splitPath(rel)
	if len(segs) == 0 || len(ps) == 0 {
		return false
	}
	return matchSegments(segs, ps) || prefixSegments(segs, ps)
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func matchSegments(segs, ps []string) bool {
	if len(segs) == 0 {
		return len(ps) == 0
	}
	if segs[0] == "**" {
		for i := 0; i <= len(ps); i++ {
			if matchSegments(segs[1:], ps[i:]) {
				return true
			}
		}
		return false
	}
	if len(ps) == 0 {
		return false
	}
	ok, err := path.Match(segs[0], ps[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(segs[1:], ps[1:])
}

// prefixSegments reports whether ps is a proper ancestor directory of paths
// the pattern covers. Only the pattern's leading literal segments (those
// before the first wildcard) count, so "/cmd/*" covers "cmd" but a pattern
// like "/**/*.go" doesn't cover every path as a potential directory.
func prefixSegments(segs, ps []string) bool {
	literal := 0
	for literal < len(segs) && !strings.ContainsAny(segs[literal], "*?[\\") {
		literal++
	}
	if len(ps) == 0 || len(ps) >= len(segs) || len(ps) > literal {
		return false
	}
	for i := range ps {
		if ps[i] != segs[i] {
			return false
		}
	}
	return true
}

// validPattern reports whether every segment of pattern is a well-formed
// path.Match pattern.
func validPattern(pattern string) bool {
	segs := splitPath(pattern)
	if len(segs) == 0 {
		return false
	}
	for _, s := range segs {
		if s == "**" {
			continue
		}
		if _, err := path.Match(s, ""); err != nil {
			return false
		}
	}
	return true
}
