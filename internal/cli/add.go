package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/shell"
	"github.com/albttx/p/pkg/tmux"
	"github.com/albttx/p/pkg/vcs"
)

// addCommand is "start working on this project", whether or not it is already
// on disk: clone if missing, ensure the tmux session, then go there.
//
// It is idempotent by design. Running it on a project you already have is not
// an error — it just puts you in the session — which is what makes it safe to
// use as the single verb for onboarding a repository.
func addCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:      "add",
		Usage:     "existing repo: clone if missing, then session + attach",
		ArgsUsage: "<spec>",
		Description: specForms + "\n\n" +
			"Clones into $CODE_DIR/{host}/{owner}/{repo} only if that path does not\n" +
			"already exist, creates a detached tmux session named owner/repo rooted\n" +
			"there unless one exists, then attaches — or switches, if you are already\n" +
			"inside tmux. Safe to re-run.\n\n" +
			"See `p clone` to fetch without a session, or `p new` for a project that\n" +
			"does not exist anywhere yet.",
		Flags: []ucli.Flag{
			&ucli.BoolFlag{Name: flagHTTPS, Usage: "clone over HTTPS instead of SSH", Local: true},
			&ucli.BoolFlag{Name: flagNoAttach, Usage: "clone and create the session but stay put", Local: true},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			spec, dest, err := resolveSpec(cmd, "add")
			if err != nil {
				return err
			}

			// Step 1: clone, unless it is already there.
			switch exists, err := dirExists(dest); {
			case err != nil:
				return err
			case exists:
				fmt.Fprintf(a.stderr, "p: %s already present at %s\n", spec.Full(), dest)
			default:
				git := vcs.Git{Runner: a.git}
				if err := git.Clone(ctx, spec.CloneURL(cmd.Bool(flagHTTPS)), dest); err != nil {
					return err
				}
				fmt.Fprintf(a.stderr, "p: cloned %s to %s\n", spec.Full(), dest)
			}

			return a.enterProject(ctx, spec.Owner+"/"+spec.Repo, dest, cmd.Bool(flagNoAttach))
		},
	}
}

// enterProject ensures the tmux session for a checkout and puts the user in
// it. It is the shared tail of `p add` and `p new`, which differ only in how
// the directory came to exist.
func (a *app) enterProject(ctx context.Context, session, dir string, noAttach bool) error {
	runner, release := a.tmuxRunner(false)
	defer release()

	client := tmux.Client{Runner: runner}

	created, err := client.EnsureSession(ctx, session, dir)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(a.stderr, "p: created tmux session %s\n", tmux.SessionName(session))
	}

	if noAttach {
		return nil
	}
	return a.focus(ctx, client, session)
}

// focus puts the user into a tmux session, by whichever of three routes can
// actually work from where p is running.
//
//   - Already inside tmux: switch-client. It only messages the running server
//     and needs no terminal, so it works even under command substitution.
//   - Outside tmux with stdout on a terminal: attach in-process. p was run
//     directly rather than through the shim, so it still owns the terminal.
//   - Outside tmux with stdout captured: emit [shell.SentinelTmux] and let the
//     shell function attach once the substitution has closed.
//
// The third case is the one that matters in daily use, and it is why this is
// not simply a call to tmux.Client.Focus. "tmux attach" has to take over the
// controlling terminal, which a process whose stdout is a pipe cannot do; it
// fails with "open terminal failed: can't use /dev/tty". Reopening /dev/tty is
// not enough. Delegating to the parent shell is the same trick the cd sentinel
// uses, for the same underlying reason.
//
// An empty session means "attach to the server without naming a session".
func (a *app) focus(ctx context.Context, client tmux.Client, session string) error {
	if a.inTmux() {
		if session == "" {
			// Already inside tmux and no particular session was asked for:
			// there is nothing to attach to.
			fmt.Fprintln(a.stderr, "p: already inside tmux, not attaching")
			return nil
		}
		return client.SwitchClient(ctx, session)
	}

	if a.stdoutIsTTY() {
		if session == "" {
			return client.Attach(ctx)
		}
		return client.AttachSession(ctx, session)
	}

	// stdout is captured, so hand the attach to the shell shim. The payload is
	// already sanitised: the shim passes it straight to "tmux attach -t=".
	_, err := fmt.Fprintln(a.stdout, shell.SentinelTmux+tmux.SessionName(session))
	return err
}

// dirExists reports whether path is present, treating a non-directory as
// present too so that a stray file is reported rather than cloned over.
func dirExists(path string) (bool, error) {
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
}
