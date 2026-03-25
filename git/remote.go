package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// CanonicalName returns the "owner/repo" identifier for this repository,
// parsed from the origin remote URL. Falls back to the directory basename
// if no origin remote is configured.
func (r *Repo) CanonicalName() (string, error) {
	var buf, stderr bytes.Buffer
	cmd := exec.Command("git", "config", "remote.origin.url")
	cmd.Dir = r.Dir
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// No origin remote — fall back to directory name.
		return filepath.Base(r.Dir), nil
	}
	url := strings.TrimSpace(buf.String())
	if url == "" {
		return filepath.Base(r.Dir), nil
	}
	name := ParseCanonicalName(url)
	if name == "" {
		return "", fmt.Errorf("could not parse canonical name from remote URL: %s", url)
	}
	return name, nil
}

// ParseCanonicalName extracts "owner/repo" from a remote URL.
// Supports HTTPS, SSH URL, and SSH shorthand formats.
func ParseCanonicalName(rawURL string) string {
	s := strings.TrimRight(rawURL, "/")
	s = strings.TrimSuffix(s, ".git")

	var path string
	if strings.Contains(s, "://") {
		// HTTPS or SSH URL: https://github.com/owner/repo or ssh://git@github.com/owner/repo
		idx := strings.Index(s, "://")
		afterScheme := s[idx+3:]
		// Strip user@host
		if slashIdx := strings.Index(afterScheme, "/"); slashIdx >= 0 {
			path = afterScheme[slashIdx+1:]
		}
	} else if idx := strings.Index(s, ":"); idx >= 0 && !strings.Contains(s[:idx], "/") {
		// SSH shorthand: git@github.com:owner/repo
		path = s[idx+1:]
	} else {
		// Local path or bare name
		path = s
	}

	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}
