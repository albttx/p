// Package shell generates the wrapper functions that turn p's cd sentinel
// into an actual directory change.
//
// A child process cannot change its parent shell's working directory, so p
// prints a sentinel line on stdout and a shell function does the cd. Only
// navigation emits the sentinel; everything else prints normally and every
// diagnostic goes to stderr, so the shim never cds to garbage.
package shell

import (
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// SentinelCD prefixes the one stdout line that asks the shell to change
// directory: "__P_CD__/abs/path".
const SentinelCD = "__P_CD__"

// DefaultBinary is the name the shim invokes. The shim itself takes the name
// "p", so the binary must be installed under a different one.
const DefaultBinary = "p-bin"

// DefaultFunc is the name of the generated shell function.
const DefaultFunc = "p"

// Options configure shim generation.
type Options struct {
	// Binary is the executable the shim calls. Defaults to [DefaultBinary].
	Binary string
	// Func is the shell function to define. Defaults to [DefaultFunc].
	Func string
}

func (o Options) withDefaults() Options {
	if o.Binary == "" {
		o.Binary = DefaultBinary
	}
	if o.Func == "" {
		o.Func = DefaultFunc
	}
	return o
}

// Shim returns the wrapper function for the named shell. Supported shells are
// zsh, bash and fish.
func Shim(name string, opts Options) (string, error) {
	tmpl, ok := shims[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return "", fmt.Errorf("unsupported shell %q: supported shells are %s", name, strings.Join(Supported(), ", "))
	}

	data := struct {
		Binary   string
		Func     string
		Sentinel string
	}{}
	o := opts.withDefaults()
	data.Binary, data.Func, data.Sentinel = o.Binary, o.Func, SentinelCD

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render %s shim: %w", name, err)
	}
	return b.String(), nil
}

// Supported returns the shell names [Shim] accepts, sorted.
func Supported() []string {
	names := make([]string, 0, len(shims))
	for name := range shims {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var shims = map[string]*template.Template{
	"zsh":  template.Must(template.New("zsh").Parse(zshShim)),
	"bash": template.Must(template.New("bash").Parse(bashShim)),
	"fish": template.Must(template.New("fish").Parse(fishShim)),
}

const zshShim = `# p shell integration for zsh. Add to ~/.zshrc, in this order:
#   eval "$({{.Binary}} init zsh)"          # 1. this cd wrapper
#   source <({{.Binary}} completion zsh)    # 2. tab completion, needs the wrapper
{{.Func}}() {
  local out
  out="$(command {{.Binary}} "$@")" || return $?
  case "$out" in
    {{.Sentinel}}*) builtin cd -- "${out#{{.Sentinel}}}" ;;
    *) [ -n "$out" ] && print -r -- "$out" ;;
  esac
  return 0
}
`

const bashShim = `# p shell integration for bash. Add to ~/.bashrc, in this order:
#   eval "$({{.Binary}} init bash)"          # 1. this cd wrapper
#   source <({{.Binary}} completion bash)    # 2. tab completion, needs the wrapper
{{.Func}}() {
  local out
  out="$(command {{.Binary}} "$@")" || return $?
  case "$out" in
    {{.Sentinel}}*) builtin cd -- "${out#{{.Sentinel}}}" ;;
    *) [ -n "$out" ] && printf '%s\n' "$out" ;;
  esac
  return 0
}
`

// fish differs in three ways that matter here: command substitution splits on
// newlines unless piped through "string collect", the exit status of a
// substitution has to be read from $pipestatus, and "string match" with no
// string argument reads stdin, so the empty case must be handled first.
const fishShim = `# p shell integration for fish. Add to config.fish, in this order:
#   {{.Binary}} init fish | source          # 1. this cd wrapper
#   {{.Binary}} completion fish | source    # 2. tab completion, needs the wrapper
function {{.Func}} --description 'jump to a project'
    set -l out (command {{.Binary}} $argv | string collect)
    set -l code $pipestatus[1]
    if test $code -ne 0
        return $code
    end
    if test -z "$out"
        return 0
    end
    if string match -q -- '{{.Sentinel}}*' "$out"
        set -l dir (string replace -- '{{.Sentinel}}' '' "$out")
        builtin cd $dir
        return $status
    end
    printf '%s\n' "$out"
    return 0
end
`
