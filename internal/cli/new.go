package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/pkg/projectsearcher"
	"github.com/albttx/p/pkg/vcs"
)

// newCommand starts a project that does not exist yet: create the directory,
// git init it, then go there.
//
// There is no remote involved at any point. The git init step is not a nicety
// — [projectsearcher.Scan] only recognises a directory as a project once it
// holds a .git entry, so without it the new project would be invisible to
// `p list`, `p tmux` and bare `p <query>`.
//
// Like `p add`, it is idempotent: every step is skipped if it is already done,
// so re-running it on an existing project simply puts you back in its session.
func newCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:      "new",
		Usage:     "no remote yet: create the dir, git init, then session + attach",
		ArgsUsage: "<spec>",
		Description: "Creates $CODE_DIR/{host}/{owner}/{repo}, runs `git init` in it, opens a\n" +
			"tmux session named owner/repo rooted there, and attaches — or switches, if\n" +
			"you are already inside tmux.\n\n" +
			"Nothing is fetched and no remote is configured: this is for a project that\n" +
			"does not exist anywhere yet. Use `p add` for a repository that already has\n" +
			"a remote. Accepts owner/repo, defaulting the host to " + vcs.DefaultHost +
			", or an explicit host/owner/repo.\n\n" +
			"Safe to re-run: an existing directory is kept, and `git init` is skipped if\n" +
			"the directory is already a repository.",
		Flags: []ucli.Flag{
			&ucli.BoolFlag{Name: flagNoAttach, Usage: "create the project and session but stay put", Local: true},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			spec, dest, err := resolveSpec(cmd, "new")
			if err != nil {
				return err
			}

			// git init is skipped only when the directory is already a
			// repository by exactly the rule Scan uses, so whatever this
			// command leaves behind is guaranteed to be discoverable.
			if projectsearcher.IsRepo(dest) {
				fmt.Fprintf(a.stderr, "p: %s already a repository at %s\n", spec.Full(), dest)
			} else {
				// Init creates the directory and its parents, so the
				// not-yet-existing and exists-but-not-a-repo cases are the
				// same call.
				if err := (vcs.Git{Runner: a.git}).Init(ctx, dest); err != nil {
					return err
				}
				fmt.Fprintf(a.stderr, "p: created %s at %s\n", spec.Full(), dest)
			}

			return a.enterProject(ctx, spec.Owner+"/"+spec.Repo, dest, cmd.Bool(flagNoAttach))
		},
	}
}
