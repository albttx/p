# p

A project switcher for a GOPATH-style source tree.

```sh
$ p nixpkgs
$ pwd
/Users/albttx/go/src/github.com/albttx/nixpkgs
```

Projects live at `$CODE_DIR/{host}/{owner}/{repo}`:

```
/Users/albttx/go/src/github.com/albttx/p
└──── CODE_DIR ────┘ └──host──┘ └owner┘└repo┘
```

`p <query>` resolves a short name to exactly one project and drops you in it.
`p add` clones a new one and opens a tmux session for it. Nothing else to
remember.

## Install

```sh
go install github.com/albttx/p/cmd/p@latest
```

Add these two lines to your shell config, **in this order** — the completion
script binds to the name `p`, so the function that defines it has to run first:

```sh
# ~/.zshrc
eval "$(p init zsh)"          # defines the `p` shell function
source <(p completion zsh)    # tab completion
```

`p init` prints a shell function that turns `p`'s output into a real `cd`,
since a child process can't change its parent shell's directory.

<details>
<summary>bash / fish</summary>

```sh
# ~/.bashrc
eval "$(p init bash)"
source <(p completion bash)

# ~/.config/fish/config.fish
p init fish | source
p completion fish | source
```
</details>

## Commands

| command | what it does |
|---|---|
| `p <query>` | resolve the query to one project and `cd` there |
| `p path <query>` | print the resolved absolute path only, for `$(...)` |
| `p list` | print every project as `host/owner/repo` |
| `p query [term]` | print every project the term matches |
| `p clone <spec>` | clone into `$CODE_DIR/{host}/{owner}/{repo}` — fetch only |
| `p add <spec>` | clone if missing, then create and attach a tmux session |
| `p new <spec>` | create a new local project, `git init` it, then session + attach |
| `p tmux` | ensure a tmux session per project, then attach |
| `p init <shell>` | print the `cd` wrapper for zsh, bash or fish |
| `p completion <shell>` | print tab completion for zsh, bash, fish or pwsh |

`p clone` is the inert half — it fetches and prints the destination, nothing
else, so it's safe for scripts and CI. `p add` is "start working on this":
clone if needed, spin up a tmux session named `owner/repo`, and attach to it.
`p new` is `add` for a project that has no remote yet — it creates the
directory and `git init`s it instead of cloning, so `p list` picks it up.

The commands that select projects — `p <query>`, `path`, `list`, `query` and
`tmux` — accept `--host`, `--owner` and `--exclude PATTERN` (repeatable) to
narrow the set. Run `p <command> --help` for the rest.

A query resolves in this order, stopping at the first tier with a match:

1. exact `host/owner/repo`
2. exact `owner/repo`
3. exact `repo`
4. case-insensitive substring of `repo` or `owner/repo`

Zero matches is an error. Several matches prints the candidates and fails
rather than guessing — narrow it with `--owner`, `--host`, or a longer query.

## Configuration

`CODE_DIR` is resolved in this order, with `~` and `$HOME` expanded:

1. the `$CODE_DIR` environment variable
2. `code_dir` in `~/.config/p/config.yaml`
3. `$HOME/codes`

A missing config file is not an error. `--code-dir` overrides everything.

<details>
<summary>example config.yaml</summary>

```yaml
# ~/.config/p/config.yaml
code_dir: ~/go/src
```
</details>

## Packages

The scanning and matching logic is importable:

| package | what it gives you |
|---|---|
| [`pkg/projectsearcher`](pkg/projectsearcher) | `Scan` a tree, then `Match` / `Filter` / `Resolve` a typed fragment to one project |
| [`pkg/tmux`](pkg/tmux) | session-per-project helpers over an injectable command runner |
| [`pkg/vcs`](pkg/vcs) | repository spec → clone URL → destination path |

## Development

```sh
go test -race -cover ./...
go test ./internal/shell -update   # refresh the shim golden files
```

## Credits

Inspired by [gfanton/project](https://github.com/gfanton/project) — same core
idea, navigate a `{owner}/{repo}` tree zoxide-style instead of typing paths.
`p` adds a host-qualified `{host}/{owner}/{repo}` layout and folds in the
tmux session-per-project workflow it was written to replace.
