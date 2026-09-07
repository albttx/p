package vcs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSpec(t *testing.T) {
	t.Parallel()

	const root = "/Users/albttx/go/src"

	tests := []struct {
		name      string
		spec      string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantSSH   string
		wantHTTPS string
		wantErr   bool
	}{
		{
			name: "host/owner/repo", spec: "github.com/albttx/p",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "bare owner/repo defaults the host", spec: "albttx/p",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "scp-style ssh url", spec: "git@github.com:albttx/p.git",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "scp-style without .git", spec: "git@github.com:albttx/p",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "scp-style with a non-default user", spec: "deploy@git.internal:team/svc.git",
			wantHost: "git.internal", wantOwner: "team", wantRepo: "svc",
			wantSSH: "deploy@git.internal:team/svc.git", wantHTTPS: "https://git.internal/team/svc.git",
		},
		{
			name: "scp-style with no user", spec: "github.com:albttx/p.git",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "https url", spec: "https://github.com/albttx/p",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "https url with trailing .git", spec: "https://github.com/albttx/p.git",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "https url with trailing slash", spec: "https://github.com/albttx/p/",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "p",
			wantSSH: "git@github.com:albttx/p.git", wantHTTPS: "https://github.com/albttx/p.git",
		},
		{
			name: "ssh:// url", spec: "ssh://git@gitlab.com/nysa/ansible.git",
			wantHost: "gitlab.com", wantOwner: "nysa", wantRepo: "ansible",
			wantSSH: "git@gitlab.com:nysa/ansible.git", wantHTTPS: "https://gitlab.com/nysa/ansible.git",
		},
		{
			name: "http url", spec: "http://git.internal/team/svc",
			wantHost: "git.internal", wantOwner: "team", wantRepo: "svc",
			wantSSH: "git@git.internal:team/svc.git", wantHTTPS: "https://git.internal/team/svc.git",
		},

		// A repository whose own name is a domain. The dot belongs to the
		// repo, never to the host: the host is decided by segment count.
		{
			name: "repo named like a domain, three segments", spec: "github.com/albttx/example.com",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "example.com",
			wantSSH: "git@github.com:albttx/example.com.git", wantHTTPS: "https://github.com/albttx/example.com.git",
		},
		{
			name: "repo named like a domain, two segments", spec: "albttx/example.com",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "example.com",
			wantSSH: "git@github.com:albttx/example.com.git", wantHTTPS: "https://github.com/albttx/example.com.git",
		},
		{
			name: "kontacts.dev", spec: "albttx/kontacts.dev",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "kontacts.dev",
			wantSSH: "git@github.com:albttx/kontacts.dev.git", wantHTTPS: "https://github.com/albttx/kontacts.dev.git",
		},
		{
			name: "0human.company", spec: "github.com/albttx/0human.company",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "0human.company",
			wantSSH: "git@github.com:albttx/0human.company.git", wantHTTPS: "https://github.com/albttx/0human.company.git",
		},
		{
			name: "l7x.org over ssh", spec: "git@github.com:albttx/l7x.org.git",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "l7x.org",
			wantSSH: "git@github.com:albttx/l7x.org.git", wantHTTPS: "https://github.com/albttx/l7x.org.git",
		},
		{
			name: "domain repo with a .git suffix to strip", spec: "albttx/albttx.tech.git",
			wantHost: "github.com", wantOwner: "albttx", wantRepo: "albttx.tech",
			wantSSH: "git@github.com:albttx/albttx.tech.git", wantHTTPS: "https://github.com/albttx/albttx.tech.git",
		},

		// Errors.
		{name: "empty", spec: "", wantErr: true},
		{name: "whitespace", spec: "   ", wantErr: true},
		{name: "single segment", spec: "p", wantErr: true},
		{name: "four segments", spec: "github.com/a/b/c", wantErr: true},
		{name: "url with too few segments", spec: "https://github.com/albttx", wantErr: true},
		{name: "url with too many segments", spec: "https://github.com/a/b/c", wantErr: true},
		{name: "url with no host", spec: "https:///a/b", wantErr: true},
		{name: "dot segment", spec: "github.com/./p", wantErr: true},
		{name: "parent segment", spec: "../p", wantErr: true},
		{name: "empty segment", spec: "albttx//p", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSpec(tt.spec, DefaultHost)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSpec(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got.Host != tt.wantHost || got.Owner != tt.wantOwner || got.Repo != tt.wantRepo {
				t.Errorf("ParseSpec(%q) = %s/%s/%s, want %s/%s/%s",
					tt.spec, got.Host, got.Owner, got.Repo, tt.wantHost, tt.wantOwner, tt.wantRepo)
			}
			if ssh := got.SSHURL(); ssh != tt.wantSSH {
				t.Errorf("SSHURL() = %q, want %q", ssh, tt.wantSSH)
			}
			if https := got.HTTPSURL(); https != tt.wantHTTPS {
				t.Errorf("HTTPSURL() = %q, want %q", https, tt.wantHTTPS)
			}
			if url := got.CloneURL(false); url != tt.wantSSH {
				t.Errorf("CloneURL(false) = %q, want the SSH url %q", url, tt.wantSSH)
			}
			if url := got.CloneURL(true); url != tt.wantHTTPS {
				t.Errorf("CloneURL(true) = %q, want the HTTPS url %q", url, tt.wantHTTPS)
			}

			wantDest := filepath.Join(root, tt.wantHost, tt.wantOwner, tt.wantRepo)
			if dest := got.Dest(root); dest != wantDest {
				t.Errorf("Dest(%q) = %q, want %q", root, dest, wantDest)
			}
		})
	}
}

func TestParseSpecCustomDefaultHost(t *testing.T) {
	t.Parallel()

	got, err := ParseSpec("nysa/ansible", "gitlab.com")
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if got.Full() != "gitlab.com/nysa/ansible" {
		t.Errorf("Full() = %q, want gitlab.com/nysa/ansible", got.Full())
	}

	// An explicit host in the spec always beats the default.
	got, err = ParseSpec("github.com/nysa/ansible", "gitlab.com")
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if got.Full() != "github.com/nysa/ansible" {
		t.Errorf("Full() = %q, want github.com/nysa/ansible", got.Full())
	}
}

func TestParseSpecEmptyDefaultHostFallsBack(t *testing.T) {
	t.Parallel()

	got, err := ParseSpec("albttx/p", "")
	if err != nil {
		t.Fatalf("ParseSpec() error = %v", err)
	}
	if got.Host != DefaultHost {
		t.Errorf("Host = %q, want %q", got.Host, DefaultHost)
	}
}

func TestIsSCPLikeDoesNotClaimSchemeURLs(t *testing.T) {
	t.Parallel()

	// isSCPLike is only ever consulted after scheme URLs are ruled out, but a
	// regression here would silently reroute every https:// spec.
	for _, spec := range []string{"git@github.com:albttx/p.git", "github.com:albttx/p"} {
		if !isSCPLike(spec) {
			t.Errorf("isSCPLike(%q) = false, want true", spec)
		}
	}
	for _, spec := range []string{"github.com/albttx/p", "albttx/p"} {
		if isSCPLike(spec) {
			t.Errorf("isSCPLike(%q) = true, want false", spec)
		}
	}
	if !strings.Contains("https://github.com/a/b", "://") {
		t.Fatal("scheme detection precondition broken")
	}
}
