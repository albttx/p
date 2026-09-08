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
//
// Nothing here writes to stdout: both commands move the user through tmux,
// never through the cd sentinel, so stdout stays clean for the shell shim.
func (a *app) enterProject(ctx context.Context, session, dir string, noAttach bool) error {
	// tmux needs the real terminal. These commands are normally run through
	// the shell shim, whose command substitution has already captured stdout.
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
	return client.Focus(ctx, session, a.inTmux())
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
