package cli

import (
	"strings"
	"testing"

	"github.com/albttx/p/internal/shell"
)

// TestNavigateTmux covers session-per-project navigation: with the config's
// tmux key (or --tmux) on, `p <query>` ensures the session and puts the user
// in it instead of emitting a cd.
func TestNavigateTmux(t *testing.T) {
	t.Parallel()

	const session = "gnolang/gno"

	tests := []struct {
		name        string
		configOn    bool // Params.NavigateTmux, i.e. tmux: true in config.yaml
		args        []string
		existing    bool // the session is already up
		inTmux      bool
		stdoutTTY   bool
		wantArgv    []string // tmux argvs, has-session probes dropped
		wantOut     string   // exact stdout
		wantCreated bool     // new-session issued
	}{
		{
			name: "--tmux creates the missing session and delegates the attach",
			args: []string{"gno", "--tmux"}, stdoutTTY: false,
			wantOut:     shell.SentinelTmux + session + "\n",
			wantCreated: true,
		},
		{
			name: "--tmux switches when the session exists and p runs inside tmux",
			args: []string{"gno", "--tmux"}, existing: true, inTmux: true,
			wantArgv: []string{"tmux switch-client -t=" + session},
			wantOut:  "",
		},
		{
			name: "--tmux attaches in-process on a terminal",
			args: []string{"gno", "--tmux"}, existing: true, stdoutTTY: true,
			wantArgv: []string{"tmux attach -t=" + session},
			wantOut:  "",
		},
		{
			name:     "config tmux key enables it without the flag",
			configOn: true,
			args:     []string{"gno"}, existing: true, inTmux: true,
			wantArgv: []string{"tmux switch-client -t=" + session},
			wantOut:  "",
		},
		{
			name:     "--tmux=false overrides the config back to a plain cd",
			configOn: true,
			args:     []string{"gno", "--tmux=false"}, existing: true, inTmux: true,
			wantArgv: nil,
			wantOut:  "", // asserted separately: the cd sentinel
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, allRepos()...)
			h.navigateTmux = tt.configOn
			h.stdoutTTY = tt.stdoutTTY
			if tt.existing {
				h.tmux.existing[session] = true
			}
			if tt.inTmux {
				h.env["TMUX"] = "/private/tmp/tmux-501/default,999,0"
			}

			if err := h.run(tt.args...); err != nil {
				t.Fatalf("p %v: %v", tt.args, err)
			}

			var got []string
			created := false
			for _, argv := range h.tmux.argvs() {
				if strings.Contains(argv, "has-session") {
					continue
				}
				if strings.Contains(argv, "new-session") {
					created = true
					continue
				}
				got = append(got, argv)
			}
			if created != tt.wantCreated {
				t.Errorf("new-session issued = %v, want %v (all: %v)", created, tt.wantCreated, h.tmux.argvs())
			}
			assertArgv(t, "tmux", got, tt.wantArgv)

			if strings.HasSuffix(tt.args[len(tt.args)-1], "=false") {
				if !strings.HasPrefix(h.out(), shell.SentinelCD) {
					t.Errorf("stdout = %q, want the cd sentinel", h.out())
				}
				return
			}
			if h.out() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", h.out(), tt.wantOut)
			}
			if strings.Contains(h.out(), shell.SentinelCD) {
				t.Errorf("tmux navigation must not also cd: %q", h.out())
			}
		})
	}
}

// TestNavigateTmuxKeepsStdoutCleanOnFailure extends the sentinel safety
// property to the tmux path: a miss must not leave a session behind or leak
// anything onto stdout.
func TestNavigateTmuxKeepsStdoutCleanOnFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t, allRepos()...)
	h.stdoutTTY = false

	if err := h.run("nonexistent", "--tmux"); err == nil {
		t.Fatal("p nonexistent --tmux should fail")
	}
	if h.out() != "" {
		t.Errorf("stdout = %q, want empty", h.out())
	}
	if len(h.tmux.calls) != 0 {
		t.Errorf("a failed resolve touched tmux: %v", h.tmux.argvs())
	}
}
