package projectsearcher

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrNotFound is returned by [Resolve] when a term matches no project. Test
// for it with [errors.Is]; it is wrapped with the term that failed.
var ErrNotFound = errors.New("no matching project")

// AmbiguousError reports a term that matched more than one project.
//
// Its Error message lists every candidate on its own line, so a command-line
// caller can print it verbatim and give the user something actionable.
type AmbiguousError struct {
	// Term is the query that was ambiguous.
	Term string
	// Candidates are the projects the term matched, in the order [Match]
	// returned them. There are always at least two.
	Candidates []Project
}

// Error implements the error interface, listing the term and every candidate.
func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous query %q: %d matches", e.Term, len(e.Candidates))
	for _, p := range e.Candidates {
		b.WriteString("\n  ")
		b.WriteString(p.Full())
	}
	return b.String()
}

// Options narrows a set of projects before matching. The zero value applies no
// narrowing at all and is the right thing to pass when you want everything.
type Options struct {
	// Host keeps only projects on this host, compared case-insensitively.
	// Empty means any host.
	Host string
	// Owner keeps only projects with this owner, compared case-insensitively.
	// Empty means any owner.
	Owner string
	// Exclude drops any project matching one or more of these patterns.
	//
	// A pattern containing '*', '?' or '[' is treated as a glob (see
	// [path.Match]) and tested against the host/owner/repo, owner/repo and
	// repo forms. Any other pattern is a case-insensitive substring of
	// host/owner/repo. Empty patterns are ignored.
	Exclude []string
	// Limit caps how many projects are returned. Zero means no limit.
	//
	// Limit truncates, it does not rank: the projects kept are the first ones
	// in the existing order, so pair it with sorted input if which ones you
	// get matters.
	Limit int
}

// Filter applies Host, Owner and Exclude, then truncates to Limit. The input
// slice is not modified and the result is a new slice.
func Filter(projects []Project, opts Options) []Project {
	out := make([]Project, 0, len(projects))
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

// Match returns the projects a term identifies, using the first of these tiers
// that matches anything at all:
//
//  1. exact host/owner/repo
//  2. exact owner/repo
//  3. exact repo
//  4. case-insensitive substring of repo
//  5. case-insensitive substring of owner/repo
//
// The search stops at the first tier with any result, so a term that is an
// exact repository name never also drags in everything containing it as a
// substring. That is what lets a short term like "gno" mean the repository
// named gno rather than the fifteen projects whose owner happens to contain
// those letters.
//
// An empty term matches every project. The result preserves the input order.
func Match(projects []Project, term string) []Project {
	term = strings.TrimSpace(term)
	if term == "" {
		return append([]Project(nil), projects...)
	}

	lower := strings.ToLower(term)
	tiers := []func(p Project) bool{
		func(p Project) bool { return p.Full() == term },
		func(p Project) bool { return p.Name() == term },
		func(p Project) bool { return p.Repo == term },
		func(p Project) bool { return strings.Contains(strings.ToLower(p.Repo), lower) },
		func(p Project) bool { return strings.Contains(strings.ToLower(p.Name()), lower) },
	}

	for _, match := range tiers {
		var hits []Project
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

// Search narrows projects with opts and then matches term against what
// remains.
//
// Options.Limit is applied last, so it caps the matches rather than the pool
// that matching ran over. Searching with a limit of one therefore yields the
// best match, not an arbitrary project that happened to survive filtering.
func Search(projects []Project, term string, opts Options) []Project {
	limit := opts.Limit
	opts.Limit = 0
	return truncate(Match(Filter(projects, opts), term), limit)
}

// Resolve returns the single project a term identifies.
//
// It returns [ErrNotFound] when nothing matches, and an [*AmbiguousError]
// listing the candidates when more than one does. Resolve deliberately takes
// no [Options]: narrowing is a separate decision, so compose it when you need
// it, which also keeps the ambiguity report honest about what was considered.
//
//	p, err := projectsearcher.Resolve(
//		projectsearcher.Filter(all, projectsearcher.Options{Owner: "gnolang"}),
//		"blog",
//	)
//
// Take care not to pass an Options.Limit into that Filter: truncating the
// candidates first would turn a genuine ambiguity into a confident wrong
// answer.
func Resolve(projects []Project, term string) (Project, error) {
	matches := Match(projects, term)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Project{}, fmt.Errorf("%w for %q", ErrNotFound, term)
	default:
		return Project{}, &AmbiguousError{Term: term, Candidates: matches}
	}
}

// excluded reports whether p matches any of the patterns.
func excluded(p Project, patterns []string) bool {
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

// matchesPattern applies one Exclude pattern to a project.
func matchesPattern(p Project, pattern string) bool {
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

// truncate caps a result set, treating a non-positive limit as unlimited.
func truncate(projects []Project, limit int) []Project {
	if limit > 0 && len(projects) > limit {
		return projects[:limit]
	}
	return projects
}
