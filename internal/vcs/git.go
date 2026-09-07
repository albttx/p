package vcs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner executes an external command. It is the seam that keeps tests from
// shelling out to a real git.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
//
// Stdout deliberately defaults to stderr rather than stdout: p's stdout
// carries the cd sentinel that the shell shim consumes, so no subprocess may
// be allowed to write to it.
type ExecRunner struct {
	Stdout io.Writer
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

// Git clones repositories through a [Runner].
type Git struct {
	// Runner executes git. Required.
	Runner Runner
	// Bin is the git executable, "git" when empty.
	Bin string
}

func (g Git) bin() string {
	if g.Bin == "" {
		return "git"
	}
	return g.Bin
}

// Clone clones url into dest, creating dest's parent directories first.
// It refuses to clone over an existing path.
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
