package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/albttx/p/internal/project"
)

// corpus mirrors the shape of the real source tree, including the repository
// basenames that genuinely collide across owners there.
func corpus() []project.Project {
	specs := []string{
		"github.com/albttx/p",
		"github.com/albttx/blog",
		"github.com/nysa-network/blog",
		"github.com/gnolang/gno",
		"github.com/gnolang/faucet",
		"github.com/nysa-network/faucet",
		"github.com/allinbits/infrastructure",
		"github.com/allinbits/networks",
		"github.com/nysa-network/networks",
		"github.com/albttx/nixpkgs",
		"github.com/NixOS/nixpkgs",
		"github.com/albttx/kontacts.dev",
		"github.com/albttx/albttx.tech",
		"github.com/albttx/0human.company",
		"gitlab.com/nysa/ansible",
		"gitlab.com/albttx/p",
	}

	out := make([]project.Project, 0, len(specs))
	for _, s := range specs {
		parts := strings.Split(s, "/")
		out = append(out, project.Project{
			Root:  "/src",
			Host:  parts[0],
			Owner: parts[1],
			Repo:  parts[2],
		})
	}
	project.Sort(out)
	return out
}

func fulls(projects []project.Project) []string {
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		out = append(out, p.Full())
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMatchTierPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		term string
		want []string
		why  string
	}{
		{
			name: "tier 1 exact host/owner/repo wins over the same repo elsewhere",
			term: "github.com/albttx/p",
			want: []string{"github.com/albttx/p"},
			why:  "gitlab.com/albttx/p also ends in /p but tier 1 is exact and stops the search",
		},
		{
			name: "tier 2 exact owner/repo",
			term: "gnolang/gno",
			want: []string{"github.com/gnolang/gno"},
		},
		{
			name: "tier 2 exact owner/repo can legitimately span hosts",
			term: "albttx/p",
			want: []string{"github.com/albttx/p", "gitlab.com/albttx/p"},
			why:  "same owner/repo on two hosts: ambiguous, but still tier 2",
		},
		{
			name: "tier 3 exact repo",
			term: "infrastructure",
			want: []string{"github.com/allinbits/infrastructure"},
		},
		{
			name: "tier 3 exact repo beats substring hits on other repos",
			term: "gno",
			want: []string{"github.com/gnolang/gno"},
			why:  "gnolang/faucet contains gno in its owner, but an exact repo match stops earlier",
		},
		{
			name: "tier 3 exact repo across owners is ambiguous, not narrowed",
			term: "nixpkgs",
			want: []string{"github.com/albttx/nixpkgs", "github.com/NixOS/nixpkgs"},
		},
		{
			name: "tier 4 case-insensitive substring of repo",
			term: "KONTACTS",
			want: []string{"github.com/albttx/kontacts.dev"},
		},
		{
			name: "tier 4 substring of repo, dotted name",
			term: ".dev",
			want: []string{"github.com/albttx/kontacts.dev"},
		},
		{
			name: "tier 5 substring of owner/repo when no repo matches",
			term: "nysa-network",
			want: []string{"github.com/nysa-network/blog", "github.com/nysa-network/faucet", "github.com/nysa-network/networks"},
			why:  "no repo name contains nysa-network, so the search falls through to owner/repo",
		},
		{
			name: "empty term matches everything",
			term: "",
			want: fulls(corpus()),
		},
		{
			name: "no match",
			term: "definitely-not-here",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fulls(Match(corpus(), tt.term))
			if !equal(got, tt.want) {
				t.Errorf("Match(%q) = %v, want %v (%s)", tt.term, got, tt.want, tt.why)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "host",
			opts: Options{Host: "gitlab.com"},
			want: []string{"gitlab.com/albttx/p", "gitlab.com/nysa/ansible"},
		},
		{
			name: "owner",
			opts: Options{Owner: "gnolang"},
			want: []string{"github.com/gnolang/faucet", "github.com/gnolang/gno"},
		},
		{
			name: "host and owner combine",
			opts: Options{Host: "gitlab.com", Owner: "albttx"},
			want: []string{"gitlab.com/albttx/p"},
		},
		{
			name: "owner match is case-insensitive",
			opts: Options{Owner: "NIXOS"},
			want: []string{"github.com/NixOS/nixpkgs"},
		},
		{
			name: "single exclude, substring",
			opts: Options{Owner: "nysa-network", Exclude: []string{"blog"}},
			want: []string{"github.com/nysa-network/faucet", "github.com/nysa-network/networks"},
		},
		{
			name: "repeated exclude drops the union",
			opts: Options{Owner: "nysa-network", Exclude: []string{"blog", "faucet"}},
			want: []string{"github.com/nysa-network/networks"},
		},
		{
			name: "repeated exclude across owners",
			opts: Options{Exclude: []string{"gnolang", "nysa", "allinbits", "nixos", "albttx"}},
			want: nil,
		},
		{
			name: "exclude glob",
			opts: Options{Owner: "albttx", Exclude: []string{"*.dev", "*.tech", "*.company"}},
			want: []string{"github.com/albttx/blog", "github.com/albttx/nixpkgs", "github.com/albttx/p", "gitlab.com/albttx/p"},
		},
		{
			name: "exclude is case-insensitive",
			opts: Options{Owner: "NixOS", Exclude: []string{"NIXPKGS"}},
			want: nil,
		},
		{
			name: "empty exclude pattern is ignored",
			opts: Options{Owner: "gnolang", Exclude: []string{""}},
			want: []string{"github.com/gnolang/faucet", "github.com/gnolang/gno"},
		},
		{
			name: "limit truncates",
			opts: Options{Host: "github.com", Limit: 3},
			want: []string{"github.com/albttx/0human.company", "github.com/albttx/albttx.tech", "github.com/albttx/blog"},
		},
		{
			name: "limit zero is unlimited",
			opts: Options{Owner: "gnolang", Limit: 0},
			want: []string{"github.com/gnolang/faucet", "github.com/gnolang/gno"},
		},
		{
			name: "limit larger than the result set",
			opts: Options{Owner: "gnolang", Limit: 99},
			want: []string{"github.com/gnolang/faucet", "github.com/gnolang/gno"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fulls(Filter(corpus(), tt.opts))
			if !equal(got, tt.want) {
				t.Errorf("Filter(%+v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

func TestFilterDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	in := corpus()
	before := fulls(in)
	Filter(in, Options{Owner: "albttx", Limit: 1})
	if !equal(fulls(in), before) {
		t.Errorf("Filter mutated its input: %v, want %v", fulls(in), before)
	}
}

func TestSearchAppliesLimitToMatchesNotCandidates(t *testing.T) {
	t.Parallel()

	// Three repos are named or contain "networks"-ish terms; limit must cut
	// the matches, not the pool that matching runs over.
	got := fulls(Search(corpus(), "networks", Options{Limit: 1}))
	want := []string{"github.com/allinbits/networks"}
	if !equal(got, want) {
		t.Errorf("Search(networks, limit 1) = %v, want %v", got, want)
	}

	got = fulls(Search(corpus(), "networks", Options{}))
	want = []string{"github.com/allinbits/networks", "github.com/nysa-network/networks"}
	if !equal(got, want) {
		t.Errorf("Search(networks) = %v, want %v", got, want)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		term          string
		opts          Options
		want          string
		wantNotFound  bool
		wantAmbiguous []string
	}{
		{
			name: "unique exact repo",
			term: "infrastructure",
			want: "github.com/allinbits/infrastructure",
		},
		{
			name: "unique via full path",
			term: "gitlab.com/albttx/p",
			want: "gitlab.com/albttx/p",
		},
		{
			name:          "ambiguous repo name across owners",
			term:          "blog",
			wantAmbiguous: []string{"github.com/albttx/blog", "github.com/nysa-network/blog"},
		},
		{
			name:          "ambiguous owner/repo across hosts",
			term:          "albttx/p",
			wantAmbiguous: []string{"github.com/albttx/p", "gitlab.com/albttx/p"},
		},
		{
			name: "ambiguity resolved by --host",
			term: "albttx/p",
			opts: Options{Host: "gitlab.com"},
			want: "gitlab.com/albttx/p",
		},
		{
			name: "ambiguity resolved by --owner",
			term: "nixpkgs",
			opts: Options{Owner: "NixOS"},
			want: "github.com/NixOS/nixpkgs",
		},
		{
			name: "ambiguity resolved by --exclude",
			term: "faucet",
			opts: Options{Exclude: []string{"nysa-network"}},
			want: "github.com/gnolang/faucet",
		},
		{
			name:         "zero match",
			term:         "nope",
			wantNotFound: true,
		},
		{
			name:         "filtered out entirely",
			term:         "gno",
			opts:         Options{Host: "gitlab.com"},
			wantNotFound: true,
		},
		{
			name: "limit does not silently disambiguate",
			term: "blog",
			opts: Options{Limit: 1},
			wantAmbiguous: []string{
				"github.com/albttx/blog",
				"github.com/nysa-network/blog",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(corpus(), tt.term, tt.opts)

			switch {
			case tt.wantNotFound:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("Resolve(%q) error = %v, want ErrNotFound", tt.term, err)
				}
			case tt.wantAmbiguous != nil:
				var ambig *AmbiguousError
				if !errors.As(err, &ambig) {
					t.Fatalf("Resolve(%q) error = %v, want *AmbiguousError", tt.term, err)
				}
				if !equal(fulls(ambig.Candidates), tt.wantAmbiguous) {
					t.Errorf("candidates = %v, want %v", fulls(ambig.Candidates), tt.wantAmbiguous)
				}
				// The message has to carry the candidates, since the CLI
				// prints it verbatim to stderr and stdout must stay empty.
				for _, want := range tt.wantAmbiguous {
					if !strings.Contains(ambig.Error(), want) {
						t.Errorf("AmbiguousError message %q does not list %q", ambig.Error(), want)
					}
				}
			default:
				if err != nil {
					t.Fatalf("Resolve(%q) error = %v", tt.term, err)
				}
				if got.Full() != tt.want {
					t.Errorf("Resolve(%q) = %q, want %q", tt.term, got.Full(), tt.want)
				}
			}
		})
	}
}
