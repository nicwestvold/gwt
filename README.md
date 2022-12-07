# Setup

When using `git worktree`, you must update the git config so that `git fetch` works properly.

```
git config remote.origin.fetch "+refs/heads/*:refs/remotes/origin/*"
```
