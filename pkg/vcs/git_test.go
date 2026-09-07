package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every argv it is asked to run instead of executing it.
type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.err
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil, f.err
}

func (f *fakeRunner) argv(i int) string {
	if i >= len(f.calls) {
		return ""
	}
	return strings.Join(f.calls[i], " ")
}

func TestGitClone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		https    bool
		wantArgv string
	}{
		{
			name: "ssh by default", spec: "github.com/albttx/p",
			wantArgv: "git clone git@github.com:albttx/p.git DEST",
		},
		{
			name: "https when asked", spec: "github.com/albttx/p", https: true,
			wantArgv: "git clone https://github.com/albttx/p.git DEST",
		},
		{
			name: "bare owner/repo", spec: "albttx/example.com",
			wantArgv: "git clone git@github.com:albttx/example.com.git DEST",
		},
		{
			name: "full ssh url round-trips", spec: "git@github.com:albttx/kontacts.dev.git",
			wantArgv: "git clone git@github.com:albttx/kontacts.dev.git DEST",
		},
		{
			name: "https url cloned over ssh", spec: "https://github.com/gnolang/gno",
			wantArgv: "git clone git@github.com:gnolang/gno.git DEST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			spec, err := ParseSpec(tt.spec, DefaultHost)
			if err != nil {
				t.Fatalf("ParseSpec(%q) error = %v", tt.spec, err)
			}

			dest := spec.Dest(root)
			runner := &fakeRunner{}
			git := Git{Runner: runner}

			if err := git.Clone(context.Background(), spec.CloneURL(tt.https), dest); err != nil {
				t.Fatalf("Clone() error = %v", err)
			}

			if len(runner.calls) != 1 {
				t.Fatalf("Clone() issued %d commands %v, want 1", len(runner.calls), runner.calls)
			}
			want := strings.Replace(tt.wantArgv, "DEST", dest, 1)
			if got := runner.argv(0); got != want {
				t.Errorf("argv = %q, want %q", got, want)
			}

			// The parent of the destination must exist so git can write into
			// it; the destination itself must not, or git refuses.
			parent := filepath.Dir(dest)
			if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
				t.Errorf("parent %q was not created: err=%v", parent, err)
			}
			if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("destination %q should not have been created by Clone: err=%v", dest, err)
			}
		})
	}
}

func TestGitCloneErrors(t *testing.T) {
	t.Parallel()

	t.Run("refuses an existing destination", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		dest := filepath.Join(root, "github.com", "albttx", "p")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		runner := &fakeRunner{}
		err := Git{Runner: runner}.Clone(context.Background(), "git@github.com:albttx/p.git", dest)
		if err == nil {
			t.Fatal("Clone() into an existing directory should fail")
		}
		if len(runner.calls) != 0 {
			t.Errorf("Clone() ran %v, want no command at all", runner.calls)
		}
	})

	t.Run("propagates the runner error", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("git exploded")
		runner := &fakeRunner{err: sentinel}
		dest := filepath.Join(t.TempDir(), "github.com", "albttx", "p")

		err := Git{Runner: runner}.Clone(context.Background(), "git@github.com:albttx/p.git", dest)
		if !errors.Is(err, sentinel) {
			t.Errorf("Clone() error = %v, want it to wrap %v", err, sentinel)
		}
	})

	t.Run("no runner configured", func(t *testing.T) {
		t.Parallel()

		dest := filepath.Join(t.TempDir(), "github.com", "albttx", "p")
		if err := (Git{}).Clone(context.Background(), "git@github.com:albttx/p.git", dest); err == nil {
			t.Fatal("Clone() without a runner should fail")
		}
	})
}

func TestGitCustomBin(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	dest := filepath.Join(t.TempDir(), "github.com", "albttx", "p")

	git := Git{Runner: runner, Bin: "/usr/local/bin/git"}
	if err := git.Clone(context.Background(), "git@github.com:albttx/p.git", dest); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if got := runner.calls[0][0]; got != "/usr/local/bin/git" {
		t.Errorf("binary = %q, want /usr/local/bin/git", got)
	}
}
