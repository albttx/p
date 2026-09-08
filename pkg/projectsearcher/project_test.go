package projectsearcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAccessors(t *testing.T) {
	t.Parallel()

	p := Project{Root: "/Users/albttx/go/src", Host: "github.com", Owner: "albttx", Repo: "p"}

	if got, want := p.Path(), filepath.Join("/Users/albttx/go/src", "github.com", "albttx", "p"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if got, want := p.Name(), "albttx/p"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := p.Full(), "github.com/albttx/p"; got != want {
		t.Errorf("Full() = %q, want %q", got, want)
	}
}

// mkRepo creates a directory at rel below root with a .git marker. When
// asFile is true the marker is a regular file, the way git records a linked
// worktree or a submodule.
func mkRepo(t *testing.T, root, rel string, asFile bool) {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	git := filepath.Join(dir, ".git")
	if asFile {
		if err := os.WriteFile(git, []byte("gitdir: /elsewhere/.git/worktrees/wt\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", git, err)
		}
		return
	}
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", git, err)
	}
}

func mkDir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
}

// fixture builds a tree that mirrors the shapes found in the real source
// tree: plain clones, a worktree recorded as a .git file, sub-repositories
// vendored inside a checkout, a deep node_modules tree, a directory that is
// not a checkout at all, and a dot-directory.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Canonical projects.
	mkRepo(t, root, "github.com/albttx/p", false)
	mkRepo(t, root, "github.com/albttx/sysphera", false)
	mkRepo(t, root, "github.com/albttx/webapp", false)
	mkRepo(t, root, "github.com/gnolang/gno", false)
	mkRepo(t, root, "gitlab.com/nysa/ansible", false)

	// A linked worktree: .git is a regular file, still a real checkout.
	mkRepo(t, root, "github.com/albttx/kontacts.dev", true)

	// Sub-repositories nested inside checkouts. None of these is a project.
	mkRepo(t, root, "github.com/albttx/sysphera/frontend", false)
	mkRepo(t, root, "github.com/albttx/meetings2md/meeting-recorder", false)
	mkRepo(t, root, "github.com/lgtm-solutions/hq/packages/agent-store/upstream", false)
	mkRepo(t, root, "github.com/allinbits/infrastructure/networks/gno/test7", false)
	mkRepo(t, root, "github.com/albttx/zed-hcl/grammars/hcl", false)

	// meetings2md and zed-hcl themselves have no .git, exactly like the real
	// tree, so they must not be reported either.

	// A deep node_modules tree inside a checkout, with a tripwire .git at the
	// bottom. Walking it is the bug this package exists to avoid.
	deep := "github.com/albttx/webapp/node_modules"
	for _, seg := range []string{"a", "b", "c", "d", "e", "f"} {
		deep += "/" + seg
		mkDir(t, root, deep)
	}
	mkRepo(t, root, deep+"/tripwire", false)

	// Not a checkout: a directory at project depth with no .git.
	mkDir(t, root, "github.com/albttx/scratch")

	// Dot-directories are skipped wholesale.
	mkRepo(t, root, "github.com/.cache/owner/repo", false)

	// An owner directory with nothing in it, and a loose file at the root.
	mkDir(t, root, "github.com/emptyowner")
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	return root
}

func TestScan(t *testing.T) {
	t.Parallel()

	root := fixture(t)

	var visited []string
	s := newScanner(root)
	s.onVisit = func(path string) { visited = append(visited, path) }

	got, err := s.run()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	want := []string{
		"github.com/albttx/kontacts.dev", // .git file worktree
		"github.com/albttx/p",
		"github.com/albttx/sysphera",
		"github.com/albttx/webapp",
		"github.com/gnolang/gno",
		"gitlab.com/nysa/ansible",
	}

	var full []string
	for _, p := range got {
		full = append(full, p.Full())
		if p.Root != root {
			t.Errorf("project %s has Root %q, want %q", p.Full(), p.Root, root)
		}
	}

	if len(full) != len(want) {
		t.Fatalf("Scan() returned %d projects %v, want %d %v", len(full), full, len(want), want)
	}
	for i := range want {
		if full[i] != want[i] {
			t.Errorf("Scan()[%d] = %q, want %q (full result %v)", i, full[i], want[i], full)
		}
	}
}

// TestScanNeverDescendsIntoProjects is the performance contract, asserted
// structurally rather than by timing: once a directory at project depth is
// reached the walk stops, so no path inside any checkout is ever visited.
func TestScanNeverDescendsIntoProjects(t *testing.T) {
	t.Parallel()

	root := fixture(t)

	var visited []string
	s := newScanner(root)
	s.onVisit = func(path string) { visited = append(visited, path) }

	if _, err := s.run(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	for _, path := range visited {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("Rel(%q): %v", path, err)
		}
		if strings.Contains(filepath.ToSlash(rel), "node_modules") {
			t.Errorf("walk descended into vendored tree: %s", rel)
		}
		if rel == "." {
			continue
		}
		if depth := len(strings.Split(filepath.ToSlash(rel), "/")); depth > projectDepth {
			t.Errorf("walk visited %s at depth %d, want at most %d", rel, depth, projectDepth)
		}
	}

	// The tripwire .git buried under node_modules must not have produced a
	// project, and neither must any other nested checkout.
	for _, bad := range []string{"node_modules", "meeting-recorder", "frontend", "upstream", "test7", "hcl"} {
		for _, path := range visited {
			if strings.Contains(path, "/"+bad+"/") {
				t.Errorf("walk visited nested path containing %q: %s", bad, path)
			}
		}
	}
}

func TestScanErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing root", func(t *testing.T) {
		t.Parallel()
		if _, err := Scan(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
			t.Fatal("Scan() on a missing root should return an error")
		}
	})

	t.Run("empty root", func(t *testing.T) {
		t.Parallel()
		got, err := Scan(t.TempDir())
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Scan() = %v, want no projects", got)
		}
	})

	t.Run("unreadable directory is skipped not fatal", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}

		root := t.TempDir()
		mkRepo(t, root, "github.com/albttx/p", false)
		locked := filepath.Join(root, "github.com", "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

		got, err := Scan(root)
		if err != nil {
			t.Fatalf("Scan() error = %v, want the unreadable directory to be skipped", err)
		}
		if len(got) != 1 || got[0].Full() != "github.com/albttx/p" {
			t.Errorf("Scan() = %v, want just github.com/albttx/p", got)
		}
	})
}

func TestSortIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	projects := []Project{
		{Host: "github.com", Owner: "albttx", Repo: "zed-hcl"},
		{Host: "github.com", Owner: "Allinbits", Repo: "infrastructure"},
		{Host: "github.com", Owner: "albttx", Repo: "0human.company"},
	}
	Sort(projects)

	want := []string{
		"github.com/albttx/0human.company",
		"github.com/albttx/zed-hcl",
		"github.com/Allinbits/infrastructure",
	}
	for i, p := range projects {
		if p.Full() != want[i] {
			t.Errorf("Sort()[%d] = %q, want %q", i, p.Full(), want[i])
		}
	}
}

// TestIsRepo covers the exported form of the rule Scan applies, which is what
// a tool creating a project checks against to know Scan will find it.
func TestIsRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkRepo(t, root, "clone", false)    // .git directory
	mkRepo(t, root, "worktree", true)  // .git regular file
	mkDir(t, root, "plain")            // no .git at all
	mkDir(t, root, "nested/deep/repo") // depth is not IsRepo's concern
	mkRepo(t, root, "nested/deep/repo", false)

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "git directory", dir: "clone", want: true},
		{name: "git file worktree", dir: "worktree", want: true},
		{name: "no git entry", dir: "plain", want: false},
		{name: "missing directory", dir: "does-not-exist", want: false},
		{name: "IsRepo ignores depth", dir: "nested/deep/repo", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRepo(filepath.Join(root, filepath.FromSlash(tt.dir))); got != tt.want {
				t.Errorf("IsRepo(%s) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

// TestIsRepoAgreesWithScan pins the two together: anything Scan reports must
// satisfy IsRepo, or a caller using IsRepo to decide whether to git init would
// make a project Scan cannot see.
func TestIsRepoAgreesWithScan(t *testing.T) {
	t.Parallel()

	root := fixture(t)
	projects, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("fixture produced no projects")
	}
	for _, p := range projects {
		if !IsRepo(p.Path()) {
			t.Errorf("Scan reported %s but IsRepo says it is not a repository", p.Full())
		}
	}
}
