package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceEntry groups several registered repos that must be checked out as
// sibling worktrees sharing a branch.
type WorkspaceEntry struct {
	// Members lists repos by canonical name ("owner/repo") or unique short
	// last segment ("repo"). The first member is the default primary.
	Members []string `toml:"members"`
	// Primary is the member you cd into after add; followers mirror its branch.
	Primary string `toml:"primary,omitempty"`
	// Setup is a shell command run after all worktrees exist (optional).
	Setup string `toml:"setup,omitempty"`
	// SetupCwd is the member whose worktree Setup runs in (defaults to Primary).
	SetupCwd string `toml:"setup_cwd,omitempty"`
	// WorktreeRoot overrides where per-branch group dirs are created.
	WorktreeRoot string `toml:"worktree_root,omitempty"`
}

// lastSegment returns the final path segment of a canonical name,
// e.g. "grafana/grafana-enterprise" -> "grafana-enterprise".
func lastSegment(canonical string) string {
	if i := strings.LastIndex(canonical, "/"); i >= 0 {
		return canonical[i+1:]
	}
	return canonical
}

// WorkspaceForRepo returns the workspace that lists the given canonical repo
// name as a member (matched by canonical name or unique short segment).
func (c *Config) WorkspaceForRepo(canonical string) (string, WorkspaceEntry, bool) {
	short := lastSegment(canonical)
	for name, ws := range c.Workspaces {
		for _, m := range ws.Members {
			if m == canonical || m == short {
				return name, ws, true
			}
		}
	}
	return "", WorkspaceEntry{}, false
}

// ResolvedMember is a workspace member resolved to a concrete repo on disk.
type ResolvedMember struct {
	Name       string // canonical name as registered, e.g. "grafana/grafana"
	Short      string // last segment, used as the sibling directory name
	Path       string // repo path on disk
	MainBranch string // defaults to "main"
	IsPrimary  bool
}

// resolveMember maps a member reference (canonical or unique short segment)
// to its registered repo entry.
func (c *Config) resolveMember(ref string) (string, RepoEntry, error) {
	if e, ok := c.Repos[ref]; ok {
		return ref, e, nil
	}
	var matches []string
	for name := range c.Repos {
		if lastSegment(name) == ref {
			matches = append(matches, name)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], c.Repos[matches[0]], nil
	case 0:
		return "", RepoEntry{}, fmt.Errorf("member %q not registered in [repos]; run gwt init there", ref)
	default:
		return "", RepoEntry{}, fmt.Errorf("member %q is ambiguous; matches %v — use the full canonical name", ref, matches)
	}
}

// ResolveMembers resolves every member of a workspace, marking the primary.
// If Primary is empty, the first member is primary.
func (c *Config) ResolveMembers(ws WorkspaceEntry) ([]ResolvedMember, error) {
	if len(ws.Members) == 0 {
		return nil, fmt.Errorf("workspace has no members")
	}
	primaryCanon := ""
	if ws.Primary != "" {
		pc, _, err := c.resolveMember(ws.Primary)
		if err != nil {
			return nil, fmt.Errorf("primary: %w", err)
		}
		primaryCanon = pc
	}
	out := make([]ResolvedMember, 0, len(ws.Members))
	for i, ref := range ws.Members {
		canon, entry, err := c.resolveMember(ref)
		if err != nil {
			return nil, err
		}
		mb := entry.MainBranch
		if mb == "" {
			mb = "main"
		}
		out = append(out, ResolvedMember{
			Name:       canon,
			Short:      lastSegment(canon),
			Path:       entry.Path,
			MainBranch: mb,
			IsPrimary:  (primaryCanon == "" && i == 0) || canon == primaryCanon,
		})
	}
	return out, nil
}

// expandTilde expands a leading "~" to the user's home directory.
func expandTilde(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// ResolveWorktreeRoot returns the directory under which per-branch group dirs
// are created. Defaults to <dataDir>/worktrees/<workspace-name>.
func (ws WorkspaceEntry) ResolveWorktreeRoot(name string) (string, error) {
	if ws.WorktreeRoot != "" {
		return expandTilde(ws.WorktreeRoot)
	}
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "worktrees", name), nil
}
