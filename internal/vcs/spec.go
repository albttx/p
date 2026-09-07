// Package vcs turns repository specs into clone URLs and destination paths,
// and clones them.
package vcs

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// DefaultHost is used when a spec names only owner/repo.
const DefaultHost = "github.com"

// DefaultSSHUser is the user in a generated scp-style SSH URL.
const DefaultSSHUser = "git"

// Spec identifies one repository.
type Spec struct {
	Host  string
	Owner string
	Repo  string
	// User is the SSH user, "git" unless the spec supplied another one.
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
		s.User = DefaultSSHUser
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

// SSHURL returns the scp-style SSH clone URL.
func (s Spec) SSHURL() string {
	user := s.User
	if user == "" {
		user = DefaultSSHUser
	}
	return fmt.Sprintf("%s@%s:%s/%s.git", user, s.Host, s.Owner, s.Repo)
}

// HTTPSURL returns the HTTPS clone URL.
func (s Spec) HTTPSURL() string {
	return fmt.Sprintf("https://%s/%s/%s.git", s.Host, s.Owner, s.Repo)
}

// CloneURL returns the HTTPS URL when https is true and the SSH URL
// otherwise. SSH is the default.
func (s Spec) CloneURL(https bool) string {
	if https {
		return s.HTTPSURL()
	}
	return s.SSHURL()
}

// Dest returns the checkout path for this spec under root.
func (s Spec) Dest(root string) string {
	return filepath.Join(root, s.Host, s.Owner, s.Repo)
}

// Full returns "host/owner/repo".
func (s Spec) Full() string { return s.Host + "/" + s.Owner + "/" + s.Repo }
