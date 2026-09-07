package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/pkg/vcs"
)

const flagHTTPS = "https"

const specForms = "Accepts github.com/owner/repo, owner/repo (host defaults to github.com),\n" +
	"git@host:owner/repo.git or https://host/owner/repo."

// cloneCommand fetches a repository and stops there.
//
// It is deliberately the inert half of the pair: it never creates a tmux
// session and never attaches, so it cannot hang, which is what makes it the
// verb to use from scripts and CI. `p add` is the interactive one.
func cloneCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:      "clone",
		Usage:     "clone a repository into the source tree (fetch only)",
		ArgsUsage: "<spec>",
		Description: specForms + "\n\n" +
			"Clones over SSH into $CODE_DIR/{host}/{owner}/{repo}, creating parent\n" +
			"directories, and prints the destination. No tmux session, no attach —\n" +
			"use `p add` for that.",
		Flags: []ucli.Flag{
			&ucli.BoolFlag{Name: flagHTTPS, Usage: "clone over HTTPS instead of SSH", Local: true},
		},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			spec, dest, err := resolveSpec(cmd, "clone")
			if err != nil {
				return err
			}

			git := vcs.Git{Runner: a.git}
			if err := git.Clone(ctx, spec.CloneURL(cmd.Bool(flagHTTPS)), dest); err != nil {
				return err
			}

			_, err = fmt.Fprintln(a.stdout, dest)
			return err
		},
	}
}

// resolveSpec reads the single positional spec argument and resolves it
// against the configured source tree.
func resolveSpec(cmd *ucli.Command, verb string) (vcs.Spec, string, error) {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return vcs.Spec{}, "", fmt.Errorf("%s: expected a single repository spec, got %d", verb, len(args))
	}

	root, err := codeDir(cmd)
	if err != nil {
		return vcs.Spec{}, "", err
	}

	spec, err := vcs.ParseSpec(args[0], vcs.DefaultHost)
	if err != nil {
		return vcs.Spec{}, "", err
	}
	return spec, spec.Dest(root), nil
}
