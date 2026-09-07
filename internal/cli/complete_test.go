package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/albttx/p/internal/shell"
)

func TestCompletionLastArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "bare query: the shell drops the partial word",
			args: []string{"p", completionFlag},
			want: "",
		},
		{
			name: "after a subcommand",
			args: []string{"p", "path", completionFlag},
			want: "path",
		},
		{
			name: "waiting for a flag value",
			args: []string{"p", "blog", "--owner", completionFlag},
			want: "--owner",
		},
		{
			name: "partial flag name is passed through",
			args: []string{"p", "--ow", completionFlag},
			want: "--ow",
		},
		{name: "empty", args: nil, want: ""},
		{name: "only the flag", args: []string{completionFlag}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := completionLastArg(tt.args); got != tt.want {
				t.Errorf("completionLastArg(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestFlagAwaitingValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		last     string
		wantName string
		wantOK   bool
	}{
		{last: "--owner", wantName: flagOwner, wantOK: true},
		{last: "--host", wantName: flagHost, wantOK: true},
		{last: "--filter", wantName: flagFilter, wantOK: true},
		{last: "-owner", wantName: flagOwner, wantOK: true},
		// Already carries its value, so nothing is pending.
		{last: "--owner=albttx"},
		// Not a value-completable flag.
		{last: "--json"},
		{last: "--limit"},
		// Not a flag at all.
		{last: "blog"},
		{last: ""},
	}

	for _, tt := range tests {
		t.Run("last="+tt.last, func(t *testing.T) {
			t.Parallel()
			name, ok := flagAwaitingValue(tt.last)
			if ok != tt.wantOK || name != tt.wantName {
				t.Errorf("flagAwaitingValue(%q) = (%q, %v), want (%q, %v)",
					tt.last, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestCompletionWriterDedupesAndGuardsTheSentinel(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	w := newCompletionWriter(&b)

	w.emit("gno", "github.com/gnolang/gno")
	w.emit("gno", "duplicate, must be dropped")
	w.emit("plain", "")
	w.emit("", "empty value is dropped")
	// A candidate that looks like the cd sentinel must never be emitted: the
	// shim reads this same stdout.
	w.emit(shell.SentinelCD+"/etc", "hostile")
	// A colon in a description would truncate it in zsh's _describe.
	w.emit("colon", "a:b")

	want := "gno:github.com/gnolang/gno\nplain\ncolon:a b\n"
	if b.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", b.String(), want)
	}
}

// completionLines runs a completion request and splits the result.
func completionLines(t *testing.T, h *harness, args ...string) []string {
	t.Helper()

	if err := h.runCompletion(args...); err != nil {
		t.Fatalf("completion for %v: %v", args, err)
	}
	out := strings.TrimSuffix(h.out(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// TestCompleteProjects is not parallel: completion reads os.Args.
func TestCompleteProjects(t *testing.T) {
	h := newHarness(t, allRepos()...)
	got := completionLines(t, h)

	// Subcommands still come first; the default completer is chained, not
	// replaced.
	for _, want := range []string{"clone", "add", "list", "query", "tmux", "path", "init"} {
		if !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, want+":") }) {
			t.Errorf("completion is missing subcommand %q", want)
		}
	}

	// All three resolvable forms are offered, because the shell filters by
	// prefix on its side and the user may start typing at any of them.
	for _, want := range []string{
		"kontacts.dev:github.com/albttx/kontacts.dev",
		"albttx/kontacts.dev:github.com",
		"github.com/albttx/kontacts.dev",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("completion is missing %q\ngot: %v", want, got)
		}
	}

	// A repo name shared by several projects is described by its count rather
	// than by a single misleading path.
	if !slices.Contains(got, "blog:2 projects") {
		t.Errorf("completion should describe the ambiguous repo name, got: %v", got)
	}
	// A unique repo name is described by the project it resolves to.
	if !slices.Contains(got, "gno:github.com/gnolang/gno") {
		t.Errorf("completion should describe the unique repo name, got: %v", got)
	}

	// Nothing may look like a cd sentinel, and nothing may repeat.
	seen := map[string]bool{}
	for _, line := range got {
		value, _, _ := strings.Cut(line, ":")
		if strings.HasPrefix(value, shell.SentinelCD) {
			t.Errorf("completion emitted a sentinel-like candidate: %q", line)
		}
		if seen[value] {
			t.Errorf("completion emitted duplicate candidate %q", value)
		}
		seen[value] = true
	}
}

func TestCompleteFlagValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
		// none of these should include subcommands or project names
		reject []string
	}{
		{
			name:   "--owner lists owners with counts",
			args:   []string{"--owner"},
			want:   []string{"albttx:3 projects", "gnolang:1 project", "nysa:1 project", "nysa-network:1 project"},
			reject: []string{"clone:", "github.com/albttx/p"},
		},
		{
			name:   "--host lists hosts with counts",
			args:   []string{"--host"},
			want:   []string{"github.com:5 projects", "gitlab.com:1 project"},
			reject: []string{"clone:", "albttx/p:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, allRepos()...)
			got := completionLines(t, h, tt.args...)

			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("completion for %v is missing %q\ngot: %v", tt.args, want, got)
				}
			}
			for _, bad := range tt.reject {
				for _, line := range got {
					if strings.HasPrefix(line, bad) {
						t.Errorf("completion for %v should not offer %q, got line %q", tt.args, bad, line)
					}
				}
			}
		})
	}
}

// TestCompletePartialFlagOffersNoProjects keeps the candidate list meaningful:
// someone typing "--" wants flags, not 500 project names.
func TestCompletePartialFlagOffersNoProjects(t *testing.T) {
	h := newHarness(t, allRepos()...)
	got := completionLines(t, h, "--ow")

	for _, line := range got {
		if strings.Contains(line, "github.com/") && !strings.HasPrefix(line, "-") {
			t.Errorf("flag completion leaked a project name: %q", line)
		}
	}
	if !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "--owner") }) {
		t.Errorf("flag completion should suggest --owner, got: %v", got)
	}
}

// TestCompletionNeverFailsLoudly is the safety property: a broken CODE_DIR
// must produce silence, not an error injected into the user's command line.
func TestCompletionNeverFailsLoudly(t *testing.T) {
	h := newHarness(t)
	h.root = filepath.Join(h.root, "does-not-exist")

	if err := h.runCompletion(); err != nil {
		t.Fatalf("completion with a bad CODE_DIR returned an error: %v", err)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("completion wrote to stderr: %q", h.stderr.String())
	}
	// Subcommands still complete; only the project list is missing.
	for _, line := range strings.Split(strings.TrimSpace(h.out()), "\n") {
		if strings.Contains(line, "does-not-exist") || strings.Contains(strings.ToLower(line), "error") {
			t.Errorf("completion leaked a diagnostic: %q", line)
		}
	}
}
