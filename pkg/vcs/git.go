package vcs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner executes an external command.
//
// It is the seam that keeps callers and tests from shelling out to a real git.
// Implementations receive the binary name and its arguments already split, and
// must never pass them through a shell.
type Runner interface {
	// Run executes the command and reports whether it succeeded.
	Run(ctx context.Context, name string, args ...string) error
	// Output executes the command and returns what it wrote to stdout.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec. The zero value is usable.
//
// Stdout deliberately defaults to os.Stderr rather than os.Stdout. Git writes
// its progress to stderr anyway, and a program whose own stdout is a data
// channel — a path, a JSON document, a shell sentinel — must not let a
// subprocess write into it. Set Stdout explicitly if you want the usual
// behaviour.
type ExecRunner struct {
	// Stdout receives the child's standard output. It defaults to os.Stderr
	// rather than os.Stdout; see the note on ExecRunner above.
	Stdout io.Writer
	// Stderr receives the child's standard error. Defaults to os.Stderr.
	Stderr io.Writer
}

// Run executes the command, streaming its output to the configured writers.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = orStderr(r.Stdout)
	cmd.Stderr = orStderr(r.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// Output executes the command and returns its standard output.
func (r ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = orStderr(r.Stderr)
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %v: %w", name, args, err)
	}
	return out, nil
}

func orStderr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}

// Git clones repositories through a [Runner]. The zero value is not usable;
// Runner is required.
type Git struct {
	// Runner executes git. Required.
	Runner Runner
	// Bin is the git executable to invoke. Defaults to "git", resolved on
	// PATH; set it to an absolute path to pin a particular install.
	Bin string
}

func (g Git) bin() string {
	if g.Bin == "" {
		return "git"
	}
	return g.Bin
}

// Init runs "git init" in dir, creating dir and its parents first.
//
// It deliberately passes no --initial-branch or other branch flag, so the
// user's own init.defaultBranch configuration decides the branch name.
//
// git init is itself idempotent — re-running it in an existing repository just
// reinitialises it — but callers that want to avoid the noise should check
// first; see projectsearcher.IsRepo.
func (g Git) Init(ctx context.Context, dir string) error {
	if g.Runner == nil {
		return fmt.Errorf("init %s: no runner configured", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := g.Runner.Run(ctx, g.bin(), "init", dir); err != nil {
		return fmt.Errorf("init %s: %w", dir, err)
	}
	return nil
}

// Clone runs "git clone url dest", creating dest's parent directories first so
// that a fresh {host}/{owner} prefix does not have to exist beforehand.
//
// It refuses to clone onto an existing path rather than letting git fail
// halfway, and it does not clean up a partial checkout if git fails: the
// directory is left in place for inspection.
func (g Git) Clone(ctx context.Context, url, dest string) error {
	if g.Runner == nil {
		return fmt.Errorf("clone %s: no runner configured", url)
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("clone %s: destination %s already exists", url, dest)
	}
	if parent := filepath.Dir(dest); parent != "" && parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", parent, err)
		}
	}
	if err := g.Runner.Run(ctx, g.bin(), "clone", url, dest); err != nil {
		return fmt.Errorf("clone %s: %w", url, err)
	}
	return nil
}
