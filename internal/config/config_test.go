package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env builds a Getenv func from a map.
func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// writeConfig writes a config.yaml under dir and returns its path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	const home = "/home/albttx"

	tests := []struct {
		name       string
		vars       map[string]string
		configBody string // written only when non-empty
		noFile     bool   // point ConfigPath at a file that does not exist
		want       string
		wantSource string
		wantErr    bool
	}{
		{
			name:       "env beats file",
			vars:       map[string]string{EnvCodeDir: "/from/env"},
			configBody: "code_dir: /from/file\n",
			want:       "/from/env",
			wantSource: "env",
		},
		{
			name:       "file beats default",
			configBody: "code_dir: /from/file\n",
			want:       "/from/file",
			wantSource: "file",
		},
		{
			name:       "default when neither is set",
			noFile:     true,
			want:       filepath.Join(home, DefaultDirName),
			wantSource: "default",
		},
		{
			name:       "empty env value falls through to the file",
			vars:       map[string]string{EnvCodeDir: ""},
			configBody: "code_dir: /from/file\n",
			want:       "/from/file",
			wantSource: "file",
		},
		{
			name:       "whitespace-only env value falls through",
			vars:       map[string]string{EnvCodeDir: "   "},
			configBody: "code_dir: /from/file\n",
			want:       "/from/file",
			wantSource: "file",
		},
		{
			name:       "missing config file is not an error",
			noFile:     true,
			want:       filepath.Join(home, DefaultDirName),
			wantSource: "default",
		},
		{
			name:       "empty config file falls through to the default",
			configBody: "",
			want:       filepath.Join(home, DefaultDirName),
			wantSource: "default",
		},
		{
			name:       "config file with no code_dir key falls through",
			configBody: "other: value\n",
			want:       filepath.Join(home, DefaultDirName),
			wantSource: "default",
		},
		{
			name:       "malformed yaml is an error",
			configBody: "code_dir: [unclosed\n",
			wantErr:    true,
		},

		// Expansion, from every source.
		{
			name:       "tilde from env",
			vars:       map[string]string{EnvCodeDir: "~/go/src"},
			noFile:     true,
			want:       filepath.Join(home, "go", "src"),
			wantSource: "env",
		},
		{
			name:       "bare tilde from env",
			vars:       map[string]string{EnvCodeDir: "~"},
			noFile:     true,
			want:       home,
			wantSource: "env",
		},
		{
			name:       "$HOME from env",
			vars:       map[string]string{EnvCodeDir: "$HOME/go/src"},
			noFile:     true,
			want:       filepath.Join(home, "go", "src"),
			wantSource: "env",
		},
		{
			name:       "${HOME} from env",
			vars:       map[string]string{EnvCodeDir: "${HOME}/go/src"},
			noFile:     true,
			want:       filepath.Join(home, "go", "src"),
			wantSource: "env",
		},
		{
			name:       "tilde from file",
			configBody: "code_dir: ~/go/src\n",
			want:       filepath.Join(home, "go", "src"),
			wantSource: "file",
		},
		{
			name:       "$HOME from file",
			configBody: "code_dir: $HOME/codes\n",
			want:       filepath.Join(home, "codes"),
			wantSource: "file",
		},
		{
			name:       "quoted value from file",
			configBody: `code_dir: "~/go/src"` + "\n",
			want:       filepath.Join(home, "go", "src"),
			wantSource: "file",
		},
		{
			name:       "surrounding whitespace is trimmed",
			configBody: "code_dir: \"  /from/file  \"\n",
			want:       "/from/file",
			wantSource: "file",
		},
		{
			name:       "an arbitrary env var expands too",
			vars:       map[string]string{EnvCodeDir: "$GOPATH/src", "GOPATH": "/opt/go"},
			noFile:     true,
			want:       "/opt/go/src",
			wantSource: "env",
		},
		{
			name:       "a tilde in the middle is not a home reference",
			vars:       map[string]string{EnvCodeDir: "/src/~backup"},
			noFile:     true,
			want:       "/src/~backup",
			wantSource: "env",
		},
		{
			name:       "path is cleaned",
			vars:       map[string]string{EnvCodeDir: "/go/src/../src/"},
			noFile:     true,
			want:       "/go/src",
			wantSource: "env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			if !tt.noFile {
				configPath = writeConfig(t, dir, tt.configBody)
			}

			got, err := Resolve(Options{
				Getenv:     env(tt.vars),
				Home:       home,
				ConfigPath: configPath,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.CodeDir != tt.want {
				t.Errorf("CodeDir = %q, want %q", got.CodeDir, tt.want)
			}
			if got.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tt.wantSource)
			}
		})
	}
}

func TestResolveRequiresGetenv(t *testing.T) {
	t.Parallel()

	if _, err := Resolve(Options{Home: "/home/albttx"}); err == nil {
		t.Fatal("Resolve() without Getenv should fail")
	}
}

func TestResolveRelativePathBecomesAbsolute(t *testing.T) {
	t.Parallel()

	got, err := Resolve(Options{
		Getenv: env(map[string]string{EnvCodeDir: "relative/src"}),
		Home:   "/home/albttx",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !filepath.IsAbs(got.CodeDir) {
		t.Errorf("CodeDir = %q, want an absolute path", got.CodeDir)
	}
	if !strings.HasSuffix(got.CodeDir, filepath.Join("relative", "src")) {
		t.Errorf("CodeDir = %q, want it to end in relative/src", got.CodeDir)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	const home = "/home/albttx"

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "absolute", path: "/go/src", want: "/go/src"},
		{name: "tilde slash", path: "~/go/src", want: filepath.Join(home, "go", "src")},
		{name: "bare tilde", path: "~", want: home},
		{name: "HOME var", path: "$HOME/go", want: filepath.Join(home, "go")},
		{name: "braced HOME var", path: "${HOME}/go", want: filepath.Join(home, "go")},
		{name: "unknown var expands to empty", path: "$NOPE/go", want: "/go"},
		{name: "empty", path: "", wantErr: true},
		{name: "expands to nothing", path: "$NOPE", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Expand(tt.path, home, func(string) string { return "" })
			if (err != nil) != tt.wantErr {
				t.Fatalf("Expand(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	want := filepath.Join("/home/albttx", ".config", "p", "config.yaml")
	if got := DefaultConfigPath("/home/albttx"); got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}
