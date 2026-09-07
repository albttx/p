package cli

import (
	"context"
	"encoding/json"
	"fmt"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/project"
	"github.com/albttx/p/internal/query"
)

const (
	flagJSON = "json"
	flagPath = "path"
)

// listCommand prints every project in the tree.
func listCommand(a *app) *ucli.Command {
	return &ucli.Command{
		Name:  "list",
		Usage: "list every project in the source tree",
		Flags: append(selectorFlags(),
			&ucli.BoolFlag{Name: flagJSON, Usage: "print JSON objects instead of names", Local: true},
			&ucli.BoolFlag{Name: flagPath, Usage: "print absolute paths instead of names", Local: true},
		),
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			if err := ctxDone(ctx); err != nil {
				return err
			}
			all, err := projects(cmd)
			if err != nil {
				return err
			}
			matches := query.Filter(all, selectorOptions(cmd))
			return a.printProjects(matches, cmd.Bool(flagJSON), cmd.Bool(flagPath))
		},
	}
}

// jsonProject is the wire shape of a project in --json output.
type jsonProject struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Full  string `json:"full"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

// printProjects renders a result set in the requested form.
func (a *app) printProjects(projects []project.Project, asJSON, asPath bool) error {
	if asJSON {
		out := make([]jsonProject, 0, len(projects))
		for _, p := range projects {
			out = append(out, jsonProject{
				Host: p.Host, Owner: p.Owner, Repo: p.Repo,
				Full: p.Full(), Name: p.Name(), Path: p.Path(),
			})
		}
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		return nil
	}

	for _, p := range projects {
		line := p.Full()
		if asPath {
			line = p.Path()
		}
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}
