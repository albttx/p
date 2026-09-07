package shell

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
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

			// Every shim must call the binary, test for the sentinel and cd.
			// A shim missing any of these is broken in a way the golden file
			// alone would happily enshrine.
			for _, want := range []string{"command " + DefaultBinary, SentinelCD, "cd"} {
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
			// --bin is no longer the way to avoid a name clash (see
			// TestShimAlwaysUsesCommandPrefix) but it still has to work for
			// anyone who installed the binary under another name.
			if !strings.Contains(got, "command proj-bin") {
				t.Errorf("%s shim does not invoke `command proj-bin`:\n%s", name, got)
			}
			if strings.Contains(got, "command "+DefaultBinary+" ") {
				t.Errorf("%s shim still calls the default binary:\n%s", name, got)
			}
			// The function must take the requested name, not the default.
			if !strings.Contains(got, "proj() {") && !strings.Contains(got, "function proj ") {
				t.Errorf("%s shim does not define the function `proj`:\n%s", name, got)
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

// invocationOf matches the named binary where a shell expects a command word:
// at the start of a line, or just inside "$(", "(" or after a pipe. Requiring
// a trailing space is what separates an invocation from the function
// definition "p() {" and from unrelated words like "printf" or "$pipestatus".
// Group 1 captures the "command " prefix when present.
func invocationOf(bin string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)(?:\$\(|\(|\||^)[ \t]*(command[ \t]+)?` + regexp.QuoteMeta(bin) + `[ \t]`)
}

// TestShimAlwaysUsesCommandPrefix guards the property that lets the shell
// function and the binary share the name "p".
//
// `command NAME` suppresses shell-function lookup in POSIX sh, zsh, bash and
// fish alike, so the function `p` calling `command p` reaches the binary on
// PATH. Drop that prefix and the function calls itself: infinite recursion and
// a hung shell. Nothing else in the suite would notice — the golden files
// would simply record the broken form — so this is asserted explicitly, for
// every shell, at the default name and a custom one.
func TestShimAlwaysUsesCommandPrefix(t *testing.T) {
	t.Parallel()

	for _, name := range Supported() {
		for _, bin := range []string{DefaultBinary, "proj-bin"} {
			t.Run(name+"/"+bin, func(t *testing.T) {
				t.Parallel()

				got, err := Shim(name, Options{Binary: bin})
				if err != nil {
					t.Fatalf("Shim(%q) error = %v", name, err)
				}

				body := shimBody(got)
				matches := invocationOf(bin).FindAllStringSubmatch(body, -1)
				if len(matches) == 0 {
					t.Fatalf("%s shim never invokes %q:\n%s", name, bin, body)
				}
				for _, m := range matches {
					if m[1] == "" {
						t.Errorf("%s shim invokes %q without the `command` prefix, which would recurse: %q",
							name, bin, strings.TrimSpace(m[0]))
					}
				}
			})
		}
	}
}

// TestInvocationDetectorCatchesRecursion checks the checker: if the matcher
// above could not tell a bare call from a guarded one, the test that depends
// on it would pass silently forever.
func TestInvocationDetectorCatchesRecursion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		wantMatch   bool
		wantGuarded bool
	}{
		{name: "zsh guarded", line: `  out="$(command p "$@")" || return $?`, wantMatch: true, wantGuarded: true},
		{name: "zsh bare recurses", line: `  out="$(p "$@")" || return $?`, wantMatch: true},
		{name: "fish guarded", line: `    set -l out (command p $argv | string collect)`, wantMatch: true, wantGuarded: true},
		{name: "fish bare recurses", line: `    set -l out (p $argv | string collect)`, wantMatch: true},
		{name: "start of line bare", line: `p list`, wantMatch: true},
		// Things that must not be mistaken for an invocation.
		{name: "function definition", line: `p() {`},
		{name: "printf", line: `    printf '%s\n' "$out"`},
		{name: "pipestatus", line: `    set -l code $pipestatus[1]`},
		{name: "string replace", line: `    set -l dir (string replace -- '__P_CD__' '' "$out")`},
	}

	re := invocationOf("p")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := re.FindStringSubmatch(tt.line)
			if (m != nil) != tt.wantMatch {
				t.Fatalf("invocationOf(p).match(%q) = %v, want match %v", tt.line, m != nil, tt.wantMatch)
			}
			if tt.wantMatch && (m[1] != "") != tt.wantGuarded {
				t.Errorf("line %q guarded = %v, want %v", tt.line, m[1] != "", tt.wantGuarded)
			}
		})
	}
}

// shimBody strips the leading comment block so the install-instruction lines,
// which legitimately name the binary without `command`, are not scanned.
func shimBody(shim string) string {
	var b strings.Builder
	for _, line := range strings.Split(shim, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
