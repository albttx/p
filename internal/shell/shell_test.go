package shell

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden shim files")

func TestShimGolden(t *testing.T) {
	t.Parallel()

	for _, name := range Supported() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Shim(name, Options{})
			if err != nil {
				t.Fatalf("Shim(%q) error = %v", name, err)
			}

			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("write %s: %v", golden, err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read %s: %v (run: go test ./internal/shell -update)", golden, err)
			}
			if got != string(want) {
				t.Errorf("Shim(%q) does not match %s.\n--- got ---\n%s\n--- want ---\n%s",
					name, golden, got, want)
			}
		})
	}
}

func TestShimContract(t *testing.T) {
	t.Parallel()

	for _, name := range Supported() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Shim(name, Options{})
			if err != nil {
				t.Fatalf("Shim(%q) error = %v", name, err)
			}

			// Every shim must define the function, call the binary, test for
			// the sentinel and cd. A shim missing any of these is broken in a
			// way the golden file alone would happily enshrine.
			for _, want := range []string{DefaultFunc, DefaultBinary, SentinelCD, "cd"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s shim does not mention %q:\n%s", name, want, got)
				}
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("%s shim should end with a newline", name)
			}
		})
	}
}

func TestShimCustomNames(t *testing.T) {
	t.Parallel()

	for _, name := range Supported() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Shim(name, Options{Binary: "proj-bin", Func: "proj"})
			if err != nil {
				t.Fatalf("Shim(%q) error = %v", name, err)
			}
			if !strings.Contains(got, "proj-bin") {
				t.Errorf("%s shim does not call proj-bin:\n%s", name, got)
			}
			// The default names must not leak through as the function name.
			if strings.Contains(got, DefaultFunc+"()") || strings.Contains(got, "function "+DefaultFunc+" ") {
				t.Errorf("%s shim still defines the default function name:\n%s", name, got)
			}
		})
	}
}

func TestShimUnsupported(t *testing.T) {
	t.Parallel()

	tests := []string{"", "powershell", "csh", "nu", "sh"}
	for _, name := range tests {
		t.Run("shell="+name, func(t *testing.T) {
			t.Parallel()
			if _, err := Shim(name, Options{}); err == nil {
				t.Fatalf("Shim(%q) should have failed", name)
			}
		})
	}
}

func TestShimAcceptsMessyShellNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ZSH", "  bash  ", "Fish"} {
		if _, err := Shim(name, Options{}); err != nil {
			t.Errorf("Shim(%q) error = %v, want it to be normalised", name, err)
		}
	}
}

func TestSupported(t *testing.T) {
	t.Parallel()

	want := []string{"bash", "fish", "zsh"}
	got := Supported()
	if len(got) != len(want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Supported()[%d] = %q, want %q (should be sorted)", i, got[i], want[i])
		}
	}
}
