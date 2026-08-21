package main

import (
	"bytes"
	"strings"
	"testing"

	gitpkg "github.com/nicwestvold/gwt/git"
)

type fakeSelectionList struct {
	choices    []gitpkg.WorktreeEntry
	activePath string
}

func (f fakeSelectionList) Choices() []gitpkg.WorktreeEntry { return f.choices }
func (f fakeSelectionList) ActivePath() string              { return f.activePath }
func (f fakeSelectionList) Render(selectedPath string, color bool) string {
	return "selected " + selectedPath + "\n"
}

func testSelectionList() fakeSelectionList {
	return fakeSelectionList{
		choices: []gitpkg.WorktreeEntry{
			{Branch: "main", Path: "/repo/main"},
			{Branch: "feature", Path: "/repo/feature"},
			{Branch: "review", Path: "/repo/review"},
		},
		activePath: "/repo/main",
	}
}

func TestRunWorktreeSelectorNavigation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "j", input: "j\r", want: "feature"},
		{name: "down arrow", input: "\033[B\r", want: "feature"},
		{name: "k wraps", input: "k\r", want: "review"},
		{name: "up arrow wraps", input: "\033[A\r", want: "review"},
		{name: "multiple moves", input: "jjk\r", want: "feature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			got, accepted, err := runWorktreeSelector(testSelectionList(), strings.NewReader(tt.input), &output, false)
			if err != nil {
				t.Fatal(err)
			}
			if !accepted || got.Branch != tt.want {
				t.Errorf("selection = (%q, %t), want (%q, true)", got.Branch, accepted, tt.want)
			}
			if !strings.Contains(output.String(), "↑/↓ or j/k move") {
				t.Errorf("selector help missing:\n%q", output.String())
			}
		})
	}
}

func TestRunWorktreeSelectorStartsAtActiveWorktree(t *testing.T) {
	list := testSelectionList()
	list.activePath = "/repo/feature"
	got, accepted, err := runWorktreeSelector(list, strings.NewReader("\r"), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted || got.Branch != "feature" {
		t.Errorf("selection = (%q, %t), want (feature, true)", got.Branch, accepted)
	}
}

func TestRunWorktreeSelectorCancel(t *testing.T) {
	for _, input := range []string{"q", string([]byte{3})} {
		got, accepted, err := runWorktreeSelector(testSelectionList(), strings.NewReader(input), &bytes.Buffer{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if accepted || got != (gitpkg.WorktreeEntry{}) {
			t.Errorf("canceled selection = (%+v, %t), want zero, false", got, accepted)
		}
	}
}

func TestRunWorktreeSelectorNoChoices(t *testing.T) {
	_, _, err := runWorktreeSelector(fakeSelectionList{}, strings.NewReader("\r"), &bytes.Buffer{}, false)
	if err == nil || !strings.Contains(err.Error(), "no branch-backed worktrees") {
		t.Fatalf("error = %v, want no-worktrees error", err)
	}
}
