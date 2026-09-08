package projectsearcher

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// projectDepth is the number of path segments below the scan root at which a
// project lives: {host}/{owner}/{repo}.
const projectDepth = 3

// Project is a single repository checkout in a GOPATH-style tree.
//
// The four fields are the path components of the checkout, split at the
// boundaries that matter for addressing it: Root is the tree the project was
// found in, and Host, Owner and Repo are the three segments below it. A
// Project is a value; copying one is free and safe.
type Project struct {
	// Root is the absolute path of the tree this project was found in,
	// for example "/Users/albttx/go/src".
	Root string `json:"root"`
	// Host is the forge the project came from, for example "github.com".
	Host string `json:"host"`
	// Owner is the user or organisation that owns the repository,
	// for example "albttx".
	Owner string `json:"owner"`
	// Repo is the repository name, for example "p". It may itself look like a
	// domain — "kontacts.dev", "example.com" — so never infer anything from a
	// dot in this field.
	Repo string `json:"repo"`
}

// Path returns the absolute path of the checkout, Root/Host/Owner/Repo.
func (p Project) Path() string {
	return filepath.Join(p.Root, p.Host, p.Owner, p.Repo)
}

// Name returns the "owner/repo" form, which is short enough to be readable but
// still unique within a single host.
func (p Project) Name() string { return p.Owner + "/" + p.Repo }

// Full returns the fully qualified "host/owner/repo" form, which is unique
// across an entire tree.
func (p Project) Full() string { return p.Host + "/" + p.Owner + "/" + p.Repo }

// String implements [fmt.Stringer] and reports the same value as [Project.Full].
func (p Project) String() string { return p.Full() }

// Scan walks root and returns every project found at exactly
// {host}/{owner}/{repo}, sorted by [Sort].
//
// A directory at that depth is a project when it contains a .git entry that is
// either a directory (an ordinary clone) or a regular file (a linked worktree
// or a submodule, which record .git as a file pointing at a shared object
// store). Missing the file case silently loses every worktree in the tree.
//
// Scan never descends into a project directory. Once a candidate at project
// depth is reached, the walk prunes there whether or not it turned out to be a
// project. Two consequences follow, and both are the point of this function:
//
//   - It is fast. Vendored trees inside checkouts — node_modules, vendor,
//     target — are never read, so the cost is proportional to the number of
//     owners rather than to the size of the tree. The naive equivalent,
//     "find <root> -name .git -type d -prune", descends into all of them and
//     is three orders of magnitude slower on a large tree.
//   - Nested repositories are excluded. A checkout vendored inside another
//     checkout sits below project depth and so is never reported, which is
//     usually what a caller wants: it is part of its parent, not a project in
//     its own right.
//
// Unreadable directories are skipped rather than aborting the walk, so one
// permission error does not cost the caller the rest of the tree. An error is
// returned only when root itself cannot be walked.
func Scan(root string) ([]Project, error) {
	return newScanner(root).run()
}

// scanner holds the state for one [Scan]. onVisit is a test-only seam used to
// assert which paths the walk actually touched; it is nil in normal use.
type scanner struct {
	root    string
	onVisit func(path string)
}

func newScanner(root string) *scanner {
	return &scanner{root: filepath.Clean(root)}
}

func (s *scanner) run() ([]Project, error) {
	var projects []Project

	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if s.onVisit != nil {
			s.onVisit(path)
		}

		if err != nil {
			// A failure on the root itself is fatal: the caller pointed us at
			// something unusable. Anything deeper is skipped.
			if path == s.root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if path == s.root {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}

		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return fs.SkipDir
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < projectDepth {
			// Still above project depth: keep descending.
			return nil
		}

		if IsRepo(path) {
			projects = append(projects, Project{
				Root:  s.root,
				Host:  parts[0],
				Owner: parts[1],
				Repo:  parts[2],
			})
		}
		// Prune unconditionally. Nothing at or below a candidate project is
		// itself a project, so there is never a reason to read it.
		return fs.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", s.root, err)
	}

	Sort(projects)
	return projects, nil
}

// IsRepo reports whether dir is a checkout, by the same rule [Scan] applies: it
// holds a .git entry that is either a directory or a regular file.
//
// It is exported so that a tool creating a project can check, against a single
// definition, that what it produced will actually be found by [Scan]. Note that
// IsRepo asks only about dir itself and says nothing about its depth, so a
// directory can satisfy IsRepo and still not be reported by Scan.
func IsRepo(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return fi.IsDir() || fi.Mode().IsRegular()
}

// Sort orders projects in place by their [Project.Full] form, comparing
// case-insensitively so that "Allinbits" and "albttx" sort as a human would
// expect. Ties are broken by the case-sensitive comparison, which makes the
// order total and therefore stable across runs.
func Sort(projects []Project) {
	slices.SortFunc(projects, func(a, b Project) int {
		af, bf := a.Full(), b.Full()
		if c := strings.Compare(strings.ToLower(af), strings.ToLower(bf)); c != 0 {
			return c
		}
		return strings.Compare(af, bf)
	})
}
