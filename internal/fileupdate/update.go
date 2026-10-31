// Package fileupdate serializes cooperating writers and replaces files without truncation.
package fileupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Result struct {
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Recovery []string `json:"recovery,omitempty"`
}

func read(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("not a regular file: %s", path)
	}
	if err := rejectReparse(path); err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(path)
	return b, info, err
}

func Update(ctx context.Context, path string, transform func([]byte, bool) ([]byte, error)) (result Result, err error) {
	path, err = canonicalPath(path)
	result = Result{Path: path, Status: "failed"}
	if err != nil {
		return result, err
	}
	err = WithLock(ctx, path, func() error {
		before, info, err := read(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		next, err := transform(bytes.Clone(before), info != nil)
		if err != nil {
			return fmt.Errorf("transform %s: %w", path, err)
		}
		if bytes.Equal(before, next) && (info != nil || next == nil) {
			result.Status = "unchanged"
			return nil
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".ceo-update-*")
		if err != nil {
			return err
		}
		temp := f.Name()
		keep := false
		defer func() {
			if !keep {
				_ = os.Remove(temp)
			}
		}()
		if info != nil {
			if err := preservePermissions(path, temp); err != nil {
				_ = f.Close()
				return fmt.Errorf("stage permissions %s: %w", path, err)
			}
		}
		if _, err = f.Write(next); err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return fmt.Errorf("stage %s: %w", path, err)
		}
		current, now, err := read(path)
		if err != nil {
			return err
		}
		if (info == nil) != (now == nil) || (info != nil && !os.SameFile(info, now)) || !bytes.Equal(before, current) {
			return fmt.Errorf("conflict: %s changed during update", path)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		recovery, err := replace(temp, path, info != nil)
		if err != nil {
			keep = len(recovery) > 0
			result.Recovery = recovery
			if keep {
				result.Status = "recovery_required"
			}
			return fmt.Errorf("replace %s: %w", path, err)
		}
		result.Status = "updated"
		if info == nil {
			result.Status = "created"
		}
		return nil
	})
	return result, err
}
