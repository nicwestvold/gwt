package disk

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

func TestSizeSymlinkNotFollowed(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "big")
	writeFile(t, target, 1<<20) // 1 MiB
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	linkOnly := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linkOnly, "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	res, err := Size(linkOnly)
	if err != nil {
		t.Fatal(err)
	}
	// A symlink's own size is tiny; must not include the 1 MiB target.
	if res.Bytes >= 1<<20 {
		t.Errorf("Bytes = %d, symlink target was followed", res.Bytes)
	}
}

func TestSizeSkipsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not apply")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readable"), 4096)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	res, err := Size(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped == 0 {
		t.Error("Skipped = 0, want >= 1 for the unreadable dir")
	}
}

func TestSizeDeterministic(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(root, "f"+strconv.Itoa(i)), 100)
	}
	first, _ := Size(root)
	for i := 0; i < 5; i++ {
		got, _ := Size(root)
		if got.Bytes != first.Bytes {
			t.Fatalf("non-deterministic: %d vs %d", got.Bytes, first.Bytes)
		}
	}
}

func TestSizeMatchesDu(t *testing.T) {
	du, err := exec.LookPath("du")
	if err != nil {
		t.Skip("du not available")
	}
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(root, "f"+strconv.Itoa(i)), 3000)
	}
	out, err := exec.Command(du, "-sk", root).Output()
	if err != nil {
		t.Fatalf("du failed: %v", err)
	}
	fields := strings.Fields(string(out))
	duKB, _ := strconv.ParseInt(fields[0], 10, 64)

	res, _ := Size(root)
	ourKB := res.Bytes / 1024
	diff := ourKB - duKB
	if diff < 0 {
		diff = -diff
	}
	// Allow a couple KB of rounding slack.
	if diff > 4 {
		t.Errorf("our %d KiB vs du %d KiB (diff %d)", ourKB, duKB, diff)
	}
}

func TestFormatIEC(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{5153960550, "4.8 GiB"},
	}
	for _, c := range cases {
		if got := FormatIEC(c.in); got != c.want {
			t.Errorf("FormatIEC(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatApproxAndResult(t *testing.T) {
	if got := FormatApprox(1024, false); got != "1.0 KiB" {
		t.Errorf("FormatApprox exact = %q", got)
	}
	if got := FormatApprox(1024, true); got != "~1.0 KiB" {
		t.Errorf("FormatApprox approx = %q", got)
	}
	if got := Format(Result{Bytes: 1024, Skipped: 0}); got != "1.0 KiB" {
		t.Errorf("Format clean = %q", got)
	}
	if got := Format(Result{Bytes: 1024, Skipped: 3}); got != "~1.0 KiB" {
		t.Errorf("Format skipped = %q", got)
	}
}
