// Package cli defines p's command tree.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/config"
	"github.com/albttx/p/pkg/projectsearcher"
	"github.com/albttx/p/pkg/tmux"
	"github.com/albttx/p/pkg/vcs"
)

const (
	flagCodeDir = "code-dir"
	flagHost    = "host"
	flagOwner   = "owner"
	flagExclude = "exclude"
	flagLimit   = "limit"
)

// Params wire the command tree to its environment. Everything the commands
// touch outside the filesystem arrives here, so tests can substitute it.
type Params struct {
	// Version is reported by "p --version".
	Version string
	// CodeDir is the already-resolved default source root, used as the
	// default for --code-dir.
	CodeDir string
	// Stdout carries command output and the cd sentinel. Defaults to
	// os.Stdout.
	Stdout io.Writer
	// Stderr carries every diagnostic. Defaults to os.Stderr.
	Stderr io.Writer
	// Git runs git. Defaults to a real exec runner.
	Git vcs.Runner
	// Tmux runs tmux. Defaults to a runner bound to the controlling terminal.
	Tmux tmux.Runner
	// NewTmuxRunner builds the tmux runner lazily, so "p list" never opens a
	// terminal. Ignored when Tmux is set.
	NewTmuxRunner func() (tmux.Runner, func())
	// Getenv reads the environment. Defaults to os.Getenv. It exists so tests
	// can drive the $TMUX branch in "p add" without a tmux server.
	Getenv func(string) string
	// StdoutIsTerminal reports whether p's stdout is a terminal rather than a
	// pipe. It decides whether tmux can be attached in-process or has to be
	// delegated to the shell shim; see [app.focus]. Defaults to inspecting
	// os.Stdout.
	StdoutIsTerminal func() bool
}

// app holds the dependencies shared by every command action.
type app struct {
	stdout        io.Writer
	stderr        io.Writer
	git           vcs.Runner
	tmux          tmux.Runner
	newTmuxRunner func() (tmux.Runner, func())
	getenv        func(string) string
	stdoutIsTTY   func() bool
}

// inTmux reports whether p is running inside a tmux session. tmux sets $TMUX
// for every process it spawns, and "tmux attach" refuses to run there.
func (a *app) inTmux() bool { return a.getenv("TMUX") != "" }

// stdoutIsTerminal reports whether the process stdout is a character device,
// which is true when p was run directly in a terminal and false when the shell
// shim captured it with $(...).
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// New builds the root command.
func New(p Params) *ucli.Command {
	a := &app{
		stdout:        p.Stdout,
		stderr:        p.Stderr,
		git:           p.Git,
		tmux:          p.Tmux,
		newTmuxRunner: p.NewTmuxRunner,
		getenv:        p.Getenv,
		stdoutIsTTY:   p.StdoutIsTerminal,
	}
	if a.getenv == nil {
		a.getenv = os.Getenv
	}
	if a.stdoutIsTTY == nil {
		a.stdoutIsTTY = stdoutIsTerminal
	}
	if a.stdout == nil {
		a.stdout = os.Stdout
	}
	if a.stderr == nil {
		a.stderr = os.Stderr
	}
	if a.git == nil {
		a.git = vcs.ExecRunner{Stdout: a.stderr, Stderr: a.stderr}
	}
	if a.newTmuxRunner == nil {
		a.newTmuxRunner = func() (tmux.Runner, func()) {
			r, done := tmux.TerminalRunner()
			return r, done
		}
	}

	root := &ucli.Command{
		Name:    "p",
		Usage:   "jump around a GOPATH-style source tree",
		Version: p.Version,
		Description: "p resolves a short query to a project under $CODE_DIR/{host}/{owner}/{repo}\n" +
			"and asks the shell to cd there. Run `p init zsh` for the shell wrapper\n" +
			"that makes the cd happen.",
		ArgsUsage:             "<query>",
		EnableShellCompletion: true,
		Suggest:               true,
		Writer:                a.stdout,
		ErrWriter:             a.stderr,
		Flags: append([]ucli.Flag{
			&ucli.StringFlag{
				Name:    flagCodeDir,
				Usage:   "root of the source tree",
				Value:   p.CodeDir,
				Sources: ucli.EnvVars(config.EnvCodeDir),
			},
		}, selectorFlags()...),
		Commands: []*ucli.Command{
			cloneCommand(a),
			addCommand(a),
			newCommand(a),
			listCommand(a),
			queryCommand(a),
			tmuxCommand(a),
			pathCommand(a),
			initCommand(a),
		},
		// Any first argument that is not a subcommand falls through to here,
		// which is exactly the `p <query>` navigation path.
		Action:        a.navigate,
		ShellComplete: a.complete,
	}

	silenceUsageDumps(root)
	return root
}

// silenceUsageDumps stops a usage error from printing the help screen.
//
// By default urfave/cli writes "Incorrect Usage" plus the whole help text to
// the root Writer, which is stdout — the channel the shell shim reads. A
// mistyped flag would then have the shim echo an entire usage screen, and
// stdout is supposed to stay clean whenever the command fails. Returning the
// error instead routes it through main, which prints it on stderr.
//
// OnUsageError is looked up on the command that failed and is not inherited
// from the parent, so it has to be installed on every command in the tree.
func silenceUsageDumps(cmd *ucli.Command) {
	cmd.OnUsageError = func(_ context.Context, failed *ucli.Command, err error, _ bool) error {
		return fmt.Errorf("%w (try `p %s--help`)", err, helpPrefix(failed))
	}
	for _, sub := range cmd.Commands {
		silenceUsageDumps(sub)
	}
}

// helpPrefix names the subcommand in a "try p <sub> --help" hint, or nothing
// for the root command.
func helpPrefix(cmd *ucli.Command) string {
	if cmd.Name == "" || cmd.Name == "p" {
		return ""
	}
	return cmd.Name + " "
}

// codeDir returns the resolved, expanded source root for this invocation.
func codeDir(cmd *ucli.Command) (string, error) {
	raw := cmd.String(flagCodeDir)
	if raw == "" {
		return "", fmt.Errorf("no source tree configured: set $%s or --%s", config.EnvCodeDir, flagCodeDir)
	}
	// The value may have come straight from the environment, so it can still
	// contain "~" or "$HOME".
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	dir, err := config.Expand(raw, home, os.Getenv)
	if err != nil {
		return "", fmt.Errorf("resolve --%s: %w", flagCodeDir, err)
	}
	return dir, nil
}

// projects scans the configured source tree.
func projects(cmd *ucli.Command) ([]projectsearcher.Project, error) {
	dir, err := codeDir(cmd)
	if err != nil {
		return nil, err
	}
	return projectsearcher.Scan(dir)
}

// selectorFlags are the filters shared by the commands that operate on sets of
// projects.
func selectorFlags() []ucli.Flag {
	return []ucli.Flag{
		&ucli.StringFlag{Name: flagHost, Usage: "only projects on this host", Local: true},
		&ucli.StringFlag{Name: flagOwner, Usage: "only projects with this owner", Local: true},
		&ucli.StringSliceFlag{
			Name:  flagExclude,
			Usage: "drop projects matching `PATTERN` (repeatable; glob or substring)",
			Local: true,
		},
	}
}

// selectorOptions reads the shared filters off cmd.
func selectorOptions(cmd *ucli.Command) projectsearcher.Options {
	return projectsearcher.Options{
		Host:    cmd.String(flagHost),
		Owner:   cmd.String(flagOwner),
		Exclude: cmd.StringSlice(flagExclude),
	}
}

// ctxDone is a small guard so long scans respect cancellation.
func ctxDone(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
