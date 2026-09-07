package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"
)

// pathCommand prints a resolved path with no sentinel, for use in $(...).
func pathCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:      "path",
		Usage:     "print the absolute path of a project",
		ArgsUsage: "<query>",
		Description: "Resolves a query exactly like bare `p <query>` but prints only the path,\n" +
			"with no cd sentinel, so it composes with command substitution:\n\n" +
			"    cd \"$(p path gno)\"",
		ShellComplete: a.complete,
		Flags:         selectorFlags(),
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 1 {
				return fmt.Errorf("path: expected a single query, got %d", len(args))
			}
			if err := ctxDone(ctx); err != nil {
				return err
			}
			p, err := a.resolve(cmd, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(a.stdout, p.Path())
			return err
		},
	}
}
