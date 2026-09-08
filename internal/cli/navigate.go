package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/shell"
	"github.com/albttx/p/pkg/projectsearcher"
)

// navigate is the root action: it runs for any first argument that is not a
// subcommand, resolves it to exactly one project, and emits the cd sentinel.
//
// This is the only command that writes the cd sentinel; `p add`, `p new` and
// `p tmux` may write the attach sentinel instead. Ambiguity and misses go
// to stderr as errors so stdout stays empty and the shell shim has nothing to
// cd to.
func (a *app) navigate(ctx context.Context, cmd *ucli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return ucli.ShowRootCommandHelp(cmd)
	}
	if len(args) > 1 {
		return fmt.Errorf("expected a single query, got %d arguments: %v", len(args), args)
	}
	if err := ctxDone(ctx); err != nil {
		return err
	}

	p, err := a.resolve(cmd, args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.stdout, shell.SentinelCD+p.Path())
	return err
}

// resolve scans the tree and narrows it to the single project term names.
func (a *app) resolve(cmd *ucli.Command, term string) (projectsearcher.Project, error) {
	all, err := projects(cmd)
	if err != nil {
		return projectsearcher.Project{}, err
	}
	// Resolve takes no options of its own, so the narrowing flags are applied
	// first. selectorOptions never carries a Limit -- only `p query` has that
	// flag -- so this cannot truncate candidates and hide an ambiguity.
	return projectsearcher.Resolve(projectsearcher.Filter(all, selectorOptions(cmd)), term)
}
