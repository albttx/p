package vcs

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// ExecRunner is exercised with a harmless binary rather than a real git, so
// the production wiring is covered without touching the network.

func TestExecRunnerRun(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	r := ExecRunner{Stdout: &stdout, Stderr: &stderr}

	if err := r.Run(context.Background(), "echo", "cloned"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "cloned" {
		t.Errorf("stdout = %q, want %q", got, "cloned")
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

func TestExecRunnerErrorNamesTheCommand(t *testing.T) {
	t.Parallel()

	r := ExecRunner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	err := r.Run(context.Background(), "false", "--nope")
	if err == nil {
		t.Fatal("Run() should report a non-zero exit")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Errorf("error %q should name the command", err)
	}

	if _, err := r.Output(context.Background(), "false"); err == nil {
		t.Fatal("Output() should report a non-zero exit")
	}
}
