# p

A project switcher for a GOPATH-style source tree.

Projects live at `$CODE_DIR/{host}/{owner}/{repo}`:

```
/Users/albttx/go/src/github.com/albttx/p
└──────── CODE_DIR ────────┘ └─host─┘ └owner┘ └repo┘
```

`p <query>` resolves a short name to exactly one of them and drops you in it.
`p tmux` opens a session per project. Nothing else to remember.

## Why it is fast

The obvious way to find every checkout is what the shell script this replaces
did:

```sh
find $HOME/go/src -name .git -type d -prune
```

`-prune` prunes the `.git` directory it just matched — not the repository. The
walk therefore descends into every `node_modules`, `vendor` and `target` inside
every checkout, and it misses linked worktrees entirely, because those record
`.git` as a *file*.

`p` stops at project depth instead. A directory three levels below the root is
a project when it holds a `.git` entry — directory or regular file — and either
way the walk prunes there, so nothing inside a checkout is ever read. Vendored
sub-repositories such as `github.com/albttx/sysphera/frontend` are excluded for
free, since a project is only ever recognised at exactly `{host}/{owner}/{repo}`.

Measured on a 185-repository tree with 361 `.git` entries in it:

| | time | results |
|---|---|---|
| `find … -name .git -type d -prune` | 9.16s | 171, including nested sub-repos, missing worktrees |
| `p list` | 0.073s | 185, exact |

## Install

```sh
go install github.com/albttx/p/cmd/p@latest
```

The binary must be installed under a different name from the shell function, so
rename it to `p-bin` somewhere on `PATH`:

```sh
mv "$(go env GOPATH)/bin/p" "$(go env GOPATH)/bin/p-bin"
```

Then add two lines to your shell config, **in this order**:

```sh
# ~/.zshrc
eval "$(p-bin init zsh)"          # 1. defines the `p` function that does the cd
source <(p-bin completion zsh)    # 2. tab completion for it

# ~/.bashrc
eval "$(p-bin init bash)"
source <(p-bin completion bash)

# ~/.config/fish/config.fish
p-bin init fish | source
p-bin completion fish | source
```

They do different jobs and you want both:

- **`p init`** defines the shell function named `p`. A program cannot change its
  parent shell's working directory, so the binary prints a sentinel line and
  this function performs the `cd`. Without it, `p gno` just prints a path.
- **`p completion`** wires up `<TAB>`. It completes subcommands, flags, **and
  all 185 of your project names** — see [Completion](#completion).

Order matters: the completion script binds to the name `p`, so the function has
to exist first. `p init` takes `--bin` and `--func` if you want different names.

## Configuration

`CODE_DIR` is resolved in this order, with `~` and `$HOME` expanded:

1. the `$CODE_DIR` environment variable
2. `code_dir` in `~/.config/p/config.yaml`
3. `$HOME/codes`

```yaml
# ~/.config/p/config.yaml
code_dir: ~/go/src
```

A missing config file is not an error. `--code-dir` overrides everything.

## Commands

| command | what it does |
|---|---|
| `p <query>` | resolve the query to one project and `cd` there |
| `p path <query>` | print the resolved absolute path only, for `$(...)` |
| `p list` | print every project as `host/owner/repo` |
| `p query [term]` | print every project the term matches |
| `p clone <spec>` | clone into `$CODE_DIR/{host}/{owner}/{repo}`, nothing else |
| `p add <spec>` | clone if missing, make the tmux session, and go there |
| `p tmux` | ensure a tmux session per project, then attach |
| `p init <shell>` | print the `cd` wrapper for zsh, bash or fish |
| `p completion <shell>` | print tab completions |

### Flags

Every command that selects projects — including bare `p <query>` — accepts the
same three selectors:

| selector | effect |
|---|---|
| `--host HOST` | only projects on that host |
| `--owner OWNER` | only projects with that owner |
| `--exclude PATTERN` | drop matching projects; repeatable |

On top of those:

| command | extra flags |
|---|---|
| `p list` | `--json`, `--path` |
| `p query` | `--limit N` (0 = unlimited), `--json`, `--path` |
| `p clone` | `--https` |
| `p add` | `--https`, `--no-attach` |
| `p tmux` | `--filter TERM`, `--no-attach`, `--dry-run` |
| `p init` | `--bin`, `--func` |
| global | `--code-dir` |

`--exclude` is a case-insensitive substring of `host/owner/repo`, or a glob if
it contains `*`, `?` or `[`.

## How a query resolves

The first tier that matches anything wins; later tiers are never consulted.

1. exact `host/owner/repo`
2. exact `owner/repo`
3. exact `repo`
4. case-insensitive substring of `repo`
5. case-insensitive substring of `owner/repo`

Exactly one match navigates. Zero matches is an error. Several matches prints
the candidates and fails, rather than guessing:

```
$ p blog
p: ambiguous query "blog": 2 matches
  github.com/albttx/blog
  github.com/nysa-network/blog
```

Narrow it with `--owner`, `--host`, `--exclude`, or a longer query:

```sh
p nysa-network/blog
p blog --owner albttx
```

### The sentinel protocol

Only navigation writes to stdout, and only one line:

```
__P_CD__/Users/albttx/go/src/github.com/gnolang/gno
```

Every diagnostic — ambiguity, misses, usage errors — goes to stderr, and
stdout stays empty whenever the command fails. That is what makes the shim
safe: it can only ever `cd` to a path `p` deliberately produced.

`p path` prints the same path with no sentinel, so it composes:

```sh
cd "$(p path gno)"
tar cf gno.tar "$(p path gno)"
```

## Getting a repository: `add` vs `clone`

`p add` is "start working on this project". It is the one you want:

```sh
p add github.com/albttx/lol
```

1. clones to `$CODE_DIR/github.com/albttx/lol` — **skipped if already there**
2. creates a detached tmux session `albttx/lol` rooted at it — skipped if it exists
3. puts you in it: `tmux attach` from outside tmux, `tmux switch-client` from inside

Every step is skip-if-done, so `p add` is safe to re-run; on a project you
already have it just drops you into the session. `--no-attach` does steps 1 and
2 only, which is handy for queueing several projects before jumping into one.

`p clone` is the inert half — it fetches and prints the destination, full stop.
No session, no attach, so it can never hang. That is the one for scripts and CI.

```sh
p clone github.com/albttx/example.com   # git@github.com:albttx/example.com.git
p clone albttx/example.com              # host defaults to github.com
p clone --https gnolang/gno             # https://github.com/gnolang/gno.git
p clone git@gitlab.com:nysa/ansible.git # full URLs work, destination derived
p clone https://github.com/albttx/p
```

Both accept the same spec forms and both default to SSH. The host is decided by
how many path segments the spec has, never by whether a segment contains a dot,
so repositories named after domains (`example.com`, `kontacts.dev`,
`albttx.tech`) land in the right place.

`p add` never prints the cd sentinel — it moves you via tmux, not via `cd` — and
its status messages go to stderr, so stdout stays clean.

## tmux

```sh
p tmux                                    # a session per project, then attach
p tmux --no-attach                        # create them, stay put
p tmux --filter gno                       # only matching projects
p tmux --exclude archive --exclude vendor # repeatable
p tmux --dry-run                          # print the argv, run nothing
```

Sessions are named `owner/repo`. Many real names contain dots, where `.` and
`:` are meaningful in tmux target specs, so every probe uses `has-session
-t=<name>` — the `=` prefix forces an exact match instead of fnmatch — and
every invocation passes argv directly to `exec` rather than building a shell
string.

Start with `--dry-run` the first time; a large tree is a lot of sessions. For a
single project, `p add` is the lighter way in.

## Completion

`<TAB>` completes subcommands, flags, and every project, at whichever point you
started typing:

```
$ p kontact<TAB>          ->  p kontacts.dev
$ p albttx/kontac<TAB>    ->  p albttx/kontacts.dev
$ p github.com/gnolang/gnoch<TAB>
                          ->  p github.com/gnolang/gnochess
$ p blog --owner gnol<TAB> ->  p blog --owner gnolang
```

The shell strips the partial word before asking, then filters the candidates by
prefix itself. So `p` offers all three resolvable forms of every project — bare
repo, `owner/repo` and `host/owner/repo` — which is why any of the above
completes. Everything offered is something the resolver accepts, and ambiguous
repo names are labelled with their count:

```
blog        2 projects
gno         github.com/gnolang/gno
```

`--owner` and `--host` complete their values from the same scan.

Completion is never allowed to fail loudly: if `CODE_DIR` is wrong it offers
nothing rather than spilling an error into your command line.

## Development

```sh
go test -race -cover ./...
go test ./internal/shell -update   # refresh the shim golden files
```

`internal/vcs` and `internal/tmux` each take a `Runner` interface, and `$TMUX`
is read through an injected lookup, so the tests assert the exact argv that
would be executed — attach vs switch-client included — and never invoke real
git or tmux.

## Credits

Inspired by [gfanton/project](https://github.com/gfanton/project) — a Go tool
with the same core idea: keep every checkout in a `{owner}/{repo}` tree, then
navigate it zoxide-style instead of typing paths. The directory layout, the
shell-integration approach, and the clone/list/query command shape all come
from there.

`p` differs mainly in scope: it targets a host-qualified
`{host}/{owner}/{repo}` tree, and folds in the tmux session-per-project
workflow it was written to replace.
