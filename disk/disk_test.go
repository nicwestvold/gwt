package disk

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes n bytes and returns the path.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSizeSumsRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a"), 4096)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "b"), 8192)

	res, err := Size(root)
	if err != nil {
		t.Fatalf("Size returned error: %v", err)
	}
	// At least the two files' bytes; block rounding may add more, never less.
	if res.Bytes < 4096+8192 {
		t.Errorf("Bytes = %d, want >= %d", res.Bytes, 4096+8192)
	}
	if res.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", res.Skipped)
	}
}

func TestSizeEmptyDir(t *testing.T) {
	res, err := Size(t.TempDir())
	if err != nil {
		t.Fatalf("Size returned error: %v", err)
	}
	if res.Bytes < 0 {
		t.Errorf("Bytes = %d, want >= 0", res.Bytes)
	}
}

func TestSizeMissingRoot(t *testing.T) {
	if _, err := Size(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing root, got nil")
	}
}
