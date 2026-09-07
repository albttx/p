// Package query filters and resolves projects from a user-supplied term.
package query

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/albttx/p/internal/project"
)

// ErrNotFound is returned when a term matches no project.
var ErrNotFound = errors.New("no matching project")

// AmbiguousError reports a term that matched more than one project. Its
// message lists the candidates so callers can print it verbatim to stderr.
type AmbiguousError struct {
	Term       string
	Candidates []project.Project
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous query %q: %d matches", e.Term, len(e.Candidates))
	for _, p := range e.Candidates {
		b.WriteString("\n  ")
		b.WriteString(p.Full())
	}
	return b.String()
}

// Options narrow a candidate set before matching.
type Options struct {
	// Host keeps only projects on this host. Empty means any host.
	Host string
	// Owner keeps only projects with this owner. Empty means any owner.
	Owner string
	// Exclude drops projects matching any of these patterns. A pattern
	// containing *, ? or [ is matched as a glob against both the
	// host/owner/repo and owner/repo forms; otherwise it is a case-insensitive
	// substring of host/owner/repo.
	Exclude []string
	// Limit caps the number of results. Zero means unlimited.
	Limit int
}

// Filter applies Host, Owner and Exclude, then truncates to Limit. The input
// slice is not modified.
func Filter(projects []project.Project, opts Options) []project.Project {
	out := make([]project.Project, 0, len(projects))
	for _, p := range projects {
		if opts.Host != "" && !strings.EqualFold(p.Host, opts.Host) {
			continue
		}
		if opts.Owner != "" && !strings.EqualFold(p.Owner, opts.Owner) {
			continue
		}
		if excluded(p, opts.Exclude) {
			continue
		}
		out = append(out, p)
	}
	return truncate(out, opts.Limit)
}

// Match returns the projects matching term, using the first tier of the
// resolution order that yields any result:
//
//  1. exact host/owner/repo
//  2. exact owner/repo
//  3. exact repo
//  4. case-insensitive substring of repo
//  5. case-insensitive substring of owner/repo
//
// An empty term matches every project.
func Match(projects []project.Project, term string) []project.Project {
	term = strings.TrimSpace(term)
	if term == "" {
		return append([]project.Project(nil), projects...)
	}

	lower := strings.ToLower(term)
	tiers := []func(p project.Project) bool{
		func(p project.Project) bool { return p.Full() == term },
		func(p project.Project) bool { return p.Name() == term },
		func(p project.Project) bool { return p.Repo == term },
		func(p project.Project) bool { return strings.Contains(strings.ToLower(p.Repo), lower) },
		func(p project.Project) bool { return strings.Contains(strings.ToLower(p.Name()), lower) },
	}

	for _, match := range tiers {
		var hits []project.Project
		for _, p := range projects {
			if match(p) {
				hits = append(hits, p)
			}
		}
		if len(hits) > 0 {
			return hits
		}
	}
	return nil
}

// Search filters projects with opts and then matches term against what
// remains. Limit is applied last, so it caps the matches rather than the
// candidate pool.
func Search(projects []project.Project, term string, opts Options) []project.Project {
	limit := opts.Limit
	opts.Limit = 0
	return truncate(Match(Filter(projects, opts), term), limit)
}

// Resolve returns the single project a term identifies.
//
// It returns [ErrNotFound] when nothing matches and an [*AmbiguousError]
// listing the candidates when more than one does. Callers must keep stdout
// clean in both cases: the shell shim cds to whatever stdout holds.
func Resolve(projects []project.Project, term string, opts Options) (project.Project, error) {
	opts.Limit = 0
	matches := Search(projects, term, opts)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return project.Project{}, fmt.Errorf("%w for %q", ErrNotFound, term)
	default:
		return project.Project{}, &AmbiguousError{Term: term, Candidates: matches}
	}
}

// excluded reports whether p matches any of the patterns.
func excluded(p project.Project, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if matchesPattern(p, pattern) {
			return true
		}
	}
	return false
}

func matchesPattern(p project.Project, pattern string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		lower := strings.ToLower(pattern)
		for _, candidate := range []string{p.Full(), p.Name(), p.Repo} {
			if ok, err := path.Match(lower, strings.ToLower(candidate)); err == nil && ok {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(p.Full()), strings.ToLower(pattern))
}

func truncate(projects []project.Project, limit int) []project.Project {
	if limit > 0 && len(projects) > limit {
		return projects[:limit]
	}
	return projects
}
