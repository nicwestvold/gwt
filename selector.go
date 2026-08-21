package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	gitpkg "github.com/nicwestvold/gwt/git"
	"golang.org/x/term"
)

type worktreeSelectionList interface {
	Choices() []gitpkg.WorktreeEntry
	ActivePath() string
	Render(selectedPath string, color bool) string
}

type selectorKey int

const (
	selectorIgnore selectorKey = iota
	selectorUp
	selectorDown
	selectorAccept
	selectorCancel
)

func readSelectorKey(r *bufio.Reader) (selectorKey, error) {
	b, err := r.ReadByte()
	if err != nil {
		return selectorIgnore, err
	}
	switch b {
	case 'k':
		return selectorUp, nil
	case 'j':
		return selectorDown, nil
	case '\r', '\n':
		return selectorAccept, nil
	case 'q', 3, 4: // q, Ctrl-C, Ctrl-D
		return selectorCancel, nil
	case 0x1b:
		prefix, err := r.ReadByte()
		if err != nil {
			return selectorIgnore, err
		}
		if prefix != '[' && prefix != 'O' {
			return selectorIgnore, nil
		}
		direction, err := r.ReadByte()
		if err != nil {
			return selectorIgnore, err
		}
		switch direction {
		case 'A':
			return selectorUp, nil
		case 'B':
			return selectorDown, nil
		}
	}
	return selectorIgnore, nil
}

func moveSelection(index, delta, count int) int {
	if count == 0 {
		return 0
	}
	return (index + delta + count) % count
}

func initialSelection(choices []gitpkg.WorktreeEntry, activePath string) int {
	for i, choice := range choices {
		if choice.Path == activePath {
			return i
		}
	}
	return 0
}

func selectorFrame(list worktreeSelectionList, selectedPath string, color bool) string {
	return list.Render(selectedPath, color) + "\n  ↑/↓ or j/k move · enter select · q cancel\n"
}

func writeSelectorFrame(w io.Writer, frame string) (int, error) {
	terminalText := strings.ReplaceAll(frame, "\n", "\r\n")
	_, err := io.WriteString(w, terminalText)
	return strings.Count(frame, "\n"), err
}

func clearSelectorFrame(w io.Writer, lines int) {
	if lines > 0 {
		_, _ = fmt.Fprintf(w, "\033[%dA\r\033[J", lines)
	}
}

// runWorktreeSelector owns the interactive loop but not terminal setup, which
// keeps navigation and rendering testable with ordinary readers and writers.
func runWorktreeSelector(list worktreeSelectionList, input io.Reader, output io.Writer, color bool) (gitpkg.WorktreeEntry, bool, error) {
	choices := list.Choices()
	if len(choices) == 0 {
		return gitpkg.WorktreeEntry{}, false, fmt.Errorf("no branch-backed worktrees found")
	}
	selected := initialSelection(choices, list.ActivePath())
	reader := bufio.NewReader(input)
	lines, err := writeSelectorFrame(output, selectorFrame(list, choices[selected].Path, color))
	if err != nil {
		return gitpkg.WorktreeEntry{}, false, err
	}
	for {
		key, err := readSelectorKey(reader)
		if err != nil {
			clearSelectorFrame(output, lines)
			return gitpkg.WorktreeEntry{}, false, err
		}
		switch key {
		case selectorUp:
			selected = moveSelection(selected, -1, len(choices))
		case selectorDown:
			selected = moveSelection(selected, 1, len(choices))
		case selectorAccept:
			clearSelectorFrame(output, lines)
			return choices[selected], true, nil
		case selectorCancel:
			clearSelectorFrame(output, lines)
			return gitpkg.WorktreeEntry{}, false, nil
		default:
			continue
		}
		clearSelectorFrame(output, lines)
		lines, err = writeSelectorFrame(output, selectorFrame(list, choices[selected].Path, color))
		if err != nil {
			return gitpkg.WorktreeEntry{}, false, err
		}
	}
}

func selectWorktree(list worktreeSelectionList) (gitpkg.WorktreeEntry, bool, error) {
	inputFD := int(os.Stdin.Fd())
	outputFD := int(os.Stderr.Fd())
	if !term.IsTerminal(inputFD) || !term.IsTerminal(outputFD) {
		return gitpkg.WorktreeEntry{}, false, fmt.Errorf("interactive use requires a terminal; pass a branch with 'gwt use <branch>'")
	}
	state, err := term.MakeRaw(inputFD)
	if err != nil {
		return gitpkg.WorktreeEntry{}, false, fmt.Errorf("entering terminal raw mode: %w", err)
	}
	defer func() { _ = term.Restore(inputFD, state) }()

	_, _ = io.WriteString(os.Stderr, "\033[?25l")
	defer func() { _, _ = io.WriteString(os.Stderr, "\033[?25h") }()
	color := os.Getenv("NO_COLOR") == ""
	return runWorktreeSelector(list, os.Stdin, os.Stderr, color)
}
