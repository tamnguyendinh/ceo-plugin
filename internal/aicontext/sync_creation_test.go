package aicontext

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncWorkspaceCreatesMissingAndDeleted(t *testing.T) {
	root := t.TempDir()
	for _, phase := range []string{"missing", "deleted"} {
		if phase == "deleted" {
			for _, name := range initContextNames {
				if err := os.Remove(filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		r, err := SyncWorkspace(context.Background(), root, testBlock())
		if err != nil || r.Status != "updated" || len(r.Files) != 2 {
			t.Fatalf("%s: %+v %v", phase, r, err)
		}
		for _, f := range r.Files {
			if f.Status != "created" || f.Error != "" {
				t.Fatalf("%s: %+v", phase, f)
			}
			raw, err := os.ReadFile(f.Path)
			if err != nil || !bytes.Contains(raw, []byte(testBlock())) {
				t.Fatalf("%s missing managed block: %v", phase, err)
			}
		}
		var diagnostic bytes.Buffer
		r.Warn(&diagnostic)
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 2 || diagnostic.Len() != 0 {
			t.Fatalf("%s unexpected files or warning: %v %v %s", phase, entries, err, diagnostic.String())
		}
	}
}

func TestSyncWorkspacePreservesOwnerAndCreatesMissingSibling(t *testing.T) {
	for _, names := range [][]string{{"AGENTS.md"}, {"CLAUDE.md"}, {"AGENTS.md", "CLAUDE.md"}} {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			root := t.TempDir()
			owner := []byte("owner notes with spaces  \r\n")
			before := map[string]os.FileInfo{}
			for _, name := range names {
				if err := os.WriteFile(filepath.Join(root, name), owner, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, status := range []string{"updated", "unchanged"} {
				r, err := SyncWorkspace(context.Background(), root, testBlock())
				if err != nil || r.Status != status {
					t.Fatalf("want %s: %+v %v", status, r, err)
				}
				for _, name := range initContextNames {
					path := filepath.Join(root, name)
					raw, err := os.ReadFile(path)
					hadOwner := false
					for _, existing := range names {
						hadOwner = hadOwner || existing == name
					}
					if err != nil || (hadOwner && !bytes.HasPrefix(raw, owner)) || bytes.Count(raw, []byte(StartMarker)) != 1 {
						t.Fatalf("owner/block not preserved: %s %v", name, err)
					}
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if old := before[name]; old != nil && (!old.ModTime().Equal(info.ModTime()) || !os.SameFile(old, info)) {
						t.Fatal("no-op replaced existing file")
					}
					before[name] = info
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("missing sibling or unexpected artifact: %v %v", entries, err)
			}
		})
	}
}

func TestSyncWorkspaceCreationPreflightSafety(t *testing.T) {
	for _, kind := range []string{"malformed", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			owner := []byte("owner notes\n")
			agents, claude := filepath.Join(root, "AGENTS.md"), filepath.Join(root, "CLAUDE.md")
			if err := os.WriteFile(agents, owner, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "malformed":
				err = os.WriteFile(claude, []byte(StartMarker), 0600)
			case "directory":
				err = os.Mkdir(claude, 0700)
			case "symlink":
				err = os.Symlink(agents, claude)
				if err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := SyncWorkspace(context.Background(), root, testBlock())
			if err == nil || r.Status != "failed" {
				t.Fatalf("unsafe input accepted: %+v %v", r, err)
			}
			raw, err := os.ReadFile(agents)
			if err != nil || !bytes.Equal(raw, owner) {
				t.Fatal("preflight failure modified sibling")
			}
			var diagnostic bytes.Buffer
			r.Warn(&diagnostic)
			if diagnostic.Len() == 0 {
				t.Fatal("failure was silent")
			}
		})
	}
}

func TestSyncWorkspaceCreatesSiblingDeletedAfterPreflight(t *testing.T) {
	root := t.TempDir()
	for _, name := range initContextNames {
		if err := os.WriteFile(filepath.Join(root, name), []byte("owner\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Both files have been preflighted when the first transform runs. Remove
	// the sibling before its locked read; it must now be recreated.
	calls := 0
	r, err := syncWorkspace(context.Background(), root, testBlock(), func() error {
		calls++
		if calls == 1 {
			return os.Remove(filepath.Join(root, "CLAUDE.md"))
		}
		return nil
	})
	if err != nil || r.Status != "updated" || calls != 2 || len(r.Files) != 2 || r.Files[1].Status != "created" {
		t.Fatalf("deletion race: %+v calls=%d %v", r, calls, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil || !bytes.Contains(raw, []byte(testBlock())) {
		t.Fatalf("deleted sibling not restored: %v", err)
	}
}
