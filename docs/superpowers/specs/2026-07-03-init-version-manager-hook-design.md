# `gwt init`: version-manager setup independent of package manager

**Date:** 2026-07-03
**Status:** Approved

## Problem

The post-checkout hook template gates the entire setup subshell on
`.PackageManager`. As a result:

- `gwt init -v mise` produces a hook whose `if` body is empty — no `mise`
  setup at all, and `if …; then fi` with an empty body is invalid bash.
- The mise branch never runs `mise trust`, so new worktrees start with an
  untrusted mise config even when a package manager is configured.
- `gwt init -v asdf` likewise produces an empty (invalid) hook.

## Rule

If a version manager is given, the hook always verifies it is available and
performs its standalone setup. The package-manager steps (corepack enable,
install, build) are an optional layer inside the version-manager block.

## Changes

All functional changes are in `hook/templates/post-checkout.sh.tmpl`.
`main.go` already installs a hook when `-v` alone is passed (`wantHook`
checks `version-manager`), and `hook.HookData` already carries both fields —
no Go code changes.

### Template

1. Gate the setup subshell on `{{if or .VersionManager .PackageManager}}`
   instead of `{{if .PackageManager}}`.
2. **mise branch:** always emit the `command -v mise` check with the
   `warning: mise not found, skipping project setup` else-arm. Inside the
   success arm: `mise trust` first (always), then — only when
   `.PackageManager` is set — `mise exec -- corepack enable` and the
   install/build block.
3. **asdf branch:** always emit the asdf sourcing block (`ASDF_DIR` →
   brew fallback → warn + `exit 0`). Only when `.PackageManager` is set:
   `corepack enable` and the install/build block.
4. **No version manager:** unchanged — plain install/build (only reachable
   when a package manager is set).
5. **Nothing configured** (no copy files, no version manager, no package
   manager): emit a no-op `:` in the `if` body so the generated hook is
   always valid bash.

### Expected outputs

`gwt init -v mise`:

```bash
#!/bin/bash

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
```

`gwt init -v mise -p pnpm`:

```bash
#!/bin/bash

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
```

`gwt init -v asdf`:

```bash
#!/bin/bash

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
```

`asdf` + package manager and package-manager-only keep today's shape
(with `mise trust` having no analog for asdf). Copy-files behavior is
untouched.

### Tests (`hook/hook_test.go`)

Extend the `TestGenerate` table:

- `mise only` (new): contains `mise trust` and `warning: mise not found`;
  excludes `install` and `corepack`.
- `asdf only` (new): contains `asdf.sh` and `warning: asdf not found`;
  excludes `install` and `corepack`.
- `with mise` (existing): additionally assert `mise trust` is present and
  appears before `corepack enable`.
- `with asdf` (existing): unchanged.
- `empty data` (existing): additionally assert the body contains the
  no-op `:`.
- Every `TestGenerate` case additionally pipes its output through
  `bash -n` to assert the generated hook is syntactically valid
  (skipped if `bash` is not on PATH).

## Out of scope

- `corepack enable` is emitted for npm too, where it is unnecessary.
- Behavior of `gwt clone` init flags (delegates to the same hook code).
