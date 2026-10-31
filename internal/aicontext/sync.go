package aicontext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"ceo-plugin/internal/fileupdate"
)

type FileResult struct {
	fileupdate.Result
	Error string `json:"error,omitempty"`
}
type SyncResult struct {
	Status string       `json:"status"`
	Reason string       `json:"reason,omitempty"`
	Files  []FileResult `json:"files,omitempty"`
}

func (r SyncResult) Warn(w io.Writer) {
	if w != nil && (r.Status == "failed" || r.Status == "partial" || r.Status == "skipped") {
		fmt.Fprintf(w, "context_sync=%s: context has not been fully updated: %s\n", r.Status, r.Reason)
	}
}

func SyncWorkspace(ctx context.Context, root, block string) (SyncResult, error) {
	return syncWorkspace(ctx, root, block, nil)
}

func syncWorkspace(ctx context.Context, root, block string, beforeWrite func() error) (SyncResult, error) {
	r := SyncResult{Status: "failed"}
	ws := validateRoot(root)
	if root == "" || ws.Root == "" {
		r.Status = "skipped"
		r.Reason = "workspace_unresolved: " + ws.Reason
		return r, nil
	}
	paths := []string{filepath.Join(ws.Root, "AGENTS.md"), filepath.Join(ws.Root, "CLAUDE.md")}
	// Preflight all targets before writing any; repeat validation under lock.
	for _, p := range paths {
		_, err := fileupdate.Update(ctx, p, func(b []byte, exists bool) ([]byte, error) {
			if _, err := UpsertManagedBlock(b, exists, []byte(block)); err != nil {
				return nil, err
			}
			return b, nil // Read/validate under the same mutex, without writing.
		})
		if err != nil {
			r.Reason = fmt.Sprintf("preflight %s: %v", p, err)
			return r, errors.New(r.Reason)
		}
	}
	changed := false
	for _, p := range paths {
		v, err := fileupdate.Update(ctx, p, func(b []byte, exists bool) ([]byte, error) {
			if beforeWrite != nil {
				if err := beforeWrite(); err != nil {
					return nil, err
				}
			}
			return UpsertManagedBlock(b, exists, []byte(block))
		})
		fr := FileResult{Result: v}
		if err != nil {
			fr.Error = err.Error()
		}
		r.Files = append(r.Files, fr)
		if err != nil {
			if changed {
				r.Status = "partial"
			}
			r.Reason = err.Error()
			return r, err
		}
		changed = changed || v.Status == "created" || v.Status == "updated"
	}
	r.Status = "unchanged"
	if changed {
		r.Status = "updated"
	}
	return r, nil
}

func upsertSection(path, block string) error {
	_, err := fileupdate.Update(context.Background(), path, func(b []byte, exists bool) ([]byte, error) { return UpsertManagedBlock(b, exists, []byte(block)) })
	return err
}

func SyncAIContext(block string) error {
	root := findWorkspaceRoot()
	if root == "" {
		return fmt.Errorf("workspace_unresolved")
	}
	_, err := SyncWorkspace(context.Background(), root, block)
	return err
}
