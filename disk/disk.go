// Package disk measures on-disk usage of directory trees, walking
// concurrently to outperform single-threaded du.
package disk

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

// walkConcurrency bounds the number of directories read in parallel.
var walkConcurrency = runtime.NumCPU()

// Result is the outcome of a Size walk.
type Result struct {
	Bytes   int64 // on-disk size (allocated blocks) in bytes
	Skipped int   // entries that could not be stat'd (permission, vanished)
}

// Size returns the on-disk size of the tree rooted at root, walking
// concurrently. Best-effort: unreadable entries and files that vanish
// mid-walk are skipped and counted in Result.Skipped. Symlinks are counted
// as the link itself and never followed.
func Size(root string) (Result, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		return Result{}, err
	}

	var bytes, skipped int64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		bytes += st.Blocks * 512
	}

	sem := make(chan struct{}, walkConcurrency)
	var wg sync.WaitGroup

	var walk func(dir string)
	walk = func(dir string) {
		defer wg.Done()
		entries, err := os.ReadDir(dir)
		if err != nil {
			atomic.AddInt64(&skipped, 1)
			return
		}
		for _, e := range entries {
			info, err := e.Info() // lstat: does not follow symlinks
			if err != nil {
				atomic.AddInt64(&skipped, 1)
				continue
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				atomic.AddInt64(&bytes, st.Blocks*512)
			}
			// Directory (not a symlink to one — e.IsDir is type-based).
			if info.Mode()&fs.ModeSymlink == 0 && e.IsDir() {
				sub := filepath.Join(dir, e.Name())
				wg.Add(1)
				select {
				case sem <- struct{}{}:
					go func(p string) {
						defer func() { <-sem }()
						walk(p)
					}(sub)
				default:
					walk(sub) // pool saturated: recurse inline, no blocking
				}
			}
		}
	}

	wg.Add(1)
	walk(root)
	wg.Wait()
	return Result{Bytes: bytes, Skipped: int(skipped)}, nil
}
