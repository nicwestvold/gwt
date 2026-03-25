package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseCanonicalName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown/", "grafana/metrics-drilldown"},
		{"https://github.com/grafana/metrics-drilldown.git/", "grafana/metrics-drilldown"},
		{"git@github.com:grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"git@github.com:grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"ssh://git@github.com/grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"ssh://git@github.com/grafana/metrics-drilldown", "grafana/metrics-drilldown"},
		{"ssh://git@github.com:22/grafana/metrics-drilldown.git", "grafana/metrics-drilldown"},
		{"https://gitlab.com/org/subgroup/repo.git", "subgroup/repo"},
		{"git@gitlab.com:org/subgroup/repo.git", "subgroup/repo"},
		{"/path/to/repo", "to/repo"},
		{"repo", "repo"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := ParseCanonicalName(tt.url)
			if got != tt.want {
				t.Errorf("ParseCanonicalName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestCanonicalName(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")

	run := func(t *testing.T, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Env = gitEnv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", name, args, err)
		}
	}

	t.Run("with origin remote", func(t *testing.T) {
		tmp := t.TempDir()
		repoDir := filepath.Join(tmp, "myrepo")
		run(t, "git", "init", repoDir)
		run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")
		run(t, "git", "-C", repoDir, "remote", "add", "origin", "https://github.com/owner/myrepo.git")

		repoDir, _ = filepath.EvalSymlinks(repoDir)
		repo := &Repo{Dir: repoDir}
		got, err := repo.CanonicalName()
		if err != nil {
			t.Fatalf("CanonicalName() error: %v", err)
		}
		if got != "owner/myrepo" {
			t.Errorf("CanonicalName() = %q, want %q", got, "owner/myrepo")
		}
	})

	t.Run("without origin remote", func(t *testing.T) {
		tmp := t.TempDir()
		repoDir := filepath.Join(tmp, "localrepo")
		run(t, "git", "init", repoDir)
		run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")

		repoDir, _ = filepath.EvalSymlinks(repoDir)
		repo := &Repo{Dir: repoDir}
		got, err := repo.CanonicalName()
		if err != nil {
			t.Fatalf("CanonicalName() error: %v", err)
		}
		if got != "localrepo" {
			t.Errorf("CanonicalName() = %q, want %q", got, "localrepo")
		}
	})
}
