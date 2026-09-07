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
// albttx/kontacts.dev, albttx/l7x.org — and both '.' and ':' are special to
// tmux. They separate the parts of a target spec, "session:window.pane", and
// tmux silently rewrites them to '_' when it creates a session. Handling that
// takes two independent things, and every name this package sends to tmux gets
// both: [SessionName] rewrites the name the way tmux will, and [Target] wraps
// it in the "-t=" form whose '=' prefix forces an exact match rather than an
// fnmatch pattern. Nothing is ever interpolated into a shell string.
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

// SessionName converts a logical session name into the name tmux will actually
// store, by replacing every '.' and ':' with '_'.
//
// tmux does this itself, in session_check_name(), because '.' and ':' are
// target-spec separators: a target is parsed as "session:window.pane" before
// any name is matched. A caller that ignores the rewrite gets a session it can
// never address again. Asking for "albttx/kontacts.dev" creates
// "albttx/kontacts_dev", and then:
//
//	tmux has-session -t=albttx/kontacts.dev   # can't find pane: dev
//	tmux has-session -t=albttx/kontacts_dev   # found
//
// so the first call reports "no such session" forever and every attempt to
// create it fails as a duplicate.
//
// Verified against tmux 3.6a: '.' and ':' are the only characters rewritten.
// Spaces, '/', '$', '*', '%', '[', ']' and tabs are all stored verbatim. A
// leading or trailing '.' is rewritten like any other. tmux rejects an empty
// name outright ("invalid session: "), and SessionName passes it through
// unchanged so that error surfaces from tmux rather than being masked here.
//
// Apply this to any name you hand to tmux. [Client] already does, and [Target]
// applies it too, so a name only needs sanitising once.
func SessionName(session string) string {
	return sessionNameReplacer.Replace(session)
}

// sessionNameReplacer mirrors tmux's session_check_name().
var sessionNameReplacer = strings.NewReplacer(".", "_", ":", "_")

// Target formats a session name as an exact-match tmux target,
// "-t=<sanitised name>", applying [SessionName] first.
//
// Both halves are load-bearing and neither is sufficient alone. [SessionName]
// is what makes the target refer to a session that can exist at all, since
// tmux would otherwise parse the '.' as a pane separator. The '=' prefix then
// stops tmux treating the remaining name as an fnmatch pattern, so
// "albttx/gno" cannot silently match "albttx/gnochess".
//
// [Client] applies this to every target it builds; Target is exported for
// callers assembling tmux commands this package does not cover.
func Target(session string) string { return "-t=" + SessionName(session) }

// HasSession reports whether a session exists, matching on the [SessionName]
// form of the given name so that it finds sessions tmux stored under a
// rewritten name. A failure to reach tmux at all is indistinguishable from a
// missing session and is reported as false.
func (c Client) HasSession(ctx context.Context, session string) bool {
	return c.Runner.Run(ctx, c.bin(), "has-session", Target(session)) == nil
}

// NewSession creates a detached session named session, with dir as the working
// directory of its first window. It fails if the session already exists; use
// [Client.EnsureSession] to make that case a no-op.
func (c Client) NewSession(ctx context.Context, session, dir string) error {
	name := SessionName(session)
	if err := c.Runner.Run(ctx, c.bin(), "new-session", "-d", "-s", name, "-c", dir); err != nil {
		return fmt.Errorf("create tmux session %s: %w", describe(session), err)
	}
	return nil
}

// describe names a session for an error message, showing the name tmux
// actually uses when it differs from the one the caller asked for.
func describe(session string) string {
	if name := SessionName(session); name != session {
		return fmt.Sprintf("%q (tmux stores it as %q)", session, name)
	}
	return fmt.Sprintf("%q", session)
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
		return fmt.Errorf("tmux attach %s: %w", describe(session), err)
	}
	return nil
}

// SwitchClient moves the calling client to another session. Use it only from
// inside tmux; see [Client.Focus].
func (c Client) SwitchClient(ctx context.Context, session string) error {
	if err := c.Runner.Run(ctx, c.bin(), "switch-client", Target(session)); err != nil {
		return fmt.Errorf("tmux switch-client %s: %w", describe(session), err)
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
