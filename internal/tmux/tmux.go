// Package tmux wraps the tmux commands p needs.
//
// Every tmux invocation goes through a [Runner] as an explicit argv slice.
// Session names routinely contain dots — albttx/kontacts.dev,
// albttx/0human.company, albttx/l7x.org — and "." and ":" are meaningful in
// tmux target specs, so nothing here builds a shell string and every target is
// passed as "-t=<name>", whose "=" prefix forces an exact match instead of
// fnmatch.
package tmux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner executes an external command. It is the seam that keeps tests from
// talking to a real tmux server.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
//
// The zero value is usable but not what you want for attaching: p's own stdout
// is consumed by the shell shim via command substitution, so a tmux that
// inherited it would find a pipe where it expects a terminal. Use
// [TerminalRunner], which binds the controlling terminal directly.
type ExecRunner struct {
	Stdin  io.Reader
	Stdout io.Writer
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

// Close releases any terminal opened by [TerminalRunner].
func (r ExecRunner) Close() {
	for _, c := range r.closers {
		_ = c.Close()
	}
}

// Run executes the command.
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

// Run prints the argv.
func (d DryRunner) Run(_ context.Context, name string, args ...string) error {
	fmt.Fprintln(d.W, QuoteArgv(append([]string{name}, args...)))
	if len(args) > 0 && args[0] == "has-session" {
		return errNoSession
	}
	return nil
}

// Output prints the argv and returns no output.
func (d DryRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return nil, d.Run(ctx, name, args...)
}

var errNoSession = fmt.Errorf("tmux: session not found")

// Client issues tmux commands.
type Client struct {
	// Runner executes tmux. Required.
	Runner Runner
	// Bin is the tmux executable, "tmux" when empty.
	Bin string
}

func (c Client) bin() string {
	if c.Bin == "" {
		return "tmux"
	}
	return c.Bin
}

// Target formats a session name as an exact-match tmux target. The "="
// prefix stops tmux from treating dots, colons and wildcards in the name as
// target syntax.
func Target(session string) string { return "-t=" + session }

// HasSession reports whether a session with this exact name exists.
func (c Client) HasSession(ctx context.Context, session string) bool {
	return c.Runner.Run(ctx, c.bin(), "has-session", Target(session)) == nil
}

// NewSession creates a detached session named session with dir as its working
// directory.
func (c Client) NewSession(ctx context.Context, session, dir string) error {
	if err := c.Runner.Run(ctx, c.bin(), "new-session", "-d", "-s", session, "-c", dir); err != nil {
		return fmt.Errorf("create tmux session %q: %w", session, err)
	}
	return nil
}

// EnsureSession creates the session if it does not already exist. It reports
// whether a session was created.
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

// QuoteArgv renders an argv as a single shell-safe line, for printing.
func QuoteArgv(argv []string) string {
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
