package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/pkg/projectsearcher"
)

// queryCommand lists the projects a term matches, using the same tiered
// matcher as bare `p <query>`. Where navigation insists on exactly one result,
// this shows the whole match set, which makes it the tool for inspecting an
// ambiguous query.
func queryCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:          "query",
		Aliases:       []string{"q"},
		Usage:         "list projects matching a term",
		ArgsUsage:     "[term]",
		ShellComplete: a.complete,
		Flags: append(selectorFlags(),
			&ucli.IntFlag{Name: flagLimit, Usage: "maximum results, 0 for unlimited", Local: true},
			&ucli.BoolFlag{Name: flagJSON, Usage: "print JSON objects instead of names", Local: true},
			&ucli.BoolFlag{Name: flagPath, Usage: "print absolute paths instead of names", Local: true},
		),
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			args := cmd.Args().Slice()
			if len(args) > 1 {
				return fmt.Errorf("query: expected at most one term, got %d", len(args))
			}
			if err := ctxDone(ctx); err != nil {
				return err
			}

			all, err := projects(cmd)
			if err != nil {
				return err
			}

			term := ""
			if len(args) == 1 {
				term = args[0]
			}

			opts := selectorOptions(cmd)
			opts.Limit = cmd.Int(flagLimit)

			matches := projectsearcher.Search(all, term, opts)
			if len(matches) == 0 {
				return fmt.Errorf("%w for %q", projectsearcher.ErrNotFound, term)
			}
			return a.printProjects(matches, cmd.Bool(flagJSON), cmd.Bool(flagPath))
		},
	}
}
