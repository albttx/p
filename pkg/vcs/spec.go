// Package vcs turns the short repository references people actually type into
// clone URLs and destination paths, and clones them.
//
// A spec is anything that names a repository: "github.com/albttx/p",
// "albttx/p", "git@github.com:albttx/p.git" or "https://github.com/albttx/p".
// [ParseSpec] normalises all of them into a [Spec], from which a clone URL and
// a checkout path follow:
//
//	spec, err := vcs.ParseSpec("albttx/p", vcs.DefaultHost)
//	if err != nil {
//		return err
//	}
//
//	git := vcs.Git{Runner: vcs.ExecRunner{}}
//	return git.Clone(ctx, spec.CloneURL(false), spec.Dest("/Users/albttx/go/src"))
//	// git clone git@github.com:albttx/p.git /Users/albttx/go/src/github.com/albttx/p
//
// The host is decided by how many path segments a spec has, never by whether a
// segment contains a dot. That is what lets repositories named after domains —
// example.com, kontacts.dev, albttx.tech — parse correctly instead of having
// their own name mistaken for a forge.
//
// Cloning goes through a [Runner], so tests can assert the exact argv without
// running git or touching the network. This package defines its own Runner and
// [ExecRunner] rather than sharing them with a sibling package: the few lines
// of duplication buy two public packages that do not depend on each other.
package vcs

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// DefaultHost is the conventional host to assume when a spec names only
// owner/repo. Pass it to [ParseSpec], or pass any other host to change the
// assumption.
const DefaultHost = "github.com"

// defaultSSHUser is the user in a generated scp-style SSH URL when a spec did
// not supply one.
const defaultSSHUser = "git"

// Spec identifies one repository, in the pieces needed to both fetch it and
// decide where it belongs on disk.
type Spec struct {
	// Host is the forge, for example "github.com".
	Host string
	// Owner is the user or organisation, for example "albttx".
	Owner string
	// Repo is the repository name with any ".git" suffix removed. It may
	// itself look like a domain, for example "kontacts.dev".
	Repo string
	// User is the SSH user for [Spec.SSHURL]. [ParseSpec] fills it from the
	// spec when one was given and otherwise defaults it to "git".
	User string
}

// ParseSpec accepts any of:
//
//	github.com/albttx/p        host/owner/repo
//	albttx/p                   owner/repo, host defaults to defaultHost
//	git@github.com:albttx/p.git
//	ssh://git@github.com/albttx/p.git
//	https://github.com/albttx/p
//
// A trailing ".git" is stripped from the repository name. The host is decided
// by how many path segments the spec has, never by whether a segment contains
// a dot, so repositories whose names are themselves domains — example.com,
// kontacts.dev, albttx.tech — parse correctly.
func ParseSpec(spec, defaultHost string) (Spec, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Spec{}, errors.New("empty repository spec")
	}
	if defaultHost == "" {
		defaultHost = DefaultHost
	}

	var host, path, user string

	switch {
	case strings.Contains(spec, "://"):
		u, err := url.Parse(spec)
		if err != nil {
			return Spec{}, fmt.Errorf("parse url %q: %w", spec, err)
		}
		host = u.Hostname()
		path = u.Path
		if u.User != nil {
			user = u.User.Username()
		}
		if host == "" {
			return Spec{}, fmt.Errorf("parse url %q: no host", spec)
		}

	case isSCPLike(spec):
		userHost, rest, _ := strings.Cut(spec, ":")
		if u, h, ok := strings.Cut(userHost, "@"); ok {
			user, host = u, h
		} else {
			host = userHost
		}
		path = rest
		if host == "" {
			return Spec{}, fmt.Errorf("parse %q: no host", spec)
		}

	default:
		path = spec
	}

	segments, err := splitPath(path)
	if err != nil {
		return Spec{}, fmt.Errorf("parse %q: %w", spec, err)
	}

	s := Spec{Host: host, User: user}
	switch {
	case host != "":
		// The host came from a URL, so every segment belongs to the path.
		if len(segments) != 2 {
			return Spec{}, fmt.Errorf("parse %q: want owner/repo after host, got %d segment(s)", spec, len(segments))
		}
		s.Owner, s.Repo = segments[0], segments[1]

	case len(segments) == 3:
		s.Host, s.Owner, s.Repo = segments[0], segments[1], segments[2]

	case len(segments) == 2:
		s.Host, s.Owner, s.Repo = defaultHost, segments[0], segments[1]

	default:
		return Spec{}, fmt.Errorf("parse %q: want [host/]owner/repo, got %d segment(s)", spec, len(segments))
	}

	if s.User == "" {
		s.User = defaultSSHUser
	}
	return s, nil
}

// isSCPLike reports whether spec uses git's scp-style syntax,
// user@host:path. Callers must rule out scheme URLs first, since "https" also
// precedes a colon with no slash.
func isSCPLike(spec string) bool {
	i := strings.Index(spec, ":")
	return i > 0 && !strings.Contains(spec[:i], "/")
}

// splitPath splits a repository path into validated segments and strips a
// trailing ".git" from the last one.
func splitPath(path string) ([]string, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil, errors.New("empty path")
	}

	segments := strings.Split(path, "/")
	last := len(segments) - 1
	if name := segments[last]; len(name) > len(".git") && strings.HasSuffix(name, ".git") {
		segments[last] = strings.TrimSuffix(name, ".git")
	}

	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return nil, fmt.Errorf("invalid path segment %q", seg)
		}
	}
	return segments, nil
}

// SSHURL returns the scp-style SSH clone URL, "user@host:owner/repo.git".
func (s Spec) SSHURL() string {
	user := s.User
	if user == "" {
		user = defaultSSHUser
	}
	return fmt.Sprintf("%s@%s:%s/%s.git", user, s.Host, s.Owner, s.Repo)
}

// HTTPSURL returns the HTTPS clone URL, "https://host/owner/repo.git".
func (s Spec) HTTPSURL() string {
	return fmt.Sprintf("https://%s/%s/%s.git", s.Host, s.Owner, s.Repo)
}

// CloneURL returns [Spec.HTTPSURL] when https is true and [Spec.SSHURL]
// otherwise, so a caller can pass a flag straight through.
func (s Spec) CloneURL(https bool) string {
	if https {
		return s.HTTPSURL()
	}
	return s.SSHURL()
}

// Dest returns where this repository belongs under root, as
// root/host/owner/repo. It does not create or check anything on disk.
func (s Spec) Dest(root string) string {
	return filepath.Join(root, s.Host, s.Owner, s.Repo)
}

// Full returns the "host/owner/repo" form of the spec.
func (s Spec) Full() string { return s.Host + "/" + s.Owner + "/" + s.Repo }
