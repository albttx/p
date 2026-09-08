package tmux

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records argv and answers has-session probes from a set of
// sessions it pretends already exist.
type fakeRunner struct {
	existing  map[string]bool
	calls     [][]string
	failNew   error
	alwaysErr error
}

func newFakeRunner(existing ...string) *fakeRunner {
	f := &fakeRunner{existing: map[string]bool{}}
	for _, s := range existing {
		f.existing[s] = true
	}
	return f
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))

	if f.alwaysErr != nil {
		return f.alwaysErr
	}
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "has-session":
		// Mirror tmux: a target is "-t=<name>", exact match, and tmux stores
		// names with '.' and ':' rewritten to '_'.
		target := strings.TrimPrefix(args[1], "-t=")
		if f.existing[SessionName(target)] {
			return nil
		}
		return errors.New("can't find session")
	case "new-session":
		return f.failNew
	}
	return nil
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, f.Run(ctx, name, args...)
}

func (f *fakeRunner) argvs() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTarget(t *testing.T) {
	t.Parallel()

	// The "=" prefix is what makes tmux match the name literally instead of
	// running it through fnmatch, which matters for every session name below.
	tests := []struct{ session, want string }{
		{"albttx/p", "-t=albttx/p"},
		{"albttx/kontacts.dev", "-t=albttx/kontacts_dev"},
		{"albttx/0human.company", "-t=albttx/0human_company"},
		{"albttx/albttx.tech", "-t=albttx/albttx_tech"},
		{"albttx/l7x.org", "-t=albttx/l7x_org"},
	}
	for _, tt := range tests {
		if got := Target(tt.session); got != tt.want {
			t.Errorf("Target(%q) = %q, want %q", tt.session, got, tt.want)
		}
	}
}

func TestEnsureSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		existing    []string
		session     string
		dir         string
		wantCreated bool
		wantArgv    []string
	}{
		{
			name:        "creates a missing session",
			session:     "albttx/p",
			dir:         "/Users/albttx/go/src/github.com/albttx/p",
			wantCreated: true,
			wantArgv: []string{
				"tmux has-session -t=albttx/p",
				"tmux new-session -d -s albttx/p -c /Users/albttx/go/src/github.com/albttx/p",
			},
		},
		{
			name:        "does not recreate an existing session",
			existing:    []string{"albttx/p"},
			session:     "albttx/p",
			dir:         "/Users/albttx/go/src/github.com/albttx/p",
			wantCreated: false,
			wantArgv:    []string{"tmux has-session -t=albttx/p"},
		},
		{
			name:        "dotted session name, missing",
			session:     "albttx/kontacts.dev",
			dir:         "/src/github.com/albttx/kontacts.dev",
			wantCreated: true,
			wantArgv: []string{
				"tmux has-session -t=albttx/kontacts_dev",
				"tmux new-session -d -s albttx/kontacts_dev -c /src/github.com/albttx/kontacts.dev",
			},
		},
		{
			name:        "dotted session name, already present",
			existing:    []string{"albttx/0human_company"},
			session:     "albttx/0human.company",
			dir:         "/src/github.com/albttx/0human.company",
			wantCreated: false,
			wantArgv:    []string{"tmux has-session -t=albttx/0human_company"},
		},
		{
			name: "a dotted name must not match a different session by prefix",
			// tmux would treat "albttx/l7x.org" as session "albttx/l7x",
			// window "org" without the "=" prefix. The probe must miss.
			existing:    []string{"albttx/l7x"},
			session:     "albttx/l7x.org",
			dir:         "/src/github.com/albttx/l7x.org",
			wantCreated: true,
			wantArgv: []string{
				"tmux has-session -t=albttx/l7x_org",
				"tmux new-session -d -s albttx/l7x_org -c /src/github.com/albttx/l7x.org",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := newFakeRunner(tt.existing...)
			client := Client{Runner: runner}

			created, err := client.EnsureSession(context.Background(), tt.session, tt.dir)
			if err != nil {
				t.Fatalf("EnsureSession() error = %v", err)
			}
			if created != tt.wantCreated {
				t.Errorf("EnsureSession() created = %v, want %v", created, tt.wantCreated)
			}
			if got := runner.argvs(); !equal(got, tt.wantArgv) {
				t.Errorf("argv =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tt.wantArgv, "\n  "))
			}
		})
	}
}

func TestEnsureSessionPropagatesCreateFailure(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	runner.failNew = errors.New("tmux: no server")

	_, err := Client{Runner: runner}.EnsureSession(context.Background(), "albttx/p", "/src")
	if err == nil {
		t.Fatal("EnsureSession() should surface a new-session failure")
	}
	if !strings.Contains(err.Error(), "albttx/p") {
		t.Errorf("error %q should name the session", err)
	}
}

func TestAttach(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	if err := (Client{Runner: runner}).Attach(context.Background()); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if got, want := runner.argvs(), []string{"tmux attach"}; !equal(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestClientCustomBin(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner()
	client := Client{Runner: runner, Bin: "/opt/homebrew/bin/tmux"}
	if _, err := client.EnsureSession(context.Background(), "albttx/p", "/src"); err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	for _, call := range runner.calls {
		if call[0] != "/opt/homebrew/bin/tmux" {
			t.Errorf("binary = %q, want /opt/homebrew/bin/tmux", call[0])
		}
	}
}

func TestDryRunner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	client := Client{Runner: DryRunner{W: &buf}}

	for _, s := range []struct{ name, dir string }{
		{"albttx/p", "/src/github.com/albttx/p"},
		{"albttx/kontacts.dev", "/src/github.com/albttx/kontacts.dev"},
	} {
		if _, err := client.EnsureSession(context.Background(), s.name, s.dir); err != nil {
			t.Fatalf("EnsureSession() error = %v", err)
		}
	}
	if err := client.Attach(context.Background()); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}

	want := []string{
		"tmux has-session -t=albttx/p",
		"tmux new-session -d -s albttx/p -c /src/github.com/albttx/p",
		"tmux has-session -t=albttx/kontacts_dev",
		"tmux new-session -d -s albttx/kontacts_dev -c /src/github.com/albttx/kontacts.dev",
		"tmux attach",
	}
	got := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if !equal(got, want) {
		t.Errorf("dry run output =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestQuoteArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "nothing to quote",
			argv: []string{"tmux", "new-session", "-d", "-s", "albttx/kontacts_dev", "-c", "/src/a-b_c.d"},
			want: "tmux new-session -d -s albttx/kontacts_dev -c /src/a-b_c.d",
		},
		{
			name: "spaces are quoted",
			argv: []string{"tmux", "new-session", "-c", "/src/my project"},
			want: "tmux new-session -c '/src/my project'",
		},
		{
			name: "single quotes are escaped",
			argv: []string{"tmux", "-s", "it's"},
			want: `tmux -s 'it'\''s'`,
		},
		{
			name: "empty argument",
			argv: []string{"tmux", ""},
			want: "tmux ''",
		},
		{
			name: "shell metacharacters are quoted",
			argv: []string{"tmux", "-s", "a;rm -rf /"},
			want: "tmux -s 'a;rm -rf /'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := quoteArgv(tt.argv); got != tt.want {
				t.Errorf("quoteArgv(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}

func TestFocus(t *testing.T) {
	t.Parallel()

	// "tmux attach" fails when the caller is already inside a session, so the
	// choice between attach and switch-client is not cosmetic.
	tests := []struct {
		name     string
		session  string
		inTmux   bool
		wantArgv []string
	}{
		{
			name: "outside tmux attaches", session: "albttx/p", inTmux: false,
			wantArgv: []string{"tmux attach -t=albttx/p"},
		},
		{
			name: "inside tmux switches the client", session: "albttx/p", inTmux: true,
			wantArgv: []string{"tmux switch-client -t=albttx/p"},
		},
		{
			name: "dotted name outside tmux", session: "albttx/kontacts.dev", inTmux: false,
			wantArgv: []string{"tmux attach -t=albttx/kontacts_dev"},
		},
		{
			name: "dotted name inside tmux", session: "albttx/l7x.org", inTmux: true,
			wantArgv: []string{"tmux switch-client -t=albttx/l7x_org"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := newFakeRunner()
			if err := (Client{Runner: runner}).Focus(context.Background(), tt.session, tt.inTmux); err != nil {
				t.Fatalf("Focus() error = %v", err)
			}
			if got := runner.argvs(); !equal(got, tt.wantArgv) {
				t.Errorf("argv = %v, want %v", got, tt.wantArgv)
			}
		})
	}
}

func TestFocusPropagatesFailure(t *testing.T) {
	t.Parallel()

	failing := &fakeRunner{existing: map[string]bool{}, alwaysErr: errors.New("tmux: no server running")}
	for _, inTmux := range []bool{true, false} {
		err := Client{Runner: failing}.Focus(context.Background(), "albttx/kontacts.dev", inTmux)
		if err == nil {
			t.Fatalf("Focus(inTmux=%v) should surface the failure", inTmux)
		}
		if !strings.Contains(err.Error(), "albttx/kontacts.dev") {
			t.Errorf("error %q should name the session", err)
		}
	}
}

// TestSessionName pins the rewrite tmux performs in session_check_name(),
// verified against tmux 3.6a on a private socket.
func TestSessionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single dot", input: "albttx/kontacts.dev", want: "albttx/kontacts_dev"},
		{name: "colon", input: "albttx/a:b", want: "albttx/a_b"},
		{name: "dot and colon together", input: "a.b:c", want: "a_b_c"},
		{name: "multiple dots", input: "gnolang/docs.gno.land", want: "gnolang/docs_gno_land"},
		{name: "three dots", input: "gnolang/www.gno.land", want: "gnolang/www_gno_land"},
		{name: "leading dot is not special", input: ".leading", want: "_leading"},
		{name: "trailing dot is not special", input: "trailing.", want: "trailing_"},
		{name: "only dots", input: "...", want: "___"},

		// Everything else is stored verbatim; over-sanitising would break the
		// match against sessions tmux already holds.
		{name: "plain name unchanged", input: "albttx/p", want: "albttx/p"},
		{name: "slash unchanged", input: "owner/repo", want: "owner/repo"},
		{name: "underscore unchanged", input: "already_safe", want: "already_safe"},
		{name: "hyphen unchanged", input: "nysa-network/ansible", want: "nysa-network/ansible"},
		{name: "space unchanged", input: "has space", want: "has space"},
		{name: "dollar unchanged", input: "dollar$sign", want: "dollar$sign"},
		{name: "glob chars unchanged", input: "star*glob[1]", want: "star*glob[1]"},
		{name: "percent unchanged", input: "pct%s", want: "pct%s"},
		{name: "empty passes through for tmux to reject", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SessionName(tt.input); got != tt.want {
				t.Errorf("SessionName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSessionNameIsIdempotent(t *testing.T) {
	t.Parallel()

	// Client sanitises at several points; applying the rule twice must not
	// differ from applying it once.
	for _, in := range []string{"albttx/kontacts.dev", "a.b:c", "albttx/p", ""} {
		once := SessionName(in)
		if twice := SessionName(once); twice != once {
			t.Errorf("SessionName(SessionName(%q)) = %q, want %q", in, twice, once)
		}
	}
}

// TestDottedSessionNameReachesEveryTmuxSubcommand is the regression test for
// the bug where p could neither find nor create a session for any of the 19
// projects whose name contains a dot.
//
// tmux stores "albttx/kontacts.dev" as "albttx/kontacts_dev". Before the fix,
// has-session asked for the dotted name and got "can't find pane: dev", so p
// concluded no session existed and tried to create one, which then failed as a
// duplicate. Every subcommand that names a session must use the stored form.
func TestDottedSessionNameReachesEveryTmuxSubcommand(t *testing.T) {
	t.Parallel()

	const (
		logical   = "albttx/kontacts.dev"
		stored    = "albttx/kontacts_dev"
		directory = "/Users/albttx/go/src/github.com/albttx/kontacts.dev"
	)

	tests := []struct {
		name     string
		call     func(c Client) error
		wantArgv []string
	}{
		{
			name: "has-session via EnsureSession",
			call: func(c Client) error { _, err := c.EnsureSession(context.Background(), logical, directory); return err },
			wantArgv: []string{
				"tmux has-session -t=" + stored,
				"tmux new-session -d -s " + stored + " -c " + directory,
			},
		},
		{
			name:     "attach",
			call:     func(c Client) error { return c.AttachSession(context.Background(), logical) },
			wantArgv: []string{"tmux attach -t=" + stored},
		},
		{
			name:     "switch-client",
			call:     func(c Client) error { return c.SwitchClient(context.Background(), logical) },
			wantArgv: []string{"tmux switch-client -t=" + stored},
		},
		{
			name:     "focus outside tmux",
			call:     func(c Client) error { return c.Focus(context.Background(), logical, false) },
			wantArgv: []string{"tmux attach -t=" + stored},
		},
		{
			name:     "focus inside tmux",
			call:     func(c Client) error { return c.Focus(context.Background(), logical, true) },
			wantArgv: []string{"tmux switch-client -t=" + stored},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := newFakeRunner()
			if err := tt.call(Client{Runner: runner}); err != nil {
				t.Fatalf("call error = %v", err)
			}
			if got := runner.argvs(); !equal(got, tt.wantArgv) {
				t.Errorf("argv =\n  %s\nwant\n  %s",
					strings.Join(got, "\n  "), strings.Join(tt.wantArgv, "\n  "))
			}
			// The session name is rewritten, but the -c working directory is a
			// real filesystem path and must keep its dots.
			for _, call := range runner.calls {
				for i, arg := range call {
					switch {
					case arg == "-s", arg == "-c":
						continue
					case i > 0 && call[i-1] == "-c":
						if arg != directory {
							t.Errorf("working directory was altered: %q, want %q", arg, directory)
						}
					case i > 0 && call[i-1] == "-s", strings.HasPrefix(arg, "-t="):
						if strings.Contains(arg, logical) {
							t.Errorf("session argument still carries the unsanitised name: %q", arg)
						}
					}
				}
			}
		})
	}
}

// TestAdoptsSessionTmuxAlreadyStored covers the payoff: the user's existing
// sessions were created from dotted names by an older script, so tmux holds
// them under the rewritten name. p must find them instead of trying to
// recreate 19 sessions on every run.
func TestAdoptsSessionTmuxAlreadyStored(t *testing.T) {
	t.Parallel()

	runner := newFakeRunner("albttx/kontacts_dev") // what tmux actually holds
	created, err := Client{Runner: runner}.EnsureSession(
		context.Background(), "albttx/kontacts.dev", "/src")
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if created {
		t.Error("EnsureSession created a duplicate of a session tmux already holds")
	}
	if got := runner.argvs(); !equal(got, []string{"tmux has-session -t=albttx/kontacts_dev"}) {
		t.Errorf("argv = %v, want only the has-session probe", got)
	}
}

// TestDryRunPrintsSanitisedArgv makes sure `p tmux --dry-run` shows what would
// really be run, not a command that could never work.
func TestDryRunPrintsSanitisedArgv(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	client := Client{Runner: DryRunner{W: &buf}}
	if _, err := client.EnsureSession(context.Background(), "gnolang/docs.gno.land", "/src/docs"); err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if err := client.Focus(context.Background(), "gnolang/docs.gno.land", true); err != nil {
		t.Fatalf("Focus() error = %v", err)
	}

	want := []string{
		"tmux has-session -t=gnolang/docs_gno_land",
		"tmux new-session -d -s gnolang/docs_gno_land -c /src/docs",
		"tmux switch-client -t=gnolang/docs_gno_land",
	}
	got := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if !equal(got, want) {
		t.Errorf("dry run =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestErrorNamesBothForms keeps the diagnostic useful: a user reading an error
// needs the name they asked for and the name to look for in `tmux ls`.
func TestErrorNamesBothForms(t *testing.T) {
	t.Parallel()

	failing := &fakeRunner{existing: map[string]bool{}, alwaysErr: errors.New("duplicate session")}
	err := Client{Runner: failing}.NewSession(context.Background(), "albttx/kontacts.dev", "/src")
	if err == nil {
		t.Fatal("NewSession() should have failed")
	}
	for _, want := range []string{"albttx/kontacts.dev", "albttx/kontacts_dev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestHasSessionDoesNotLeakProbeStderr is the regression test for the noise
// the user saw on every `p add`:
//
//	can't find session: albttx/gh-todoist
//
// tmux writes that to stderr whenever has-session says no, which is the normal
// path for a project not yet opened. The probe's answer is its exit status, so
// its stderr must not reach the caller's terminal — while real failures from
// the other subcommands still must.
func TestHasSessionDoesNotLeakProbeStderr(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	runner := ExecRunner{Stdout: &bytes.Buffer{}, Stderr: &stderr}

	// Stand in for tmux: fail, and complain on stderr exactly as tmux does.
	got := runner.RunQuiet(context.Background(),
		"sh", "-c", "echo \"can't find session: albttx/gh-todoist\" >&2; exit 1")
	if got == nil {
		t.Fatal("RunQuiet should still report the non-zero exit")
	}
	if stderr.Len() != 0 {
		t.Errorf("probe leaked to the caller's stderr: %q", stderr.String())
	}

	// The same command through Run must stay visible: this is what keeps
	// genuine new-session and attach failures reportable.
	stderr.Reset()
	_ = runner.Run(context.Background(), "sh",
		"-c", "echo 'real failure' >&2; exit 1")
	if !strings.Contains(stderr.String(), "real failure") {
		t.Errorf("Run swallowed a genuine diagnostic: %q", stderr.String())
	}
}

// TestHasSessionUsesTheQuietPath asserts the wiring, since the behaviour above
// only helps if Client actually reaches for it.
func TestHasSessionUsesTheQuietPath(t *testing.T) {
	t.Parallel()

	q := &quietSpy{fakeRunner: fakeRunner{existing: map[string]bool{}}}
	if (Client{Runner: q}).HasSession(context.Background(), "albttx/gh-todoist") {
		t.Error("HasSession should report false for a missing session")
	}
	if !q.quietUsed {
		t.Error("HasSession did not use RunQuiet, so tmux's probe noise would reach the terminal")
	}

	// A Runner that does not implement QuietRunner must still work.
	plain := newFakeRunner("albttx/p")
	if !(Client{Runner: plain}).HasSession(context.Background(), "albttx/p") {
		t.Error("HasSession should fall back to Run for a plain Runner")
	}
}

// quietSpy records whether the quiet path was taken.
type quietSpy struct {
	fakeRunner
	quietUsed bool
}

func (q *quietSpy) RunQuiet(ctx context.Context, name string, args ...string) error {
	q.quietUsed = true
	return q.Run(ctx, name, args...)
}

// Both shipped runners must satisfy the optional interface, or Client silently
// falls back to the noisy path.
var (
	_ QuietRunner = ExecRunner{}
	_ QuietRunner = DryRunner{}
)
