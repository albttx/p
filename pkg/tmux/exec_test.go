package tmux

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// These exercise the real os/exec path with a harmless binary rather than a
// tmux server, so the wiring that production uses is covered without needing
// tmux to be installed or running.

func TestExecRunnerRun(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	r := ExecRunner{Stdout: &stdout, Stderr: &stderr}

	if err := r.Run(context.Background(), "echo", "albttx/kontacts.dev"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "albttx/kontacts.dev" {
		t.Errorf("stdout = %q, want %q", got, "albttx/kontacts.dev")
	}
}

func TestExecRunnerOutput(t *testing.T) {
	t.Parallel()

	out, err := ExecRunner{Stderr: &bytes.Buffer{}}.Output(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "hello" {
		t.Errorf("Output() = %q, want %q", got, "hello")
	}
}

func TestExecRunnerPropagatesFailure(t *testing.T) {
	t.Parallel()

	r := ExecRunner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if err := r.Run(context.Background(), "false"); err == nil {
		t.Fatal("Run() should report a non-zero exit")
	}
	if err := r.Run(context.Background(), "definitely-not-a-real-binary-xyz"); err == nil {
		t.Fatal("Run() should report a missing binary")
	}
}

func TestExecRunnerRespectsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := ExecRunner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if err := r.Run(ctx, "echo", "hi"); err == nil {
		t.Fatal("Run() with a cancelled context should fail")
	}
}

func TestTerminalRunnerAlwaysUsable(t *testing.T) {
	t.Parallel()

	// Under `go test` there is usually no controlling terminal, which is the
	// fallback path: it must still return a working runner whose stdout is
	// never the process stdout, since that channel carries the cd sentinel.
	r, release := TerminalRunner()
	defer release()

	var sink bytes.Buffer
	r.Stdout = &sink
	r.Stderr = &sink
	if err := r.Run(context.Background(), "echo", "ok"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strings.TrimSpace(sink.String()) != "ok" {
		t.Errorf("output = %q, want ok", sink.String())
	}
}
