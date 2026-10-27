package policy

import (
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// KindGuardDBConnections is the `kind:` value of .guard/db/connections.yml.
const KindGuardDBConnections = "GuardDBConnections"

// GuardDBConnections defines the environments (production, staging, ...)
// that database connections belong to, by host and database name. It makes
// no decisions: GuardDBPolicy rules refer to the environments by name.
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

// DBEnvironment matches a connection when it matches Hosts (if set) and
// Databases (if set). Patterns follow path.Match and compare
// case-insensitively; at least one list is required.
type DBEnvironment struct {
	Name      string   `yaml:"name" validate:"required"`
	Hosts     []string `yaml:"hosts,omitempty" validate:"omitempty,dive,required"`
	Databases []string `yaml:"databases,omitempty" validate:"omitempty,dive,required"`
}

func newGuardDBConnections(data []byte, source string, deps Deps) (Policy, error) {
	var c GuardDBConnections
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	c.SourcePath = source
	return &c, nil
}

var _ DBEnvironments = (*GuardDBConnections)(nil)

func (c *GuardDBConnections) Meta() Meta {
	return Meta{Name: c.Metadata.Name, Kind: KindGuardDBConnections, Source: c.SourcePath}
}

// Environments returns the names of every environment the connection
// belongs to, in file order. An empty host means the default local
// connection, "localhost".
func (c *GuardDBConnections) Environments(host, database string) []string {
	if host == "" {
		host = "localhost"
	}
	var out []string
	for _, e := range c.Spec.Environments {
		if e.matches(host, database) {
			out = append(out, e.Name)
		}
	}
	return out
}

func (e DBEnvironment) matches(host, database string) bool {
	if len(e.Hosts) == 0 && len(e.Databases) == 0 {
		return false
	}
	if len(e.Hosts) > 0 && !anyGlob(e.Hosts, host) {
		return false
	}
	if len(e.Databases) > 0 && (database == "" || !anyGlob(e.Databases, database)) {
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

// EnvironmentNames lists the defined environment names.
func (c *GuardDBConnections) EnvironmentNames() []string {
	names := make([]string, 0, len(c.Spec.Environments))
	for _, e := range c.Spec.Environments {
		names = append(names, e.Name)
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
		if len(e.Hosts) == 0 && len(e.Databases) == 0 {
			col.Errorf("spec.environments[%d]: set hosts and/or databases, otherwise nothing can match", i)
		}
		for j, p := range e.Hosts {
			if p != "" && !validGlob(p) {
				col.Errorf("spec.environments[%d].hosts[%d]: %q is not a valid pattern", i, j, p)
			}
		}
		for j, p := range e.Databases {
			if p != "" && !validGlob(p) {
				col.Errorf("spec.environments[%d].databases[%d]: %q is not a valid pattern", i, j, p)
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
