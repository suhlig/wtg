// Package remote parses git remote URLs and derives host-specific actions from
// them.
//
// wtg itself is provider-agnostic: it talks to git rather than to a hosting
// service. This package is the one place that knows about GitHub, and only to
// help the user archive retired repos with the `gh` CLI (see
// docs/adr/0013-repo-archive.md).
package remote

import (
	"net/url"
	"strings"
)

// GitHubHost is the host v1 recognizes for archival. GitHub Enterprise and
// other forges are intentionally excluded for now, so a remote on any other
// host yields ok == false and no GitHub action is suggested.
const GitHubHost = "github.com"

// ParseGitHubRemote extracts the "owner/repo" slug and the hostname from a git
// remote URL.
//
// It understands the URL forms git produces in practice:
//
//	git@github.com:owner/repo.git
//	github.com:owner/repo.git
//	ssh://git@github.com/owner/repo.git
//	ssh://git@github.com:22/owner/repo.git
//	https://github.com/owner/repo.git
//	http://github.com/owner/repo
//
// host is the remote's hostname, lowercased and with any port stripped. ok
// reports whether the remote points at GitHub (GitHubHost) and a valid
// owner/repo slug was found. A non-GitHub remote (GitLab, GitHub Enterprise,
// Bitbucket, ...) yields ok == false but still fills in host and, when the path
// parses, ownerRepo, so the caller can explain why no GitHub action is offered.
// A URL that cannot be parsed at all yields an empty ownerRepo and host.
func ParseGitHubRemote(raw string) (ownerRepo, host string, ok bool) {
	host, path, parsed := splitRemote(raw)
	if !parsed {
		return "", "", false
	}
	slug, valid := ownerRepoSlug(path)
	if !valid {
		return "", host, false
	}
	return slug, host, host == GitHubHost
}

// ArchiveCommand returns the `gh` invocation that archives ownerRepo on GitHub.
// `gh repo archive` has existed since gh 2.32. Without --remote, wtg prints this
// command for the user to run; with --remote, SystemRunner executes the same
// command (see gh.go).
func ArchiveCommand(ownerRepo string) string {
	return "gh repo archive " + ownerRepo + " --yes"
}

// splitRemote splits a git remote URL into its hostname and path. It handles
// both URL forms (scheme://...) and the scp-like shortcut ([user@]host:path)
// that git accepts. The returned host is lowercased; the returned path keeps
// any leading slash so ownerRepoSlug can normalize it.
func splitRemote(raw string) (host, path string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", "", false
		}
		return strings.ToLower(u.Hostname()), u.Path, true
	}

	// scp-like syntax: [user@]host:path. git does not allow a port here (use
	// ssh:// for that), so the first colon separates the host from the path.
	rest := raw
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	colon := strings.Index(rest, ":")
	if colon <= 0 {
		return "", "", false
	}
	return strings.ToLower(rest[:colon]), rest[colon+1:], true
}

// ownerRepoSlug normalizes a remote path into an "owner/repo" slug. It reports
// invalid unless, after trimming slashes and a trailing ".git", the path is
// exactly two non-empty segments — the shape of a GitHub repository.
func ownerRepoSlug(path string) (string, bool) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if path == "" {
		return "", false
	}
	owner, repo, found := strings.Cut(path, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", false
	}
	return owner + "/" + repo, true
}
