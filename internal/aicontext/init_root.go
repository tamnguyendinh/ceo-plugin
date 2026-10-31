package aicontext

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func canonicalInitDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid workspace directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

func sameInitPath(a, b string) bool {
	return strings.EqualFold(cleanInitPath(a), cleanInitPath(b))
}

func cleanInitPath(path string) string {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		path = `\\` + path[len(`\\?\UNC\`):]
	} else {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	return filepath.Clean(path)
}

func insideInitPath(path, parent string) bool {
	path, parent = cleanInitPath(path), cleanInitPath(parent)
	return sameInitPath(path, parent) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(parent+string(filepath.Separator)))
}

func validateInitRoot(root string) error {
	if filepath.Dir(root) == root {
		return fmt.Errorf("unsafe_workspace_root: volume/share root")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot resolve HOME: %w", err)
	}
	canonicalHome, err := canonicalInitDir(home)
	if err != nil {
		return fmt.Errorf("cannot resolve HOME: %w", err)
	}
	if sameInitPath(root, canonicalHome) {
		return fmt.Errorf("unsafe_workspace_root: HOME")
	}
	denied := []string{filepath.Join(canonicalHome, ".ceo-plugin")}
	if windir := os.Getenv("WINDIR"); windir != "" {
		denied = append(denied, windir)
	}
	for _, path := range denied {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			abs = resolved
		}
		if insideInitPath(root, abs) {
			return fmt.Errorf("unsafe_workspace_root: %s", abs)
		}
	}
	return nil
}

func resolveInitRoot(ctx context.Context, cwd string, g initGit) (root string, existing bool, err error) {
	abs, err := canonicalInitDir(cwd)
	if err != nil {
		return "", false, err
	}
	root = abs
	marker := false
	for dir := abs; ; dir = filepath.Dir(dir) {
		info, statErr := os.Lstat(filepath.Join(dir, ".git"))
		if statErr == nil {
			if !info.IsDir() && !info.Mode().IsRegular() {
				return "", false, fmt.Errorf("invalid_repository: unsupported .git marker")
			}
			root, marker = dir, true
			break
		}
		if !os.IsNotExist(statErr) {
			return "", false, statErr
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if err = validateInitRoot(root); err != nil {
		return root, false, err
	}
	existing, err = g.repository(ctx, abs)
	if err != nil {
		return root, false, err
	}
	if marker {
		if !existing {
			return root, false, fmt.Errorf("invalid_repository: invalid .git marker at %s", root)
		}
		err = g.verifyRoot(ctx, root)
		return root, true, err
	}
	if existing {
		return root, false, fmt.Errorf("invalid_repository: repository without a local .git boundary")
	}
	return root, false, nil
}

// Keep directory identity as well as its canonical path, so replacement between
// discovery and a later write is observable. Each writer invokes this guard.
func initRootGuard(root string) (func() error, error) {
	dir, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	// File.Stat captures the Windows file ID immediately. os.Stat may defer
	// loading that ID until SameFile, after a wait during which root can change.
	info, err := dir.Stat()
	closeErr := dir.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return func() error {
		actual, err := canonicalInitDir(root)
		if err != nil {
			return err
		}
		now, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !sameInitPath(actual, root) || !os.SameFile(info, now) {
			return fmt.Errorf("workspace_changed: %s", root)
		}
		return validateInitRoot(actual)
	}, nil
}
