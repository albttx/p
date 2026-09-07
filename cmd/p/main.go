// Command p navigates a GOPATH-style source tree.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/cli"
	"github.com/albttx/p/internal/config"
)

// version is overridable at build time:
//
//	go build -ldflags "-X main.version=$(git describe --tags)" ./cmd/p
var version = "dev"

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "p:", err)
		os.Exit(exitCode(err))
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	cmd := cli.New(cli.Params{
		Version: version,
		CodeDir: cfg.CodeDir,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	// Errors are reported by main, on stderr, with a single "p:" prefix.
	// Leaving the library's handler in place would print them a second time
	// and call os.Exit from inside Run.
	cmd.ExitErrHandler = func(context.Context, *ucli.Command, error) {}

	return cmd.Run(ctx, args)
}

// exitCode maps an error to a process exit status.
func exitCode(err error) int {
	var coder ucli.ExitCoder
	if errors.As(err, &coder) && coder.ExitCode() != 0 {
		return coder.ExitCode()
	}
	return 1
}
