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

// SentinelCD prefixes the stdout line that asks the shell to change directory:
// "__P_CD__/abs/path". It is written only by navigation, `p <query>`.
const SentinelCD = "__P_CD__"

// SentinelTmux prefixes the stdout line that asks the shell to attach to a
// tmux session: "__P_TMUX__owner/repo_name".
//
// It exists for the same reason as [SentinelCD]. A child process cannot change
// its parent's working directory, and it equally cannot take over the parent's
// terminal: "tmux attach" needs the controlling terminal, and p's stdout is a
// pipe inside the shim's command substitution, so an in-process attach fails
// with "open terminal failed: can't use /dev/tty". Delegating to the shell
// function, which runs after the substitution has closed, is the only way it
// can work.
//
// The payload is already in [tmux.SessionName] form, so the shim can pass it
// straight to "tmux attach -t=". An empty payload means "attach to the server
// without naming a session", which is what `p tmux` wants.
const SentinelTmux = "__P_TMUX__"

// DefaultBinary is the name the shim invokes.
//
// It is the same name as the generated function, which is safe because every
// shim calls "command <binary>": in POSIX sh, zsh, bash and fish alike,
// "command" suppresses shell-function lookup and resolves the name on PATH.
// Without that prefix the function would call itself and recurse forever,
// which is why [Shim] is tested for it.
const DefaultBinary = "p"

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
		Binary       string
		Func         string
		Sentinel     string
		SentinelTmux string
	}{}
	o := opts.withDefaults()
	data.Binary, data.Func = o.Binary, o.Func
	data.Sentinel, data.SentinelTmux = SentinelCD, SentinelTmux

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
    {{.SentinelTmux}}*)
      local session="${out#{{.SentinelTmux}}}"
      if [ -n "$session" ]; then
        command tmux attach -t="$session"
      else
        command tmux attach
      fi
      return $? ;;
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
    {{.SentinelTmux}}*)
      local session="${out#{{.SentinelTmux}}}"
      if [ -n "$session" ]; then
        command tmux attach -t="$session"
      else
        command tmux attach
      fi
      return $? ;;
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
    if string match -q -- '{{.SentinelTmux}}*' "$out"
        set -l session (string replace -- '{{.SentinelTmux}}' '' "$out")
        if test -n "$session"
            command tmux attach -t="$session"
        else
            command tmux attach
        end
        return $status
    end
    printf '%s\n' "$out"
    return 0
end
`
