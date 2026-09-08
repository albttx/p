package cli

import (
	"strings"
	"testing"

	"github.com/albttx/p/internal/shell"
)

// TestFocusRoutes covers the three ways p can put the user into tmux, and in
// particular the one that shipped broken: outside tmux with stdout captured by
// the shell shim, where an in-process `tmux attach` fails with
// "open terminal failed: can't use /dev/tty".
func TestFocusRoutes(t *testing.T) {
	t.Parallel()

	const session = "albttx/gh-todoist"

	tests := []struct {
		name      string
		verb      string // add or new
		inTmux    bool
		stdoutTTY bool
		wantArgv  []string
		wantOut   string // exact stdout, "" means it must stay empty
	}{
		{
			name: "inside tmux switches, regardless of stdout",
			verb: "add", inTmux: true, stdoutTTY: false,
			wantArgv: []string{"tmux switch-client -t=" + session},
			wantOut:  "",
		},
		{
			name: "inside tmux switches on a terminal too",
			verb: "add", inTmux: true, stdoutTTY: true,
			wantArgv: []string{"tmux switch-client -t=" + session},
			wantOut:  "",
		},
		{
			name: "outside tmux on a terminal attaches in-process",
			verb: "add", inTmux: false, stdoutTTY: true,
			wantArgv: []string{"tmux attach -t=" + session},
			wantOut:  "",
		},
		{
			// The regression: stdout is the shim's pipe, so p must not try to
			// attach itself.
			name: "outside tmux with captured stdout delegates to the shim",
			verb: "add", inTmux: false, stdoutTTY: false,
			wantArgv: nil,
			wantOut:  shell.SentinelTmux + session + "\n",
		},
		{
			name: "p new takes the same route",
			verb: "new", inTmux: false, stdoutTTY: false,
			wantArgv: nil,
			wantOut:  shell.SentinelTmux + session + "\n",
		},
		{
			name: "p new switches inside tmux",
			verb: "new", inTmux: true, stdoutTTY: false,
			wantArgv: []string{"tmux switch-client -t=" + session},
			wantOut:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, "github.com/albttx/gh-todoist")
			h.stdoutTTY = tt.stdoutTTY
			h.tmux.existing[session] = true // session already up: isolate focus
			if tt.inTmux {
				h.env["TMUX"] = "/private/tmp/tmux-501/default,999,0"
			}

			if err := h.run(tt.verb, "albttx/gh-todoist"); err != nil {
				t.Fatalf("p %s: %v", tt.verb, err)
			}

			// Drop the has-session probe; this test is about what follows it.
			var got []string
			for _, argv := range h.tmux.argvs() {
				if !strings.Contains(argv, "has-session") {
					got = append(got, argv)
				}
			}
			assertArgv(t, "tmux", got, tt.wantArgv)

			if h.out() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", h.out(), tt.wantOut)
			}
		})
	}
}

// TestFocusSentinelIsSanitised matters because the shim passes the payload
// straight to `tmux attach -t=`, so it has to be the name tmux actually holds.
func TestFocusSentinelIsSanitised(t *testing.T) {
	t.Parallel()

	h := newHarness(t, "github.com/albttx/kontacts.dev")
	h.stdoutTTY = false
	h.tmux.existing["albttx/kontacts_dev"] = true

	if err := h.run("add", "albttx/kontacts.dev"); err != nil {
		t.Fatalf("p add: %v", err)
	}

	want := shell.SentinelTmux + "albttx/kontacts_dev\n"
	if h.out() != want {
		t.Errorf("stdout = %q, want %q", h.out(), want)
	}
	if strings.Contains(h.out(), "kontacts.dev") {
		t.Errorf("sentinel carries the unsanitised name: %q", h.out())
	}
}

// TestTmuxSentinelsCannotBeConfused guards the shim's dispatch: a __P_TMUX__
// line must never match the __P_CD__ arm, or the shell would cd into a session
// name.
func TestTmuxSentinelsCannotBeConfused(t *testing.T) {
	t.Parallel()

	if strings.HasPrefix(shell.SentinelTmux, shell.SentinelCD) ||
		strings.HasPrefix(shell.SentinelCD, shell.SentinelTmux) {
		t.Fatalf("sentinels %q and %q are prefixes of one another",
			shell.SentinelCD, shell.SentinelTmux)
	}

	h := newHarness(t, "github.com/albttx/gh-todoist")
	h.stdoutTTY = false
	h.tmux.existing["albttx/gh-todoist"] = true
	if err := h.run("add", "albttx/gh-todoist"); err != nil {
		t.Fatalf("p add: %v", err)
	}
	if strings.HasPrefix(h.out(), shell.SentinelCD) {
		t.Errorf("attach sentinel would be taken as a cd: %q", h.out())
	}
}

// TestBulkTmuxDelegatesAttach covers `p tmux`, which has the same terminal
// problem at the end of its run.
func TestBulkTmuxDelegatesAttach(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inTmux    bool
		stdoutTTY bool
		args      []string
		wantLast  string // last tmux argv, "" means none
		wantOut   string
	}{
		{
			name: "captured stdout delegates with an empty payload",
			args: []string{"tmux"}, stdoutTTY: false,
			wantLast: "",
			wantOut:  shell.SentinelTmux + "\n",
		},
		{
			name: "on a terminal it attaches in-process",
			args: []string{"tmux"}, stdoutTTY: true,
			wantLast: "tmux attach",
			wantOut:  "",
		},
		{
			name: "already inside tmux there is nothing to attach to",
			args: []string{"tmux"}, inTmux: true, stdoutTTY: false,
			wantLast: "",
			wantOut:  "",
		},
		{
			name: "--no-attach still attaches to nothing",
			args: []string{"tmux", "--no-attach"}, stdoutTTY: false,
			wantLast: "",
			wantOut:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, repoP)
			h.stdoutTTY = tt.stdoutTTY
			h.tmux.existing["albttx/p"] = true
			if tt.inTmux {
				h.env["TMUX"] = "/private/tmp/tmux-501/default,999,0"
			}

			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			argvs := h.tmux.argvs()
			last := ""
			if n := len(argvs); n > 0 && !strings.Contains(argvs[n-1], "has-session") {
				last = argvs[n-1]
			}
			if last != tt.wantLast {
				t.Errorf("last tmux argv = %q, want %q (all: %v)", last, tt.wantLast, argvs)
			}
			if h.out() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", h.out(), tt.wantOut)
			}
		})
	}
}

// TestDryRunStillPrintsAttachArgv keeps `p tmux --dry-run` honest: it shows
// commands, it does not delegate.
func TestDryRunStillPrintsAttachArgv(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoP)
	h.stdoutTTY = false
	if err := h.run("tmux", "--dry-run"); err != nil {
		t.Fatalf("p tmux --dry-run: %v", err)
	}
	if !strings.Contains(h.out(), "tmux attach") {
		t.Errorf("dry run should print the attach argv, got:\n%s", h.out())
	}
	if strings.Contains(h.out(), shell.SentinelTmux) {
		t.Errorf("dry run should not emit a sentinel, got:\n%s", h.out())
	}
}
