package remote

import "testing"

func TestParseGitHubRemote(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantSlug string
		wantHost string
		wantOK   bool
	}{
		{"scp ssh with .git", "git@github.com:owner/repo.git", "owner/repo", "github.com", true},
		{"scp ssh without .git", "git@github.com:owner/repo", "owner/repo", "github.com", true},
		{"scp ssh without user", "github.com:owner/repo.git", "owner/repo", "github.com", true},
		{"https with .git", "https://github.com/owner/repo.git", "owner/repo", "github.com", true},
		{"https without .git", "https://github.com/owner/repo", "owner/repo", "github.com", true},
		{"https trailing slash", "https://github.com/owner/repo/", "owner/repo", "github.com", true},
		{"http", "http://github.com/owner/repo.git", "owner/repo", "github.com", true},
		{"git protocol", "git://github.com/owner/repo.git", "owner/repo", "github.com", true},
		{"ssh url", "ssh://git@github.com/owner/repo.git", "owner/repo", "github.com", true},
		{"ssh url with port", "ssh://git@github.com:22/owner/repo.git", "owner/repo", "github.com", true},
		{"host case-insensitive", "https://GitHub.com/owner/repo.git", "owner/repo", "github.com", true},
		{"surrounding whitespace", "  https://github.com/owner/repo.git\n", "owner/repo", "github.com", true},

		{"gitlab scp is not github", "git@gitlab.com:group/repo.git", "group/repo", "gitlab.com", false},
		{"gitlab https is not github", "https://gitlab.com/group/repo.git", "group/repo", "gitlab.com", false},
		{"github enterprise is not github.com", "git@github.mycompany.com:owner/repo.git", "owner/repo", "github.mycompany.com", false},
		{"bitbucket is not github", "https://bitbucket.org/owner/repo.git", "owner/repo", "bitbucket.org", false},

		{"empty", "", "", "", false},
		{"garbage without a host", "not a url", "", "", false},
		{"host only", "https://github.com", "", "github.com", false},
		{"missing repo", "https://github.com/owner", "", "github.com", false},
		{"extra path segments", "https://github.com/owner/repo/extra", "", "github.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, host, ok := ParseGitHubRemote(tt.in)
			if slug != tt.wantSlug || host != tt.wantHost || ok != tt.wantOK {
				t.Errorf("ParseGitHubRemote(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.in, slug, host, ok, tt.wantSlug, tt.wantHost, tt.wantOK)
			}
		})
	}
}

func TestArchiveCommand(t *testing.T) {
	got := ArchiveCommand("owner/repo")
	want := "gh repo archive owner/repo --yes"
	if got != want {
		t.Errorf("ArchiveCommand() = %q, want %q", got, want)
	}
}
