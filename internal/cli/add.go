package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	ucli "github.com/urfave/cli/v3"

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
		Usage:     "clone if missing, ensure a tmux session, and go there",
		ArgsUsage: "<spec>",
		Description: specForms + "\n\n" +
			"Clones into $CODE_DIR/{host}/{owner}/{repo} only if that path does not\n" +
			"already exist, creates a detached tmux session named owner/repo rooted\n" +
			"there unless one exists, then attaches — or switches, if you are already\n" +
			"inside tmux. Safe to re-run. Use `p clone` for a fetch with no session.",
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

			// Steps 2 and 3 talk to tmux, which needs the real terminal: this
			// command is normally run through the shell shim, whose command
			// substitution has already captured stdout.
			runner, release := a.tmuxRunner(false)
			defer release()

			client := tmux.Client{Runner: runner}
			session := spec.Owner + "/" + spec.Repo

			created, err := client.EnsureSession(ctx, session, dest)
			if err != nil {
				return err
			}
			if created {
				fmt.Fprintf(a.stderr, "p: created tmux session %s\n", session)
			}

			if cmd.Bool(flagNoAttach) {
				return nil
			}
			// Nothing here writes to stdout: p add moves the user through
			// tmux, never through the cd sentinel.
			return client.Focus(ctx, session, a.inTmux())
		},
	}
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
