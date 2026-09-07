package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/shell"
	"github.com/albttx/p/internal/tmux"
	"github.com/albttx/p/internal/vcs"
)

// recorder is a Runner for both git and tmux that records argv and never
// executes anything.
type recorder struct {
	calls    [][]string
	existing map[string]bool
}

func (r *recorder) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "has-session" {
		if r.existing[strings.TrimPrefix(args[1], "-t=")] {
			return nil
		}
		return os.ErrNotExist
	}
	return nil
}

func (r *recorder) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, r.Run(ctx, name, args...)
}

func (r *recorder) argvs() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// tree builds a small source tree and returns its root.
func tree(t *testing.T, repos ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, r := range repos {
		dir := filepath.Join(root, filepath.FromSlash(r))
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return root
}

// harness runs the command tree with captured output.
type harness struct {
	t      *testing.T
	root   string
	git    *recorder
	tmux   *recorder
	env    map[string]string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newHarness(t *testing.T, repos ...string) *harness {
	t.Helper()
	return &harness{
		t:    t,
		root: tree(t, repos...),
		git:  &recorder{},
		tmux: &recorder{existing: map[string]bool{}},
		env:  map[string]string{},
	}
}

// run invokes p with --code-dir bound to the fixture. Passing it explicitly
// rather than through the environment keeps these tests parallel-safe and
// immune to an ambient $CODE_DIR.
func (h *harness) run(args ...string) error {
	h.t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()

	cmd := New(Params{
		Version: "test",
		CodeDir: h.root,
		Stdout:  &h.stdout,
		Stderr:  &h.stderr,
		Git:     h.git,
		Tmux:    h.tmux,
		Getenv:  func(k string) string { return h.env[k] },
	})
	cmd.ExitErrHandler = func(context.Context, *ucli.Command, error) {}

	full := append([]string{"p", "--" + flagCodeDir, h.root}, args...)
	return cmd.Run(context.Background(), full)
}

// runRaw invokes p with an argv the caller has already assembled.
func (h *harness) runRaw(argv []string) error {
	h.t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()

	cmd := New(Params{
		Version: "test",
		CodeDir: h.root,
		Stdout:  &h.stdout,
		Stderr:  &h.stderr,
		Git:     h.git,
		Tmux:    h.tmux,
		Getenv:  func(k string) string { return h.env[k] },
	})
	cmd.ExitErrHandler = func(context.Context, *ucli.Command, error) {}

	return cmd.Run(context.Background(), argv)
}

// runCompletion drives a shell completion request.
//
// urfave/cli reads os.Args directly to find the token being completed, so it
// has to be set for the duration of the call. That makes these tests
// non-parallel by nature.
func (h *harness) runCompletion(args ...string) error {
	h.t.Helper()

	full := append([]string{"p", "--" + flagCodeDir, h.root}, args...)
	full = append(full, completionFlag)

	saved := os.Args
	os.Args = full
	defer func() { os.Args = saved }()

	return h.runRaw(full)
}

func (h *harness) out() string { return h.stdout.String() }

func (h *harness) lines() []string {
	s := strings.TrimSuffix(h.out(), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

const (
	repoP        = "github.com/albttx/p"
	repoGno      = "github.com/gnolang/gno"
	repoBlogA    = "github.com/albttx/blog"
	repoBlogN    = "github.com/nysa-network/blog"
	repoKontacts = "github.com/albttx/kontacts.dev"
	repoAnsible  = "gitlab.com/nysa/ansible"
)

func allRepos() []string {
	return []string{repoP, repoGno, repoBlogA, repoBlogN, repoKontacts, repoAnsible}
}

func TestNavigateEmitsSentinel(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run("gno"); err != nil {
		t.Fatalf("p gno: %v", err)
	}

	want := shell.SentinelCD + filepath.Join(h.root, "github.com", "gnolang", "gno")
	if got := strings.TrimSpace(h.out()); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", h.stderr.String())
	}
}

// TestNavigateKeepsStdoutCleanOnFailure is the safety property the whole
// sentinel protocol rests on: if the shim ever cds to a diagnostic, the user
// lands somewhere unpredictable.
func TestNavigateKeepsStdoutCleanOnFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string // substring the error must contain
	}{
		{name: "ambiguous", args: []string{"blog"}, want: "ambiguous"},
		{name: "zero match", args: []string{"nonexistent"}, want: "no matching project"},
		{name: "too many arguments", args: []string{"a", "b"}, want: "single query"},
		{name: "unknown flag", args: []string{"--bogus"}, want: "flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			err := h.run(tt.args...)
			if err == nil {
				t.Fatalf("p %v should have failed", tt.args)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if h.out() != "" {
				t.Errorf("stdout = %q, want it empty so the shim cannot cd", h.out())
			}
			if strings.Contains(h.out(), shell.SentinelCD) {
				t.Errorf("stdout leaked the cd sentinel: %q", h.out())
			}
		})
	}
}

func TestNavigateAmbiguityListsCandidates(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	err := h.run("blog")
	if err == nil {
		t.Fatal("p blog should be ambiguous")
	}
	for _, want := range []string{repoBlogA, repoBlogN} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list candidate %q", err, want)
		}
	}
}

func TestNavigateNoArgsShowsHelp(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run(); err != nil {
		t.Fatalf("bare p: %v", err)
	}
	if !strings.Contains(h.out(), "USAGE") {
		t.Errorf("bare p should print help, got %q", h.out())
	}
	if strings.Contains(h.out(), shell.SentinelCD) {
		t.Error("help output must not contain the cd sentinel")
	}
}

// TestNavigateFallsThroughForRepoNamedLikeNothingReserved guards the root
// fallback: "p" is itself a repository name and must not be swallowed.
func TestNavigateResolvesRepoNamedP(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run("p"); err != nil {
		t.Fatalf("p p: %v", err)
	}
	want := shell.SentinelCD + filepath.Join(h.root, "github.com", "albttx", "p")
	if got := strings.TrimSpace(h.out()); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestSelectorsDisambiguateNavigation covers the flags that turn an ambiguous
// query into a usable one without retyping a longer path.
func TestSelectorsDisambiguateNavigation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "--owner", args: []string{"blog", "--owner", "albttx"}, want: repoBlogA},
		{name: "--host", args: []string{"blog", "--host", "github.com", "--owner", "nysa-network"}, want: repoBlogN},
		{name: "--exclude", args: []string{"blog", "--exclude", "nysa-network"}, want: repoBlogA},
		{
			name: "repeated --exclude",
			args: []string{"blog", "--exclude", "nysa-network", "--exclude", "nothing-matches"},
			want: repoBlogA,
		},
		{name: "path --exclude", args: []string{"path", "blog", "--exclude", "albttx"}, want: repoBlogN},
		{name: "path --owner", args: []string{"path", "blog", "--owner", "albttx"}, want: repoBlogA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			want := filepath.Join(h.root, filepath.FromSlash(tt.want))
			got := strings.TrimPrefix(strings.TrimSpace(h.out()), shell.SentinelCD)
			if got != want {
				t.Errorf("p %v = %q, want %q", tt.args, got, want)
			}
		})
	}
}

// TestExcludeCanEliminateEveryCandidate makes sure over-filtering fails
// cleanly rather than navigating somewhere arbitrary.
func TestExcludeCanEliminateEveryCandidate(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	err := h.run("blog", "--exclude", "albttx", "--exclude", "nysa-network")
	if err == nil {
		t.Fatal("excluding every candidate should fail")
	}
	if h.out() != "" {
		t.Errorf("stdout = %q, want empty", h.out())
	}
}

func TestListExclude(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run("list", "--owner", "albttx", "--exclude", "blog", "--exclude", "kontacts"); err != nil {
		t.Fatalf("p list: %v", err)
	}
	if got, want := h.lines(), []string{repoP}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("p list = %v, want %v", got, want)
	}
}

func TestPathPrintsBarePath(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run("path", "gno"); err != nil {
		t.Fatalf("p path gno: %v", err)
	}

	want := filepath.Join(h.root, "github.com", "gnolang", "gno")
	if got := strings.TrimSpace(h.out()); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if strings.Contains(h.out(), shell.SentinelCD) {
		t.Error("p path must not emit the sentinel; it is meant for $(...)")
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "all projects",
			args: []string{"list"},
			want: []string{repoBlogA, repoKontacts, repoP, repoGno, repoBlogN, repoAnsible},
		},
		{
			name: "filtered by host",
			args: []string{"list", "--host", "gitlab.com"},
			want: []string{repoAnsible},
		},
		{
			name: "filtered by owner",
			args: []string{"list", "--owner", "albttx"},
			want: []string{repoBlogA, repoKontacts, repoP},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}
			got := h.lines()
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for _, want := range tt.want {
				if !strings.Contains(h.out(), want+"\n") {
					t.Errorf("output missing %q:\n%s", want, h.out())
				}
			}
		})
	}
}

func TestListPathAndJSON(t *testing.T) {
	t.Parallel()

	t.Run("--path prints absolute paths", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, allRepos()...)
		if err := h.run("list", "--path", "--owner", "gnolang"); err != nil {
			t.Fatalf("p list --path: %v", err)
		}
		want := filepath.Join(h.root, "github.com", "gnolang", "gno")
		if got := strings.TrimSpace(h.out()); got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("--json is valid and complete", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, allRepos()...)
		if err := h.run("list", "--json", "--owner", "gnolang"); err != nil {
			t.Fatalf("p list --json: %v", err)
		}

		var got []jsonProject
		if err := json.Unmarshal(h.stdout.Bytes(), &got); err != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", err, h.out())
		}
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		want := jsonProject{
			Host: "github.com", Owner: "gnolang", Repo: "gno",
			Full: repoGno, Name: "gnolang/gno",
			Path: filepath.Join(h.root, "github.com", "gnolang", "gno"),
		}
		if got[0] != want {
			t.Errorf("json = %+v, want %+v", got[0], want)
		}
	})
}

func TestQueryCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "term lists every match",
			args: []string{"query", "blog"},
			want: []string{repoBlogA, repoBlogN},
		},
		{
			name: "no term lists everything",
			args: []string{"query"},
			want: []string{repoBlogA, repoKontacts, repoP, repoGno, repoBlogN, repoAnsible},
		},
		{
			name: "limit truncates",
			args: []string{"query", "blog", "--limit", "1"},
			want: []string{repoBlogA},
		},
		{
			name: "limit zero is unlimited",
			args: []string{"query", "blog", "--limit", "0"},
			want: []string{repoBlogA, repoBlogN},
		},
		{
			name: "repeated exclude",
			args: []string{"query", "--exclude", "blog", "--exclude", "gno", "--exclude", "kontacts"},
			want: []string{repoP, repoAnsible},
		},
		{
			name: "exclude glob",
			args: []string{"query", "--exclude", "*/blog"},
			want: []string{repoKontacts, repoP, repoGno, repoAnsible},
		},
		{
			name:    "zero match is an error",
			args:    []string{"query", "nonexistent"},
			wantErr: true,
		},
		{
			name:    "everything excluded is an error",
			args:    []string{"query", "--exclude", "github.com", "--exclude", "gitlab.com"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			err := h.run(tt.args...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("p %v error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
			if tt.wantErr {
				if h.out() != "" {
					t.Errorf("stdout = %q, want empty on failure", h.out())
				}
				return
			}

			got := h.lines()
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for _, want := range tt.want {
				if !strings.Contains(h.out(), want+"\n") {
					t.Errorf("output missing %q:\n%s", want, h.out())
				}
			}
		})
	}
}

func TestCloneArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantURL  string
		wantDest string
	}{
		{
			name: "ssh by default", args: []string{"clone", "github.com/albttx/example.com"},
			wantURL: "git@github.com:albttx/example.com.git", wantDest: "github.com/albttx/example.com",
		},
		{
			name: "https flag", args: []string{"clone", "github.com/albttx/example.com", "--https"},
			wantURL: "https://github.com/albttx/example.com.git", wantDest: "github.com/albttx/example.com",
		},
		{
			name: "bare owner/repo", args: []string{"clone", "gnolang/gno2"},
			wantURL: "git@github.com:gnolang/gno2.git", wantDest: "github.com/gnolang/gno2",
		},
		{
			name: "full ssh url", args: []string{"clone", "git@gitlab.com:nysa/newrepo.git"},
			wantURL: "git@gitlab.com:nysa/newrepo.git", wantDest: "gitlab.com/nysa/newrepo",
		},
		{
			name: "https url derives the destination", args: []string{"clone", "https://gitlab.com/nysa/other"},
			wantURL: "git@gitlab.com:nysa/other.git", wantDest: "gitlab.com/nysa/other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			dest := filepath.Join(h.root, filepath.FromSlash(tt.wantDest))
			want := "git clone " + tt.wantURL + " " + dest
			if got := h.git.argvs(); len(got) != 1 || got[0] != want {
				t.Errorf("argv = %v, want [%q]", got, want)
			}
			if got := strings.TrimSpace(h.out()); got != dest {
				t.Errorf("stdout = %q, want the destination %q", got, dest)
			}
			// The parent must exist for git to clone into it.
			if fi, err := os.Stat(filepath.Dir(dest)); err != nil || !fi.IsDir() {
				t.Errorf("parent of %q not created: %v", dest, err)
			}
			// clone is the inert verb: it must never reach tmux, which is
			// what makes it safe to call from a script that cannot attach.
			if len(h.tmux.calls) != 0 {
				t.Errorf("clone touched tmux: %v", h.tmux.argvs())
			}
		})
	}
}

func TestCloneRejectsExisting(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	if err := h.run("clone", "github.com/albttx/p"); err == nil {
		t.Fatal("cloning over an existing checkout should fail")
	}
	if len(h.git.calls) != 0 {
		t.Errorf("git was invoked anyway: %v", h.git.argvs())
	}
}

func TestTmuxArgv(t *testing.T) {
	t.Parallel()

	t.Run("creates missing sessions then attaches", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, repoP, repoKontacts)
		if err := h.run("tmux"); err != nil {
			t.Fatalf("p tmux: %v", err)
		}

		want := []string{
			"tmux has-session -t=albttx/kontacts.dev",
			"tmux new-session -d -s albttx/kontacts.dev -c " + filepath.Join(h.root, "github.com/albttx/kontacts.dev"),
			"tmux has-session -t=albttx/p",
			"tmux new-session -d -s albttx/p -c " + filepath.Join(h.root, "github.com/albttx/p"),
			"tmux attach",
		}
		if got := h.tmux.argvs(); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("argv =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("skips existing sessions", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, repoP, repoKontacts)
		h.tmux.existing["albttx/p"] = true

		if err := h.run("tmux", "--no-attach"); err != nil {
			t.Fatalf("p tmux: %v", err)
		}
		for _, argv := range h.tmux.argvs() {
			if strings.Contains(argv, "new-session") && strings.Contains(argv, "-s albttx/p ") {
				t.Errorf("recreated an existing session: %s", argv)
			}
		}
	})

	t.Run("--no-attach does not attach", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, repoP)
		if err := h.run("tmux", "--no-attach"); err != nil {
			t.Fatalf("p tmux --no-attach: %v", err)
		}
		for _, argv := range h.tmux.argvs() {
			if strings.HasSuffix(argv, "attach") {
				t.Errorf("attached despite --no-attach: %s", argv)
			}
		}
	})

	t.Run("--filter and repeated --exclude narrow the set", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, allRepos()...)
		if err := h.run("tmux", "--no-attach", "--filter", "blog", "--exclude", "nysa-network"); err != nil {
			t.Fatalf("p tmux: %v", err)
		}

		want := []string{
			"tmux has-session -t=albttx/blog",
			"tmux new-session -d -s albttx/blog -c " + filepath.Join(h.root, "github.com/albttx/blog"),
		}
		if got := h.tmux.argvs(); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("argv =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("--dry-run runs nothing", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, repoP, repoKontacts)
		if err := h.run("tmux", "--dry-run"); err != nil {
			t.Fatalf("p tmux --dry-run: %v", err)
		}
		if len(h.tmux.calls) != 0 {
			t.Errorf("--dry-run touched the tmux server: %v", h.tmux.argvs())
		}

		want := []string{
			"tmux has-session -t=albttx/kontacts.dev",
			"tmux new-session -d -s albttx/kontacts.dev -c " + filepath.Join(h.root, "github.com/albttx/kontacts.dev"),
			"tmux has-session -t=albttx/p",
			"tmux new-session -d -s albttx/p -c " + filepath.Join(h.root, "github.com/albttx/p"),
			"tmux attach",
		}
		if got := h.lines(); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("dry run printed\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}

func TestInitCommand(t *testing.T) {
	t.Parallel()

	for _, sh := range shell.Supported() {
		t.Run(sh, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			if err := h.run("init", sh); err != nil {
				t.Fatalf("p init %s: %v", sh, err)
			}
			want, err := shell.Shim(sh, shell.Options{})
			if err != nil {
				t.Fatalf("Shim: %v", err)
			}
			if h.out() != want {
				t.Errorf("p init %s printed\n%s\nwant\n%s", sh, h.out(), want)
			}
		})
	}

	t.Run("unknown shell", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		if err := h.run("init", "powershell"); err == nil {
			t.Fatal("p init powershell should fail")
		}
		if h.out() != "" {
			t.Errorf("stdout = %q, want empty", h.out())
		}
	})

	t.Run("custom binary and function names", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		if err := h.run("init", "zsh", "--bin", "pp", "--func", "j"); err != nil {
			t.Fatalf("p init zsh: %v", err)
		}
		if !strings.Contains(h.out(), "command pp ") || !strings.Contains(h.out(), "j() {") {
			t.Errorf("custom names not honoured:\n%s", h.out())
		}
	})
}

// TestSubcommandNamesDoNotShadowQueries documents the one real collision: a
// repository named after a subcommand is reachable through `p path <name>`.
func TestSubcommandNamesDoNotShadowQueries(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "github.com/someone/list")

	// `p list` is the subcommand, as it must be.
	if err := h.run("list"); err != nil {
		t.Fatalf("p list: %v", err)
	}
	if strings.Contains(h.out(), shell.SentinelCD) {
		t.Error("p list should list, not navigate")
	}

	// The repository is still reachable.
	if err := h.run("path", "list"); err != nil {
		t.Fatalf("p path list: %v", err)
	}
	if got, want := strings.TrimSpace(h.out()), filepath.Join(h.root, "github.com/someone/list"); got != want {
		t.Errorf("p path list = %q, want %q", got, want)
	}
}

// Interface satisfaction is part of the contract these packages advertise.
var (
	_ vcs.Runner  = (*recorder)(nil)
	_ tmux.Runner = (*recorder)(nil)
)
