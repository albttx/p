package cli

import (
	"context"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/shell"
)

const (
	flagBin  = "bin"
	flagFunc = "func"
)

// initCommand prints the shell function that performs the cd.
//
// This is distinct from the built-in `p completion <shell>`, which emits tab
// completion. The two are unrelated and you generally want both.
func initCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:      "init",
		Usage:     "print the shell cd wrapper for zsh, bash or fish",
		ArgsUsage: "<shell>",
		Description: "Emits a shell function that runs the p binary and turns its cd sentinel into\n" +
			"a real directory change, because a child process cannot cd its parent shell.\n" +
			"Install the binary as `p-bin` on PATH and let this function take the name `p`:\n\n" +
			"    eval \"$(p-bin init zsh)\"       # zsh, in ~/.zshrc\n" +
			"    eval \"$(p-bin init bash)\"      # bash, in ~/.bashrc\n" +
			"    p-bin init fish | source        # fish, in ~/.config/fish/config.fish\n\n" +
			"For tab completion, which is a separate concern, see `p completion <shell>`.",
		Flags: []ucli.Flag{
			&ucli.StringFlag{
				Name:  flagBin,
				Usage: "name of the p binary the wrapper calls",
				Value: shell.DefaultBinary,
				Local: true,
			},
			&ucli.StringFlag{
				Name:  flagFunc,
				Usage: "name of the shell function to define",
				Value: shell.DefaultFunc,
				Local: true,
			},
		},
		Action: func(_ context.Context, cmd *ucli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 1 {
				return fmt.Errorf("init: expected one shell name (%v), got %d arguments",
					shell.Supported(), len(args))
			}
			script, err := shell.Shim(args[0], shell.Options{
				Binary: cmd.String(flagBin),
				Func:   cmd.String(flagFunc),
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(a.stdout, script)
			return err
		},
	}
}
