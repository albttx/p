package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// repos already present in the fixture tree (created with a .git)
		repos []string
		// bare directories present without a .git
		bareDirs []string
		// sessions tmux already holds, in its own stored form
		sessions []string
		tmuxEnv  string
		args     []string

		wantDest string   // relative to the tree root
		wantGit  []string // exact git argv, DEST substituted
		wantTmux []string // exact tmux argv, DEST substituted
	}{
		{
			name:     "bare owner/repo defaults the host to github.com",
			args:     []string{"new", "albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
				"tmux attach -t=albttx/gh-todoist",
			},
		},
		{
			name:     "explicit host/owner/repo is honoured",
			args:     []string{"new", "github.com/albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
				"tmux attach -t=albttx/gh-todoist",
			},
		},
		{
			name:     "gitlab.com is not hardcoded away",
			args:     []string{"new", "gitlab.com/nysa/infra"},
			wantDest: "gitlab.com/nysa/infra",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=nysa/infra",
				"tmux new-session -d -s nysa/infra -c DEST",
				"tmux attach -t=nysa/infra",
			},
		},
		{
			name:     "already a repository: no init, session still ensured",
			repos:    []string{"github.com/albttx/gh-todoist"},
			args:     []string{"new", "albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
				"tmux attach -t=albttx/gh-todoist",
			},
		},
		{
			name:     "directory exists but is not a repository: init runs",
			bareDirs: []string{"github.com/albttx/gh-todoist"},
			args:     []string{"new", "albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
				"tmux attach -t=albttx/gh-todoist",
			},
		},
		{
			name:     "already a repository with its session up: nothing but focus",
			repos:    []string{"github.com/albttx/gh-todoist"},
			sessions: []string{"albttx/gh-todoist"},
			args:     []string{"new", "albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux attach -t=albttx/gh-todoist",
			},
		},
		{
			name:     "--no-attach stops after the session",
			args:     []string{"new", "albttx/gh-todoist", "--no-attach"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
			},
		},
		{
			name:     "inside tmux switches instead of attaching",
			tmuxEnv:  "/private/tmp/tmux-501/default,999,0",
			args:     []string{"new", "albttx/gh-todoist"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
				"tmux switch-client -t=albttx/gh-todoist",
			},
		},
		{
			name:     "--no-attach inside tmux does not switch either",
			tmuxEnv:  "/private/tmp/tmux-501/default,999,0",
			args:     []string{"new", "albttx/gh-todoist", "--no-attach"},
			wantDest: "github.com/albttx/gh-todoist",
			wantGit:  []string{"git init DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/gh-todoist",
				"tmux new-session -d -s albttx/gh-todoist -c DEST",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, tt.repos...)
			for _, d := range tt.bareDirs {
				if err := os.MkdirAll(filepath.Join(h.root, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", d, err)
				}
			}
			for _, s := range tt.sessions {
				h.tmux.existing[s] = true
			}
			if tt.tmuxEnv != "" {
				h.env["TMUX"] = tt.tmuxEnv
			}

			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			dest := filepath.Join(h.root, filepath.FromSlash(tt.wantDest))
			assertArgv(t, "git", h.git.argvs(), subst(tt.wantGit, dest))
			assertArgv(t, "tmux", h.tmux.argvs(), subst(tt.wantTmux, dest))

			// p new never fetches anything.
			for _, argv := range h.git.argvs() {
				if strings.Contains(argv, "clone") {
					t.Errorf("p new issued a clone: %s", argv)
				}
			}
			// It moves the user through tmux, so stdout stays clean.
			if h.out() != "" {
				t.Errorf("p new wrote to stdout: %q, want empty", h.out())
			}
			// The directory must exist afterwards either way.
			if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
				t.Errorf("destination %q was not created: %v", dest, err)
			}
		})
	}
}

// TestNewDottedName covers a project whose name is a domain: tmux must be
// given the rewritten session name while the checkout path keeps its dot.
func TestNewDottedName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	if err := h.run("new", "albttx/foo.dev"); err != nil {
		t.Fatalf("p new albttx/foo.dev: %v", err)
	}

	dest := filepath.Join(h.root, "github.com", "albttx", "foo.dev")
	assertArgv(t, "git", h.git.argvs(), []string{"git init " + dest})
	assertArgv(t, "tmux", h.tmux.argvs(), []string{
		"tmux has-session -t=albttx/foo_dev",
		"tmux new-session -d -s albttx/foo_dev -c " + dest,
		"tmux attach -t=albttx/foo_dev",
	})

	// The directory on disk keeps the dot; only the tmux name is rewritten.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("checkout directory %q missing: %v", dest, err)
	}
	for _, argv := range h.tmux.argvs() {
		if strings.Contains(argv, "-s albttx/foo.dev") || strings.Contains(argv, "-t=albttx/foo.dev") {
			t.Errorf("unsanitised session name reached tmux: %s", argv)
		}
	}
}

// TestNewIsIdempotent runs new twice and asserts the second run neither
// re-inits nor recreates.
func TestNewIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	if err := h.run("new", "albttx/gh-todoist", "--no-attach"); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// The fake git does not create .git, so simulate what a real init leaves.
	dest := filepath.Join(h.root, "github.com", "albttx", "gh-todoist")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h.tmux.existing["albttx/gh-todoist"] = true
	h.git.calls, h.tmux.calls = nil, nil

	if err := h.run("new", "albttx/gh-todoist", "--no-attach"); err != nil {
		t.Fatalf("second run should be a no-op, got: %v", err)
	}
	if len(h.git.calls) != 0 {
		t.Errorf("second run re-ran git: %v", h.git.argvs())
	}
	assertArgv(t, "tmux", h.tmux.argvs(), []string{"tmux has-session -t=albttx/gh-todoist"})
}

func TestNewErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "no spec", args: []string{"new"}},
		{name: "two specs", args: []string{"new", "a/b", "c/d"}},
		{name: "unparseable spec", args: []string{"new", "justone"}},
		{name: "too many segments", args: []string{"new", "a/b/c/d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			if err := h.run(tt.args...); err == nil {
				t.Fatalf("p %v should have failed", tt.args)
			}
			if h.out() != "" {
				t.Errorf("stdout = %q, want empty", h.out())
			}
			if len(h.git.calls)+len(h.tmux.calls) != 0 {
				t.Errorf("ran commands anyway: git=%v tmux=%v", h.git.argvs(), h.tmux.argvs())
			}
		})
	}
}
