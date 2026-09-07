package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/pkg/projectsearcher"
	"github.com/albttx/p/pkg/tmux"
)

const (
	flagFilter   = "filter"
	flagNoAttach = "no-attach"
	flagDryRun   = "dry-run"
)

// tmuxCommand opens a detached tmux session per project and attaches.
func tmuxCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:  "tmux",
		Usage: "ensure a tmux session per project, then attach",
		Description: "Creates a detached session named owner/repo rooted at each project, skipping\n" +
			"projects that already have one, then attaches. Use --dry-run to see the exact\n" +
			"argv without touching the tmux server.",
		ShellComplete: a.complete,
		Flags: append(selectorFlags(),
			&ucli.StringFlag{Name: flagFilter, Usage: "only projects matching this `TERM`", Local: true},
			&ucli.BoolFlag{Name: flagNoAttach, Usage: "create the sessions but do not attach", Local: true},
			&ucli.BoolFlag{Name: flagDryRun, Usage: "print the commands that would run, run nothing", Local: true},
		),
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if err := ctxDone(ctx); err != nil {
				return err
			}

			all, err := projects(cmd)
			if err != nil {
				return err
			}

			opts := selectorOptions(cmd)
			matches := projectsearcher.Search(all, cmd.String(flagFilter), opts)
			if len(matches) == 0 {
				return fmt.Errorf("tmux: %w", projectsearcher.ErrNotFound)
			}

			runner, release := a.tmuxRunner(cmd.Bool(flagDryRun))
			defer release()

			client := tmux.Client{Runner: runner}
			for _, p := range matches {
				if err := ctxDone(ctx); err != nil {
					return err
				}
				if _, err := client.EnsureSession(ctx, p.Name(), p.Path()); err != nil {
					return err
				}
			}

			if cmd.Bool(flagNoAttach) {
				return nil
			}
			return client.Attach(ctx)
		},
	}
}

// tmuxRunner picks the runner for this invocation and returns a release func.
func (a *app) tmuxRunner(dryRun bool) (tmux.Runner, func()) {
	if dryRun {
		return tmux.DryRunner{W: a.stdout}, func() {}
	}
	if a.tmux != nil {
		return a.tmux, func() {}
	}
	return a.newTmuxRunner()
}
