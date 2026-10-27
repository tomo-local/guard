package policy

import (
	"bytes"
	"errors"
	"io"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// KindGuardDBConnections is the `kind:` value of .guard/db/connections.yml.
const KindGuardDBConnections = "GuardDBConnections"

// GuardDBConnections defines the environments (production, staging, ...)
// database connections belong to, and the labels those environments carry.
// It makes no decisions: GuardDBPolicy rules refer to the labels.
type GuardDBConnections struct {
	APIVersion string            `yaml:"apiVersion" validate:"eq=ai-guard/v1"`
	Kind       string            `yaml:"kind" validate:"eq=GuardDBConnections"`
	Metadata   Metadata          `yaml:"metadata"`
	Spec       DBConnectionsSpec `yaml:"spec"`
	SourcePath string            `yaml:"-" validate:"-"`
}

type DBConnectionsSpec struct {
	Environments []DBEnvironment `yaml:"environments" validate:"min=1,dive"`
}

// DBEnvironment is one environment. Labels are what policies refer to. How a
// connection is recognized depends on the way it is made, so each way has
// its own block under the environment: CLI is the command line of psql and
// mysql. An environment needs at least one such block.
type DBEnvironment struct {
	Name   string       `yaml:"name" validate:"required"`
	Labels []string     `yaml:"labels,omitempty" validate:"omitempty,dive,required"`
	CLI    *DBCLIAccess `yaml:"cli,omitempty"`
}

// DBCLIAccess matches a psql/mysql connection when it matches Hosts (if set)
// and Databases (if set). Patterns follow path.Match and compare
// case-insensitively; at least one list is required.
type DBCLIAccess struct {
	Hosts     []string `yaml:"hosts,omitempty" validate:"omitempty,dive,required"`
	Databases []string `yaml:"databases,omitempty" validate:"omitempty,dive,required"`
}

// newGuardDBConnections decodes strictly: an unknown key (a way of
// connecting that isn't supported yet, or a typo) is an error instead of
// being silently ignored.
func newGuardDBConnections(data []byte, source string, deps Deps) (Policy, error) {
	var c GuardDBConnections
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	c.SourcePath = source
	return &c, nil
}

var _ DBEnvironments = (*GuardDBConnections)(nil)

func (c *GuardDBConnections) Meta() Meta {
	return Meta{Name: c.Metadata.Name, Kind: KindGuardDBConnections, Source: c.SourcePath}
}

// Match returns the environments a psql/mysql connection belongs to, in
// file order, and the union of their labels. An empty host means the
// default local connection, "localhost".
func (c *GuardDBConnections) Match(host, database string) DBMatch {
	if host == "" {
		host = "localhost"
	}
	var m DBMatch
	for _, e := range c.Spec.Environments {
		if e.CLI == nil || !e.CLI.matches(host, database) {
			continue
		}
		m.Environments = append(m.Environments, e.Name)
		for _, l := range e.Labels {
			if !contains(m.Labels, l) {
				m.Labels = append(m.Labels, l)
			}
		}
	}
	return m
}

func (a DBCLIAccess) matches(host, database string) bool {
	if len(a.Hosts) == 0 && len(a.Databases) == 0 {
		return false
	}
	if len(a.Hosts) > 0 && !anyGlob(a.Hosts, host) {
		return false
	}
	if len(a.Databases) > 0 && (database == "" || !anyGlob(a.Databases, database)) {
		return false
	}
	return true
}

func anyGlob(patterns []string, s string) bool {
	s = strings.ToLower(s)
	for _, p := range patterns {
		if ok, err := path.Match(strings.ToLower(p), s); err == nil && ok {
			return true
		}
	}
	return false
}

// LabelNames lists every label some environment carries.
func (c *GuardDBConnections) LabelNames() []string {
	var names []string
	for _, e := range c.Spec.Environments {
		for _, l := range e.Labels {
			if !contains(names, l) {
				names = append(names, l)
			}
		}
	}
	return names
}

func (c *GuardDBConnections) Lint() []Issue {
	col := NewIssueCollector(c.SourcePath)
	validateStruct(col, c)

	seen := map[string]bool{}
	for i, e := range c.Spec.Environments {
		if e.Name != "" && seen[e.Name] {
			col.Errorf("spec.environments[%d].name: %q is defined more than once", i, e.Name)
		}
		seen[e.Name] = true

		seenLabel := map[string]bool{}
		for j, l := range e.Labels {
			if l != "" && seenLabel[l] {
				col.Errorf("spec.environments[%d].labels[%d]: %q is listed more than once", i, j, l)
			}
			seenLabel[l] = true
		}

		if e.CLI == nil {
			col.Errorf("spec.environments[%d]: needs at least one way of connecting (cli)", i)
			continue
		}
		if len(e.CLI.Hosts) == 0 && len(e.CLI.Databases) == 0 {
			col.Errorf("spec.environments[%d].cli: set hosts and/or databases, otherwise nothing can match", i)
		}
		for j, p := range e.CLI.Hosts {
			if p != "" && !validGlob(p) {
				col.Errorf("spec.environments[%d].cli.hosts[%d]: %q is not a valid pattern", i, j, p)
			}
		}
		for j, p := range e.CLI.Databases {
			if p != "" && !validGlob(p) {
				col.Errorf("spec.environments[%d].cli.databases[%d]: %q is not a valid pattern", i, j, p)
			}
		}
	}
	return col.Issues()
}

func validGlob(p string) bool {
	_, err := path.Match(p, "")
	return err == nil
}

// lintDBConnectionsSet allows at most one connections file.
func lintDBConnectionsSet(policies []Policy) []Issue {
	var cs []*GuardDBConnections
	for _, p := range policies {
		if c, ok := p.(*GuardDBConnections); ok {
			cs = append(cs, c)
		}
	}
	var issues []Issue
	for _, c := range cs[min(1, len(cs)):] {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     c.SourcePath,
			Message:  "more than one GuardDBConnections",
			Details:  []Detail{{"also", cs[0].SourcePath}},
		})
	}
	return issues
}
