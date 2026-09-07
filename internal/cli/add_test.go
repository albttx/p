package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// argvAfterClone returns the tmux argv, which is what `p add` is really about.
func TestAdd(t *testing.T) {
	t.Parallel()

	const (
		newSpec = "github.com/albttx/lol"
		newDest = "github.com/albttx/lol"
	)

	tests := []struct {
		name string
		// existing repos in the fixture tree
		repos []string
		// sessions tmux already knows about
		sessions []string
		// $TMUX, i.e. whether we are already inside tmux
		tmuxEnv  string
		args     []string
		wantGit  []string // exact git argv, DEST is substituted
		wantTmux []string // exact tmux argv, DEST is substituted
	}{
		{
			name: "clones, creates the session and attaches",
			args: []string{"add", newSpec},
			wantGit: []string{
				"git clone git@github.com:albttx/lol.git DEST",
			},
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
				"tmux attach -t=albttx/lol",
			},
		},
		{
			name:    "already on disk: no clone, still ensures and attaches",
			repos:   []string{"github.com/albttx/lol"},
			args:    []string{"add", newSpec},
			wantGit: nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
				"tmux attach -t=albttx/lol",
			},
		},
		{
			name:     "session already exists: no new-session, still attaches",
			repos:    []string{"github.com/albttx/lol"},
			sessions: []string{"albttx/lol"},
			args:     []string{"add", newSpec},
			wantGit:  nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux attach -t=albttx/lol",
			},
		},
		{
			name:    "inside tmux: switch-client instead of attach",
			repos:   []string{"github.com/albttx/lol"},
			tmuxEnv: "/private/tmp/tmux-501/default,12345,0",
			args:    []string{"add", newSpec},
			wantGit: nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
				"tmux switch-client -t=albttx/lol",
			},
		},
		{
			name:     "inside tmux with the session already up",
			repos:    []string{"github.com/albttx/lol"},
			sessions: []string{"albttx/lol"},
			tmuxEnv:  "/private/tmp/tmux-501/default,12345,0",
			args:     []string{"add", newSpec},
			wantGit:  nil,
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux switch-client -t=albttx/lol",
			},
		},
		{
			name:    "--no-attach stops after the session",
			args:    []string{"add", newSpec, "--no-attach"},
			wantGit: []string{"git clone git@github.com:albttx/lol.git DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
			},
		},
		{
			name:    "--no-attach inside tmux does not switch either",
			tmuxEnv: "/private/tmp/tmux-501/default,12345,0",
			args:    []string{"add", newSpec, "--no-attach"},
			wantGit: []string{"git clone git@github.com:albttx/lol.git DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
			},
		},
		{
			name:    "--https",
			args:    []string{"add", newSpec, "--https", "--no-attach"},
			wantGit: []string{"git clone https://github.com/albttx/lol.git DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
			},
		},
		{
			name:    "bare owner/repo defaults the host",
			args:    []string{"add", "albttx/lol", "--no-attach"},
			wantGit: []string{"git clone git@github.com:albttx/lol.git DEST"},
			wantTmux: []string{
				"tmux has-session -t=albttx/lol",
				"tmux new-session -d -s albttx/lol -c DEST",
			},
		},
		{
			name:    "full ssh url derives owner/repo for the session",
			args:    []string{"add", "git@gitlab.com:nysa/lol.git", "--no-attach"},
			wantGit: []string{"git clone git@gitlab.com:nysa/lol.git DEST"},
			wantTmux: []string{
				"tmux has-session -t=nysa/lol",
				"tmux new-session -d -s nysa/lol -c DEST",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, tt.repos...)
			for _, s := range tt.sessions {
				h.tmux.existing[s] = true
			}
			if tt.tmuxEnv != "" {
				h.env["TMUX"] = tt.tmuxEnv
			}

			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			dest := destOf(h, tt.args[1])
			assertArgv(t, "git", h.git.argvs(), subst(tt.wantGit, dest))
			assertArgv(t, "tmux", h.tmux.argvs(), subst(tt.wantTmux, dest))

			// p add moves the user through tmux, never through the sentinel.
			if h.out() != "" {
				t.Errorf("p add wrote to stdout: %q, want empty", h.out())
			}
		})
	}
}

// TestAddDottedSessionName drives a domain-named repository through the whole
// add path, where the dot is both a tmux target metacharacter and a thing a
// naive spec parser mistakes for a host.
func TestAddDottedSessionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    string
		session string
		dest    string
	}{
		{name: "kontacts.dev", spec: "github.com/albttx/kontacts.dev", session: "albttx/kontacts.dev", dest: "github.com/albttx/kontacts.dev"},
		{name: "bare owner/domain", spec: "albttx/0human.company", session: "albttx/0human.company", dest: "github.com/albttx/0human.company"},
		{name: "ssh url", spec: "git@github.com:albttx/l7x.org.git", session: "albttx/l7x.org", dest: "github.com/albttx/l7x.org"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			if err := h.run("add", tt.spec); err != nil {
				t.Fatalf("p add %s: %v", tt.spec, err)
			}

			dest := filepath.Join(h.root, filepath.FromSlash(tt.dest))
			assertArgv(t, "git", h.git.argvs(), []string{
				"git clone " + cloneURLFor(tt.session) + " " + dest,
			})
			assertArgv(t, "tmux", h.tmux.argvs(), []string{
				"tmux has-session -t=" + tt.session,
				"tmux new-session -d -s " + tt.session + " -c " + dest,
				"tmux attach -t=" + tt.session,
			})
		})
	}
}

func TestAddErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "no spec", args: []string{"add"}},
		{name: "two specs", args: []string{"add", "a/b", "c/d"}},
		{name: "unparseable spec", args: []string{"add", "justone"}},
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

// TestAddIsIdempotent runs add twice over the same project: the second run
// must neither clone nor recreate, and must not error.
func TestAddIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	if err := h.run("add", "albttx/lol", "--no-attach"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	// The fake git never creates the directory, so simulate the clone landing.
	dest := filepath.Join(h.root, "github.com", "albttx", "lol")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h.tmux.existing["albttx/lol"] = true
	h.git.calls, h.tmux.calls = nil, nil

	if err := h.run("add", "albttx/lol", "--no-attach"); err != nil {
		t.Fatalf("second add should be a no-op, got: %v", err)
	}
	if len(h.git.calls) != 0 {
		t.Errorf("second add cloned again: %v", h.git.argvs())
	}
	assertArgv(t, "tmux", h.tmux.argvs(), []string{"tmux has-session -t=albttx/lol"})
}

// cloneURLFor rebuilds the expected SSH URL from an owner/repo session name.
func cloneURLFor(session string) string {
	owner, repo, _ := strings.Cut(session, "/")
	return "git@github.com:" + owner + "/" + repo + ".git"
}

// destOf resolves where a spec would be cloned inside the harness tree.
func destOf(h *harness, spec string) string {
	s := spec
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s, "://") {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		host := "github.com"
		if strings.Contains(spec, "gitlab.com") {
			host = "gitlab.com"
		}
		return filepath.Join(h.root, host, parts[0], parts[1])
	default:
		return filepath.Join(h.root, filepath.FromSlash(s))
	}
}

func subst(argv []string, dest string) []string {
	if argv == nil {
		return nil
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = strings.ReplaceAll(a, "DEST", dest)
	}
	return out
}

func assertArgv(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s argv =\n  %s\nwant\n  %s",
			what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
