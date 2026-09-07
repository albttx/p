// Package tmux drives the handful of tmux commands needed to keep one session
// per project.
//
// Every invocation goes through a [Runner] as an explicit argv slice, which is
// what makes the package testable: substitute a fake Runner and assert the
// exact command line, with no tmux server anywhere near the test. [DryRunner]
// is a ready-made one that prints instead of executing.
//
//	client := tmux.Client{Runner: tmux.ExecRunner{}}
//
//	created, err := client.EnsureSession(ctx, "albttx/p", "/src/github.com/albttx/p")
//	if err != nil {
//		return err
//	}
//	_ = created
//	return client.Focus(ctx, "albttx/p", os.Getenv("TMUX") != "")
//
// Session names here are "owner/repo", which routinely contain dots —
// albttx/kontacts.dev, albttx/l7x.org. Both '.' and ':' are meaningful in tmux
// target specs, so every target this package builds uses the "-t=<name>" form,
// whose '=' prefix forces an exact match instead of an fnmatch pattern. See
// [Target]. Nothing is ever interpolated into a shell string.
//
// This package deliberately defines its own [Runner] and [ExecRunner] rather
// than sharing them with a sibling package. The duplication is a few lines and
// buys two public packages that do not depend on each other, which is the
// better trade for a caller who wants only one of them.
package tmux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner executes an external command.
//
// It is the seam that keeps callers and tests away from a real tmux server.
// Implementations receive the binary name and its arguments already split, and
// must never pass them through a shell.
type Runner interface {
	// Run executes the command and reports whether it succeeded. A non-nil
	// error means a non-zero exit or a failure to start.
	Run(ctx context.Context, name string, args ...string) error
	// Output executes the command and returns what it wrote to stdout.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
//
// The zero value is usable but not what you want for attaching: p's own stdout
// is consumed by the shell shim via command substitution, so a tmux that
// inherited it would find a pipe where it expects a terminal. Use
// [TerminalRunner], which binds the controlling terminal directly.
type ExecRunner struct {
	// Stdin is the child's standard input. Defaults to the process's own.
	Stdin io.Reader
	// Stdout is the child's standard output. Defaults to os.Stderr, not
	// os.Stdout, so that a program using its own stdout as a data channel
	// cannot have it polluted by a subprocess.
	Stdout io.Writer
	// Stderr is the child's standard error. Defaults to os.Stderr.
	Stderr io.Writer

	// closers releases any file opened by TerminalRunner.
	closers []io.Closer
}

// TerminalRunner returns an [ExecRunner] bound to the controlling terminal at
// /dev/tty when one is available, so "tmux attach" works even though p's
// stdout is captured by the shell shim. Without a controlling terminal it
// falls back to the process streams, with stdout redirected to stderr to keep
// the sentinel channel clean. The returned func releases the terminal.
func TerminalRunner() (ExecRunner, func()) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return ExecRunner{Stdin: os.Stdin, Stdout: os.Stderr, Stderr: os.Stderr}, func() {}
	}
	r := ExecRunner{Stdin: tty, Stdout: tty, Stderr: tty, closers: []io.Closer{tty}}
	return r, r.Close
}

// Close releases any terminal opened by [TerminalRunner]. It is safe to call
// on an ExecRunner built any other way, where it does nothing.
func (r ExecRunner) Close() {
	for _, c := range r.closers {
		_ = c.Close()
	}
}

// Run executes the command, wiring the child to the configured streams.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = orStdin(r.Stdin)
	cmd.Stdout = orStderr(r.Stdout)
	cmd.Stderr = orStderr(r.Stderr)
	return cmd.Run()
}

// Output executes the command and returns its standard output.
func (r ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = orStdin(r.Stdin)
	cmd.Stderr = orStderr(r.Stderr)
	return cmd.Output()
}

func orStdin(r io.Reader) io.Reader {
	if r == nil {
		return os.Stdin
	}
	return r
}

func orStderr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}

// DryRunner is a [Runner] that prints the argv it would execute and runs
// nothing.
//
// "has-session" probes report failure, so a dry run prints the full set of
// commands p would issue against a tmux server with no sessions yet — which is
// the interesting case to inspect.
type DryRunner struct {
	// W receives one shell-quoted argv per line. Required.
	W io.Writer
}

var _ Runner = DryRunner{}

// Run prints the argv and reports success, except for "has-session" probes,
// which report failure so a dry run shows the sessions it would create.
func (d DryRunner) Run(_ context.Context, name string, args ...string) error {
	fmt.Fprintln(d.W, quoteArgv(append([]string{name}, args...)))
	if len(args) > 0 && args[0] == "has-session" {
		return errNoSession
	}
	return nil
}

// Output prints the argv and returns no data.
func (d DryRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, d.Run(ctx, name, args...)
}

var errNoSession = fmt.Errorf("tmux: session not found")

// Client issues tmux commands through a [Runner]. The zero value is not
// usable; Runner is required.
type Client struct {
	// Runner executes tmux. Required.
	Runner Runner
	// Bin is the tmux executable to invoke. Defaults to "tmux", resolved on
	// PATH; set it to an absolute path to pin a particular install.
	Bin string
}

func (c Client) bin() string {
	if c.Bin == "" {
		return "tmux"
	}
	return c.Bin
}

// Target formats a session name as an exact-match tmux target, "-t=<session>".
//
// The '=' prefix is load-bearing: without it tmux treats the name as an
// fnmatch pattern and splits it on '.' and ':', so a session called
// "albttx/l7x.org" would be read as window "org" of session "albttx/l7x".
// [Client] applies this to every target it builds; Target is exported for
// callers assembling tmux commands this package does not cover.
func Target(session string) string { return "-t=" + session }

// HasSession reports whether a session with this exact name exists. A failure
// to reach tmux at all is indistinguishable from a missing session and is
// reported as false.
func (c Client) HasSession(ctx context.Context, session string) bool {
	return c.Runner.Run(ctx, c.bin(), "has-session", Target(session)) == nil
}

// NewSession creates a detached session named session, with dir as the working
// directory of its first window. It fails if the session already exists; use
// [Client.EnsureSession] to make that case a no-op.
func (c Client) NewSession(ctx context.Context, session, dir string) error {
	if err := c.Runner.Run(ctx, c.bin(), "new-session", "-d", "-s", session, "-c", dir); err != nil {
		return fmt.Errorf("create tmux session %q: %w", session, err)
	}
	return nil
}

// EnsureSession creates the session unless it already exists, and reports
// whether it created one. It is idempotent, so it is safe to call on every
// project on every run.
func (c Client) EnsureSession(ctx context.Context, session, dir string) (bool, error) {
	if c.HasSession(ctx, session) {
		return false, nil
	}
	if err := c.NewSession(ctx, session, dir); err != nil {
		return false, err
	}
	return true, nil
}

// Attach attaches the terminal to the tmux server, without naming a session.
func (c Client) Attach(ctx context.Context) error {
	if err := c.Runner.Run(ctx, c.bin(), "attach"); err != nil {
		return fmt.Errorf("tmux attach: %w", err)
	}
	return nil
}

// AttachSession attaches the terminal to one named session. Use it only from
// outside tmux; see [Client.Focus].
func (c Client) AttachSession(ctx context.Context, session string) error {
	if err := c.Runner.Run(ctx, c.bin(), "attach", Target(session)); err != nil {
		return fmt.Errorf("tmux attach %q: %w", session, err)
	}
	return nil
}

// SwitchClient moves the calling client to another session. Use it only from
// inside tmux; see [Client.Focus].
func (c Client) SwitchClient(ctx context.Context, session string) error {
	if err := c.Runner.Run(ctx, c.bin(), "switch-client", Target(session)); err != nil {
		return fmt.Errorf("tmux switch-client %q: %w", session, err)
	}
	return nil
}

// Focus puts the user in session, choosing the only command that works from
// where they are.
//
// "tmux attach" fails outright when the caller is already inside a session,
// which is the common case for anyone who lives in tmux; "switch-client" is
// the in-session equivalent. inTmux reports whether $TMUX is set, and is a
// parameter rather than an environment lookup so callers can test both
// branches without a server.
func (c Client) Focus(ctx context.Context, session string, inTmux bool) error {
	if inTmux {
		return c.SwitchClient(ctx, session)
	}
	return c.AttachSession(ctx, session)
}

// quoteArgv renders an argv as a single shell-safe line, for display only.
func quoteArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = quote(a)
	}
	return strings.Join(parts, " ")
}

const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%_+=:,./-"

func quote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.ContainsAny(s, "'") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	for _, r := range s {
		if !strings.ContainsRune(safeChars, r) {
			return "'" + s + "'"
		}
	}
	return s
}
