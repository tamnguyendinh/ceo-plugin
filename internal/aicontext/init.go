package aicontext

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"ceo-plugin/internal/fileupdate"
)

var initContextNames = []string{"AGENTS.md", "CLAUDE.md"}

// InitResult retains completed work even when InitWorkspace returns an error.
type InitResult struct {
	Status        string
	WorkspaceRoot string
	Stage         string
	GitAction     string
	Files         []FileResult
	Tracked       map[string]string
	Warnings      []string
	Error         string
}

type initOperations struct {
	git    func() (initGit, error)
	update initUpdate
	sync   func(context.Context, string, string, func() error) (SyncResult, error)
}

func defaultInitOperations() initOperations {
	return initOperations{
		git:    func() (initGit, error) { return newInitGit(30 * time.Second) },
		update: fileupdate.Update,
		sync:   syncWorkspace,
	}
}

// InitWorkspace is explicit opt-in: ordinary document reads and MCP sync do not
// call it. Git/index/config are not modified except Git's own fresh init.
func InitWorkspace(ctx context.Context, cwd, block string) (InitResult, error) {
	return initWorkspace(ctx, cwd, block, defaultInitOperations())
}

func initWorkspace(ctx context.Context, cwd, block string, ops initOperations) (result InitResult, err error) {
	result = InitResult{Status: "failed", Stage: "resolve", GitAction: "not_run",
		Tracked: map[string]string{"AGENTS.md": "unknown", "CLAUDE.md": "unknown"}}
	for _, name := range []string{".gitignore", "AGENTS.md", "CLAUDE.md"} {
		result.Files = append(result.Files, FileResult{Result: fileupdate.Result{Path: name, Status: "not_run"}})
	}
	mutated := false
	defer func() {
		if err != nil {
			result.Error = err.Error()
			if mutated {
				result.Status = "partial"
			} else {
				result.Status = "failed"
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return
	}
	g, gitErr := ops.git()
	if gitErr != nil {
		result.Stage, result.GitAction, err = "git", "failed", gitErr
		return
	}
	root, _, resolveErr := resolveInitRoot(ctx, cwd, g)
	result.WorkspaceRoot = root
	if root != "" {
		for i := range result.Files {
			result.Files[i].Path = filepath.Join(root, result.Files[i].Path)
		}
	}
	if resolveErr != nil {
		err = resolveErr
		return
	}
	guard, guardErr := initRootGuard(root)
	if guardErr != nil {
		err = guardErr
		return
	}

	// One workspace lock coordinates init callers; existing sync retains its
	// per-file locking. This identity does not create a lock file in the repo.
	err = fileupdate.WithLock(ctx, filepath.Join(root, ".ceo-init-workspace"), func() error {
		if e := guard(); e != nil {
			return e
		}
		currentRoot, existing, e := resolveInitRoot(ctx, cwd, g)
		if e != nil {
			return e
		}
		if !sameInitPath(currentRoot, root) {
			return fmt.Errorf("workspace_changed during init")
		}
		if existing {
			result.GitAction = "existing"
		}

		result.Stage = "preflight"
		for i := range result.Files {
			path := result.Files[i].Path
			_, e := ops.update(ctx, path, func(raw []byte, exists bool) ([]byte, error) {
				if e := guard(); e != nil {
					return nil, e
				}
				if i == 0 {
					if e := validateInitIgnore(raw); e != nil {
						return nil, e
					}
				} else if _, e := UpsertManagedBlock(raw, exists, []byte(block)); e != nil {
					return nil, e
				}
				return raw, nil
			})
			if e != nil {
				result.Files[i].Status, result.Files[i].Error = "failed", e.Error()
				return fmt.Errorf("preflight %s: %w", path, e)
			}
		}

		result.Stage = "git"
		if !existing {
			if e := guard(); e != nil {
				return e
			}
			mutated = true // A failed Git invocation can leave partial metadata.
			if _, e := g.run(ctx, root, nil, "init"); e != nil {
				result.GitAction = "failed"
				return fmt.Errorf("git init (metadata may remain): %w", e)
			}
			result.GitAction = "initialized"
		}
		if e := g.verifyRoot(ctx, root); e != nil {
			result.GitAction = "failed"
			return e
		}

		result.Stage = "tracked"
		for _, name := range initContextNames {
			tracked, e := g.tracked(ctx, root, name)
			if e != nil {
				return e
			}
			result.Tracked[name] = "untracked"
			if tracked {
				result.Tracked[name] = "tracked"
				result.Warnings = append(result.Warnings, name+" is already tracked; .gitignore will not hide its changes. The index is unchanged.")
			}
		}

		result.Stage = "gitignore"
		if e := guard(); e != nil {
			return e
		}
		fr, e := ensureInitIgnore(ctx, root, g, guard, ops.update)
		result.Files[0] = fr
		mutated = mutated || initFileChanged(fr)
		if e != nil {
			return e
		}
		if e := verifyInitIgnore(ctx, root, g); e != nil {
			return e
		}

		result.Stage = "context"
		if e := guard(); e != nil {
			return e
		}
		synced, syncErr := ops.sync(ctx, root, block, guard)
		for _, fr := range synced.Files {
			mutated = mutated || initFileChanged(fr)
			for i := 1; i < len(result.Files); i++ {
				if sameInitPath(fr.Path, result.Files[i].Path) {
					result.Files[i] = fr
				}
			}
		}
		if syncErr != nil {
			return fmt.Errorf("context sync: %w", syncErr)
		}
		if synced.Status != "updated" && synced.Status != "unchanged" {
			return fmt.Errorf("context sync %s: %s", synced.Status, synced.Reason)
		}
		for _, fr := range result.Files[1:] {
			if fr.Status != "created" && fr.Status != "updated" && fr.Status != "unchanged" {
				return fmt.Errorf("context sync incomplete: %s (%s)", fr.Path, fr.Status)
			}
		}

		result.Stage = "verify"
		if e := guard(); e != nil {
			return e
		}
		if e := g.verifyRoot(ctx, root); e != nil {
			return e
		}
		if e := verifyInitIgnore(ctx, root, g); e != nil {
			return e
		}
		for _, fr := range result.Files[1:] {
			_, e := ops.update(ctx, fr.Path, func(raw []byte, exists bool) ([]byte, error) {
				if e := guard(); e != nil {
					return nil, e
				}
				next, e := UpsertManagedBlock(raw, exists, []byte(block))
				if e != nil {
					return nil, e
				}
				if !exists || !bytes.Equal(next, raw) {
					return nil, fmt.Errorf("context_changed: %s", fr.Path)
				}
				return raw, nil
			})
			if e != nil {
				return e
			}
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		result.Status, result.Stage = "success", "complete"
		return nil
	})
	return
}

func initFileChanged(fr FileResult) bool {
	return fr.Status == "created" || fr.Status == "updated" || fr.Status == "recovery_required"
}

func verifyInitIgnore(ctx context.Context, root string, g initGit) error {
	for _, name := range initContextNames {
		ok, err := g.ignored(ctx, root, name)
		if err != nil {
			return fmt.Errorf("verify ignore %s: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("ignore_not_effective: %s (expected root .gitignore)", name)
		}
	}
	return nil
}
