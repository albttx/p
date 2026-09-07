package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	ucli "github.com/urfave/cli/v3"

	"github.com/albttx/p/internal/shell"
)

// completionFlag is the token urfave/cli appends when a shell asks for
// completions. It is unexported in the library, so it is restated here.
const completionFlag = "--generate-shell-completion"

// complete answers shell completion requests for the commands that take a
// project query.
//
// urfave/cli installs DefaultCompleteWithFlags when ShellComplete is nil and
// does not chain to it once a custom func is set, so the default is invoked
// explicitly to keep subcommand and flag suggestions.
func (a *app) complete(ctx context.Context, cmd *ucli.Command) {
	last := completionLastArg(os.Args)

	// "--owner <TAB>" and friends: complete the value, not the flag name, and
	// suppress the subcommand list that would otherwise be mixed in.
	if name, ok := flagAwaitingValue(last); ok {
		a.completeFlagValue(cmd, name)
		return
	}

	ucli.DefaultCompleteWithFlags(ctx, cmd)

	// The user is partway through a flag; project names are not candidates.
	if strings.HasPrefix(last, "-") {
		return
	}
	a.completeProjects(cmd)
}

// completeLastArg mirrors how urfave/cli locates the token being completed:
// the shell drops the partial word and appends the completion flag, so the
// interesting argument is the one before it.
func completionLastArg(args []string) string {
	// args[0] is the program name and args[len-1] is the completion flag the
	// shell appended, so a real preceding token needs at least three entries.
	// (urfave/cli's own helper skips this check and can hand back argv[0];
	// harmless there, but it would misdirect the dispatch below.)
	if len(args) < 3 {
		return ""
	}
	if last := args[len(args)-2]; last != completionFlag {
		return last
	}
	return ""
}

// flagAwaitingValue reports whether last is one of the flags whose *value* can
// be completed from the source tree.
func flagAwaitingValue(last string) (string, bool) {
	if !strings.HasPrefix(last, "-") {
		return "", false
	}
	name := strings.TrimLeft(last, "-")
	// "--owner=albttx" already carries its value.
	if strings.Contains(name, "=") {
		return "", false
	}
	if slices.Contains([]string{flagOwner, flagHost, flagFilter}, name) {
		return name, true
	}
	return "", false
}

// completeProjects prints every string the query resolver can turn into a
// single project.
//
// The shell strips the partial word before calling back, then filters the
// candidates by prefix on its side. So a candidate is only reachable if the
// user's own prefix starts it: "kont<TAB>" can never match
// "github.com/albttx/kontacts.dev". All three accepted query forms are
// therefore emitted — bare repo, owner/repo and host/owner/repo, matching
// resolution tiers 3, 2 and 1 — so that whichever way the user starts typing,
// something completes, and whatever completes is directly resolvable.
func (a *app) completeProjects(cmd *ucli.Command) {
	all, err := projects(cmd)
	if err != nil {
		// A completion stream is not a place to report errors: a bad
		// CODE_DIR must degrade to "no suggestions", never to noise in the
		// user's command line.
		return
	}

	counts := make(map[string]int, len(all))
	for _, p := range all {
		counts[p.Repo]++
	}

	out := newCompletionWriter(cmd.Root().Writer)
	for _, p := range all {
		if counts[p.Repo] > 1 {
			out.emit(p.Repo, plural(counts[p.Repo]))
		} else {
			out.emit(p.Repo, p.Full())
		}
		out.emit(p.Name(), p.Host)
		out.emit(p.Full(), "")
	}
}

// completeFlagValue prints the distinct values of one project field.
func (a *app) completeFlagValue(cmd *ucli.Command, flag string) {
	all, err := projects(cmd)
	if err != nil {
		return
	}

	out := newCompletionWriter(cmd.Root().Writer)
	switch flag {
	case flagOwner, flagHost:
		counts := make(map[string]int, len(all))
		for _, p := range all {
			if flag == flagOwner {
				counts[p.Owner]++
			} else {
				counts[p.Host]++
			}
		}
		for _, key := range slices.Sorted(maps.Keys(counts)) {
			out.emit(key, plural(counts[key]))
		}
	case flagFilter:
		a.completeProjects(cmd)
	}
}

// plural renders a project count for a completion description.
func plural(n int) string {
	if n == 1 {
		return "1 project"
	}
	return fmt.Sprintf("%d projects", n)
}

// completionWriter emits deduplicated "value:description" lines, the format
// zsh's _describe expects.
type completionWriter struct {
	w    io.Writer
	seen map[string]bool
}

func newCompletionWriter(w io.Writer) *completionWriter {
	return &completionWriter{w: w, seen: map[string]bool{}}
}

func (c *completionWriter) emit(value, description string) {
	// A candidate must never be mistakable for the cd sentinel: the shell shim
	// reads this same stdout and would try to cd into it.
	if value == "" || strings.HasPrefix(value, shell.SentinelCD) || c.seen[value] {
		return
	}
	c.seen[value] = true

	if description == "" {
		fmt.Fprintln(c.w, value)
		return
	}
	// _describe splits on the first colon, so a colon in the description
	// would truncate it.
	fmt.Fprintf(c.w, "%s:%s\n", value, strings.ReplaceAll(description, ":", " "))
}
