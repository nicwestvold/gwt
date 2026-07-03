# `gwt init` Version-Manager-Independent Hook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the post-checkout hook's version-manager setup independent of the package manager: `gwt init -v mise` emits `mise trust`, `gwt init -v asdf` emits the asdf sourcing block, and every generated hook is valid bash.

**Architecture:** All functional changes live in the Go `text/template` at `hook/templates/post-checkout.sh.tmpl`. The setup subshell's gate widens from `.PackageManager` to `or .VersionManager .PackageManager`; each version-manager branch always does its standalone setup and nests the package-manager steps behind an inner `{{if .PackageManager}}`. A no-op `:` fills the hook body when nothing is configured. No Go source changes — `main.go` already installs a hook when `-v` alone is passed, and `HookData` already carries both fields.

**Tech Stack:** Go 1.x, `text/template`, standard `testing` package, `bash -n` for syntax validation, `mise run check` (golangci-lint + tests + go mod tidy).

**Spec:** `docs/superpowers/specs/2026-07-03-init-version-manager-hook-design.md`

## Global Constraints

- NEVER run `git commit`, `git push`, or any command that writes to git history or the remote — at the end of a task, ask the user to commit, providing a suggested message.
- Generated hook output must match the spec's three expected outputs byte-for-byte (the golden tests below encode them, including the trailing newline).
- Every `TestGenerate`/golden case must also pass `bash -n` (skip only if `bash` is not on PATH).
- Follow existing code style in `hook/hook_test.go` (table-driven tests, `t.Run` subtests).

---

### Task 1: Version-manager-independent hook template

**Files:**
- Modify: `hook/templates/post-checkout.sh.tmpl` (whole file, 65 lines)
- Test: `hook/hook_test.go`

**Interfaces:**
- Consumes: `hook.Generate(data HookData) (string, error)` and `hook.HookData{BasePath, CopyFiles, VersionManager, PackageManager}` — both already exist in `hook/hook.go`, unchanged.
- Produces: no new Go API. Only the rendered template output changes.

- [ ] **Step 1: Write the failing tests**

In `hook/hook_test.go`, update the import block to add `bytes` and `os/exec`:

```go
import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)
```

Add a `bash -n` helper after `TestShellEscape`:

```go
// assertValidBash runs `bash -n` on the script to check it parses.
func assertValidBash(t *testing.T, script string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found on PATH")
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("bash -n failed: %v\n%s\n---\n%s", err, stderr.String(), script)
	}
}
```

Add a golden test after `TestGenerateEscapesCopyFiles` encoding the spec's expected outputs byte-for-byte:

```go
func TestGenerateGolden(t *testing.T) {
	tests := []struct {
		name string
		data HookData
		want string
	}{
		{
			name: "mise only",
			data: HookData{VersionManager: "mise"},
			want: `#!/bin/bash

if [[ "$1" == "0000000000000000000000000000000000000000" ]]; then

    (
        set +e  # allow failures without killing the subshell

        if command -v mise &>/dev/null; then
            mise trust
        else
            echo "warning: mise not found, skipping project setup" >&2
        fi
    )
fi
`,
		},
		{
			name: "mise with pnpm",
			data: HookData{VersionManager: "mise", PackageManager: "pnpm"},
			want: `#!/bin/bash

if [[ "$1" == "0000000000000000000000000000000000000000" ]]; then

    (
        set +e  # allow failures without killing the subshell

        if command -v mise &>/dev/null; then
            mise trust
            mise exec -- corepack enable

            if mise exec -- pnpm install; then
                mise exec -- pnpm run build
            else
                echo "pnpm install failed; skipping build"
            fi
        else
            echo "warning: mise not found, skipping project setup" >&2
        fi
    )
fi
`,
		},
		{
			name: "asdf only",
			data: HookData{VersionManager: "asdf"},
			want: `#!/bin/bash

if [[ "$1" == "0000000000000000000000000000000000000000" ]]; then

    (
        set +e  # allow failures without killing the subshell

        export ASDF_DIR="${ASDF_DIR:-$HOME/.asdf}"
        if [[ -f "$ASDF_DIR/asdf.sh" ]]; then
            . "$ASDF_DIR/asdf.sh"
        elif command -v brew &>/dev/null && [[ -f "$(brew --prefix asdf)/libexec/asdf.sh" ]]; then
            . "$(brew --prefix asdf)/libexec/asdf.sh"
        else
            echo "warning: asdf not found, skipping project setup" >&2
            exit 0
        fi
    )
fi
`,
		},
		{
			name: "empty data",
			data: HookData{},
			want: `#!/bin/bash

if [[ "$1" == "0000000000000000000000000000000000000000" ]]; then
    :
fi
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate(tt.data)
			if err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
			assertValidBash(t, got)
		})
	}
}
```

In the existing `TestGenerate` table, add two cases and extend two existing ones:

```go
		{
			name: "mise only no package manager",
			data: HookData{
				VersionManager: "mise",
			},
			contains: []string{"mise trust", "warning: mise not found"},
			excludes: []string{"install", "corepack", "cp"},
		},
		{
			name: "asdf only no package manager",
			data: HookData{
				VersionManager: "asdf",
			},
			contains: []string{"asdf.sh", "warning: asdf not found"},
			excludes: []string{"install", "corepack", "cp"},
		},
```

Change the `"with mise"` case's `contains` line to also require `mise trust`:

```go
			contains: []string{"mise trust", "mise exec --", "pnpm install", "pnpm run build"},
```

Change the `"empty data"` case's `contains` line to also require the no-op body:

```go
			contains: []string{"#!/bin/bash", "if [[", "    :"},
```

Finally, add the `bash -n` check inside the `TestGenerate` run loop, after the excludes loop (so every table case is syntax-checked):

```go
			assertValidBash(t, got)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./hook/ -run 'TestGenerate' -v`

Expected: FAIL —
- `TestGenerateGolden/mise_only`, `asdf_only`, `empty_data`: output mismatch (current template renders an empty `if` body) and `bash -n failed` (empty `then` body is a syntax error).
- `TestGenerateGolden/mise_with_pnpm`: output mismatch (no `mise trust` line).
- `TestGenerate/mise_only_no_package_manager`, `asdf_only_no_package_manager`: missing `mise trust` / `asdf.sh`.
- `TestGenerate/with_mise`: missing `mise trust`.
- `TestGenerate/empty_data`: missing `    :` and `bash -n failed`.

- [ ] **Step 3: Rewrite the template**

Replace the entire contents of `hook/templates/post-checkout.sh.tmpl` with:

```
#!/bin/bash

if [[ "$1" == "0000000000000000000000000000000000000000" ]]; then
{{- if .CopyFiles}}
    basePath='{{shellEscape .BasePath}}'
    paths=({{range $i, $f := .CopyFiles}}{{if $i}} {{end}}'{{shellEscape $f}}'{{end}})

    for path in "${paths[@]}"; do
        src="$basePath/$path"
        dst="$(pwd)/$path"
        if [[ -d "$src" ]]; then
            mkdir -p "$dst"
            cp -R "$src/." "$dst/"
        else
            mkdir -p "$(dirname "$dst")"
            cp "$src" "$dst"
        fi
    done
{{- end}}
{{- if or .VersionManager .PackageManager}}

    (
        set +e  # allow failures without killing the subshell
{{- if eq .VersionManager "asdf"}}

        export ASDF_DIR="${ASDF_DIR:-$HOME/.asdf}"
        if [[ -f "$ASDF_DIR/asdf.sh" ]]; then
            . "$ASDF_DIR/asdf.sh"
        elif command -v brew &>/dev/null && [[ -f "$(brew --prefix asdf)/libexec/asdf.sh" ]]; then
            . "$(brew --prefix asdf)/libexec/asdf.sh"
        else
            echo "warning: asdf not found, skipping project setup" >&2
            exit 0
        fi
{{- if .PackageManager}}
        corepack enable

        if {{.PackageManager}} install; then
            {{.BuildCommand}}
        else
            echo "{{.PackageManager}} install failed; skipping build"
        fi
{{- end}}
{{- else if eq .VersionManager "mise"}}

        if command -v mise &>/dev/null; then
            mise trust
{{- if .PackageManager}}
            mise exec -- corepack enable

            if mise exec -- {{.PackageManager}} install; then
                mise exec -- {{.BuildCommand}}
            else
                echo "{{.PackageManager}} install failed; skipping build"
            fi
{{- end}}
        else
            echo "warning: mise not found, skipping project setup" >&2
        fi
{{- else}}

        if {{.PackageManager}} install; then
            {{.BuildCommand}}
        else
            echo "{{.PackageManager}} install failed; skipping build"
        fi
{{- end}}
    )
{{- end}}
{{- if not (or .CopyFiles .VersionManager .PackageManager)}}
    :
{{- end}}
fi
```

Structural notes for the implementer:
- The copy-files block and the `{{else}}` (package-manager-only) branch are byte-identical to the current template — only the gates and the asdf/mise branches change.
- The outer chain is `{{- if eq .VersionManager "asdf"}} … {{- else if eq .VersionManager "mise"}} … {{- else}} … {{- end}}`; the package-manager steps sit in a *nested* `{{- if .PackageManager}} … {{- end}}` inside the asdf and mise branches.
- The final `{{- if not (or …)}}` emits `    :` only when the hook body would otherwise be empty, keeping the script valid bash.

- [ ] **Step 4: Run the hook package tests**

Run: `go test ./hook/ -v`

Expected: PASS — all `TestGenerate`, `TestGenerateGolden`, `TestInstall`, `TestBuildCommand`, `TestShellEscape`, `TestGenerateEscapesCopyFiles` subtests green. If a golden test fails on whitespace, fix the template's `{{-` trim markers rather than the golden string — the golden strings are the spec.

- [ ] **Step 5: Run the full check**

Run: `mise run check`

Expected: lint, full test suite, and `go mod tidy` all pass with no diff.

- [ ] **Step 6: Hand off for commit**

Do NOT commit. Ask the user to commit with this suggested message:

```
fix(init): run version-manager setup in hook even without a package manager
```

Files in the commit: `hook/templates/post-checkout.sh.tmpl`, `hook/hook_test.go` (plus the spec and plan docs if the user wants them recorded).
