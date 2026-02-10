package hook

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

type HookData struct {
	BasePath       string
	CopyFiles      []string
	VersionManager string
	PackageManager string
}

func (d HookData) BuildCommand() string {
	switch d.PackageManager {
	case "yarn":
		return "yarn build"
	case "":
		return ""
	default:
		return d.PackageManager + " run build"
	}
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func Generate(data HookData) (string, error) {
	funcMap := template.FuncMap{"shellEscape": shellEscape}
	tmpl, err := template.New("post-checkout.sh.tmpl").Funcs(funcMap).ParseFS(templates, "templates/post-checkout.sh.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse hook template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render hook template: %w", err)
	}
	return buf.String(), nil
}

func Install(hooksDir string, data HookData, force bool) error {
	hookPath := filepath.Join(hooksDir, "post-checkout")

	if !force {
		if _, err := os.Stat(hookPath); err == nil {
			return fmt.Errorf("hook already exists at %s; use --force to overwrite", hookPath)
		}
	}

	content, err := Generate(data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	if err := os.WriteFile(hookPath, []byte(content), 0o755); err != nil {
		return fmt.Errorf("failed to write hook: %w", err)
	}

	return nil
}
