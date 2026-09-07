// Package project describes checkouts laid out in a GOPATH-style source tree
// and discovers them on disk.
package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Depth is the number of path segments below the root at which a project
// lives: {host}/{owner}/{repo}.
const Depth = 3

// Project is a single checkout at Root/Host/Owner/Repo.
type Project struct {
	Root  string `json:"root"`  // /Users/albttx/go/src
	Host  string `json:"host"`  // github.com
	Owner string `json:"owner"` // albttx
	Repo  string `json:"repo"`  // p
}

// Path returns the absolute path of the checkout.
func (p Project) Path() string { return filepath.Join(p.Root, p.Host, p.Owner, p.Repo) }

// Name returns "owner/repo". It is the tmux session name for the project.
func (p Project) Name() string { return p.Owner + "/" + p.Repo }

// Full returns "host/owner/repo".
func (p Project) Full() string { return p.Host + "/" + p.Owner + "/" + p.Repo }

// String implements fmt.Stringer and reports the Full form.
func (p Project) String() string { return p.Full() }

// Scan walks root and returns every project found at exactly
// {host}/{owner}/{repo}.
//
// A directory at that depth is a project when it holds a .git entry that is
// either a directory (a normal clone) or a regular file (a git worktree or
// submodule pointing at a shared object store).
//
// The walk never descends into a project directory: once depth [Depth] is
// reached the directory is pruned whether or not it turned out to be a
// project. That is what keeps Scan fast — vendor, node_modules and target
// trees inside checkouts are never read — and it is also what keeps nested
// sub-repositories such as github.com/albttx/meetings2md/meeting-recorder out
// of the results, since a project is only ever recognised at depth [Depth].
//
// Unreadable directories are skipped rather than aborting the walk, so a
// single permission error does not cost the caller the whole tree.
func Scan(root string) ([]Project, error) {
	return newScanner(root).run()
}

// scanner holds the state for one Scan. onVisit is a test-only seam used to
// assert which paths the walk actually touched; it is nil in production.
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
			// something that is not usable. Anything deeper is skipped.
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
		if len(parts) < Depth {
			// Still above project depth: keep descending.
			return nil
		}

		if isRepo(path) {
			projects = append(projects, Project{
				Root:  s.root,
				Host:  parts[0],
				Owner: parts[1],
				Repo:  parts[2],
			})
		}
		// Prune unconditionally. Nothing at or below a candidate project is a
		// project, so there is never a reason to read it.
		return fs.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", s.root, err)
	}

	Sort(projects)
	return projects, nil
}

// isRepo reports whether dir holds a .git entry that marks a real checkout.
func isRepo(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return fi.IsDir() || fi.Mode().IsRegular()
}

// Sort orders projects case-insensitively by their Full form, in place.
func Sort(projects []Project) {
	slices.SortFunc(projects, func(a, b Project) int {
		af, bf := a.Full(), b.Full()
		if c := strings.Compare(strings.ToLower(af), strings.ToLower(bf)); c != 0 {
			return c
		}
		return strings.Compare(af, bf)
	})
}
