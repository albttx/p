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
		// Mirror tmux: a target is "-t=<name>", exact match.
		target := strings.TrimPrefix(args[1], "-t=")
		if f.existing[target] {
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
		{"albttx/kontacts.dev", "-t=albttx/kontacts.dev"},
		{"albttx/0human.company", "-t=albttx/0human.company"},
		{"albttx/albttx.tech", "-t=albttx/albttx.tech"},
		{"albttx/l7x.org", "-t=albttx/l7x.org"},
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
				"tmux has-session -t=albttx/kontacts.dev",
				"tmux new-session -d -s albttx/kontacts.dev -c /src/github.com/albttx/kontacts.dev",
			},
		},
		{
			name:        "dotted session name, already present",
			existing:    []string{"albttx/0human.company"},
			session:     "albttx/0human.company",
			dir:         "/src/github.com/albttx/0human.company",
			wantCreated: false,
			wantArgv:    []string{"tmux has-session -t=albttx/0human.company"},
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
				"tmux has-session -t=albttx/l7x.org",
				"tmux new-session -d -s albttx/l7x.org -c /src/github.com/albttx/l7x.org",
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
		"tmux has-session -t=albttx/kontacts.dev",
		"tmux new-session -d -s albttx/kontacts.dev -c /src/github.com/albttx/kontacts.dev",
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
			argv: []string{"tmux", "new-session", "-d", "-s", "albttx/kontacts.dev", "-c", "/src/a-b_c.d"},
			want: "tmux new-session -d -s albttx/kontacts.dev -c /src/a-b_c.d",
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
			wantArgv: []string{"tmux attach -t=albttx/kontacts.dev"},
		},
		{
			name: "dotted name inside tmux", session: "albttx/l7x.org", inTmux: true,
			wantArgv: []string{"tmux switch-client -t=albttx/l7x.org"},
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
