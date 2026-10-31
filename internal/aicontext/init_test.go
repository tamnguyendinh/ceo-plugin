package aicontext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ceo-plugin/internal/fileupdate"
)

func initTestGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	g, err := newInitGit(30 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.run(context.Background(), root, nil, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func initTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func initTestRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func initTestIsolate(t *testing.T) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	initTestWrite(t, global, "")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func initTestCanonicalDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func initTestSuccess(t *testing.T, cwd string) InitResult {
	t.Helper()
	r, err := InitWorkspace(context.Background(), cwd, testBlock())
	if err != nil || r.Status != "success" {
		t.Fatalf("init: %+v, %v", r, err)
	}
	if len(r.Files) != 3 {
		t.Fatalf("missing file results: %+v", r)
	}
	return r
}

func initTestAbsent(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s unexpectedly exists: %v", name, err)
		}
	}
}

func TestInitWorkspaceFreshNestedAndIdempotent(t *testing.T) {
	initTestIsolate(t)
	root := initTestCanonicalDir(t)
	r := initTestSuccess(t, root)
	if r.GitAction != "initialized" || !sameInitPath(r.WorkspaceRoot, root) {
		t.Fatal(r)
	}
	g, _ := newInitGit(30 * time.Second)
	var before [][]byte
	var times []time.Time
	for _, fr := range r.Files {
		before = append(before, initTestRead(t, fr.Path))
		info, _ := os.Stat(fr.Path)
		times = append(times, info.ModTime())
	}
	for _, name := range initContextNames {
		ok, err := g.ignored(context.Background(), root, name)
		if err != nil || !ok {
			t.Fatalf("ignore %s: %v %v", name, ok, err)
		}
		if strings.Count(string(initTestRead(t, filepath.Join(root, name))), StartMarker) != 1 {
			t.Fatal("context missing/duplicated")
		}
	}
	if len(initTestGit(t, root, "ls-files", "--", "AGENTS.md", "CLAUDE.md")) != 0 {
		t.Fatal("unexpected tracked files")
	}
	status := string(initTestGit(t, root, "status", "--short"))
	if !strings.Contains(status, ".gitignore") || strings.Contains(status, "AGENTS.md") || strings.Contains(status, "CLAUDE.md") {
		t.Fatal(status)
	}
	sub := filepath.Join(root, "deep", "nested")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	r = initTestSuccess(t, sub)
	if r.GitAction != "existing" || !sameInitPath(r.WorkspaceRoot, root) {
		t.Fatal(r)
	}
	for i, fr := range r.Files {
		info, _ := os.Stat(fr.Path)
		if fr.Status != "unchanged" || !bytes.Equal(before[i], initTestRead(t, fr.Path)) || !times[i].Equal(info.ModTime()) {
			t.Fatalf("no-op rewrote %s", fr.Path)
		}
	}
	initTestAbsent(t, sub, ".git", ".gitignore", "AGENTS.md", "CLAUDE.md")
}

func TestInitWorkspaceGitfiles(t *testing.T) {
	initTestIsolate(t)
	parent := t.TempDir()
	initTestGit(t, parent, "init")
	initTestGit(t, parent, "-c", "user.name=Init Test", "-c", "user.email=init@example.invalid", "commit", "--allow-empty", "-m", "seed")
	worktree := filepath.Join(t.TempDir(), "linked")
	initTestGit(t, parent, "worktree", "add", "-b", "init-fixture", worktree)
	initTestSuccess(t, worktree)
	initTestAbsent(t, parent, ".gitignore", "AGENTS.md", "CLAUDE.md")
	source := t.TempDir()
	initTestGit(t, source, "init")
	initTestGit(t, source, "-c", "user.name=Init Test", "-c", "user.email=init@example.invalid", "commit", "--allow-empty", "-m", "seed")
	initTestGit(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", "--", source, "module")
	sub := filepath.Join(parent, "module")
	initTestSuccess(t, sub)
	for _, root := range []string{worktree, sub} {
		info, err := os.Stat(filepath.Join(root, ".git"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("gitfile not preserved: %s %v", root, err)
		}
	}
	initTestAbsent(t, parent, ".gitignore", "AGENTS.md", "CLAUDE.md")
}

func TestInitWorkspaceUnsafeAndMetadataRoots(t *testing.T) {
	initTestIsolate(t)
	t.Run("home-and-parent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("USERPROFILE", home)
		project := filepath.Join(home, "project")
		if err := os.Mkdir(project, 0700); err != nil {
			t.Fatal(err)
		}
		r, err := InitWorkspace(context.Background(), home, testBlock())
		if err == nil || r.Status != "failed" || !strings.Contains(err.Error(), "unsafe_workspace_root") {
			t.Fatal(r, err)
		}
		initTestGit(t, home, "init")
		_, err = InitWorkspace(context.Background(), project, testBlock())
		if err == nil || !strings.Contains(err.Error(), "unsafe_workspace_root") {
			t.Fatal(err)
		}
		initTestAbsent(t, home, ".gitignore", "AGENTS.md", "CLAUDE.md")
		initTestAbsent(t, project, ".git")
		alias := filepath.Join(t.TempDir(), "junction")
		cmd := exec.Command("cmd", "/c", "mklink", "/J", alias, home)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("junction fixture: %v %s", e, b)
		}
		t.Cleanup(func() { _ = os.Remove(alias) })
		if _, e := InitWorkspace(context.Background(), alias, testBlock()); e == nil {
			t.Fatal("junction bypassed HOME")
		}
	})
	t.Run("system-subtrees", func(t *testing.T) {
		home, windir := t.TempDir(), t.TempDir()
		t.Setenv("USERPROFILE", home)
		t.Setenv("WINDIR", windir)
		for _, root := range []string{windir, filepath.Join(windir, "System32", "nested"), filepath.Join(home, ".ceo-plugin", "bin", "nested")} {
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := InitWorkspace(context.Background(), root, testBlock()); err == nil {
				t.Fatalf("unsafe accepted %s", root)
			}
			initTestAbsent(t, root, ".git", ".gitignore")
			canonical, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateInitRoot(`\\?\` + canonical); err == nil {
				t.Fatalf("extended path bypassed root policy: %s", canonical)
			}
		}
		safe := windir + "-project"
		if err := os.Mkdir(safe, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(safe) })
		initTestSuccess(t, safe)
	})
	t.Run("volume", func(t *testing.T) {
		volume := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
		if _, err := InitWorkspace(context.Background(), volume, testBlock()); err == nil {
			t.Fatal("volume accepted")
		}
	})
	t.Run("invalid-marker", func(t *testing.T) {
		for _, directory := range []bool{false, true} {
			root := t.TempDir()
			if directory {
				_ = os.Mkdir(filepath.Join(root, ".git"), 0700)
			} else {
				initTestWrite(t, filepath.Join(root, ".git"), "broken")
			}
			if _, err := InitWorkspace(context.Background(), root, testBlock()); err == nil {
				t.Fatal("invalid marker accepted")
			}
			initTestAbsent(t, root, ".gitignore", "AGENTS.md", "CLAUDE.md")
		}
	})
	t.Run("bare-and-metadata", func(t *testing.T) {
		root := t.TempDir()
		initTestGit(t, root, "init", "--bare")
		if _, err := InitWorkspace(context.Background(), root, testBlock()); err == nil {
			t.Fatal("bare accepted")
		}
		normal := t.TempDir()
		initTestGit(t, normal, "init")
		if _, err := InitWorkspace(context.Background(), filepath.Join(normal, ".git", "objects"), testBlock()); err == nil {
			t.Fatal("metadata accepted")
		}
		initTestAbsent(t, normal, ".gitignore", "AGENTS.md", "CLAUDE.md")
	})
}

func TestInitWorkspaceIgnoreSemanticsAndFormatting(t *testing.T) {
	initTestIsolate(t)
	for _, tc := range []struct {
		name, raw string
		unchanged bool
	}{
		{"complete", "AGENTS.md\nCLAUDE.md\n", true},
		{"negation", "AGENTS.md\nCLAUDE.md\n!AGENTS.md\n", false},
		{"one", "/AGENTS.md\n", false},
		{"wildcard", "*.md\n", true},
		{"case-sensitive", "agents.md\nclaude.md\n", false},
		{"crlf", "node_modules\r\n", false},
		{"mixed", "node_modules\r\nbuild\n", false},
		{"bom", "\ufeffnode_modules\r\n", false},
		{"bom-only", "\ufeff", false},
		{"no-newline", "node_modules", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			initTestGit(t, root, "init")
			initTestGit(t, root, "config", "core.ignorecase", "false")
			path := filepath.Join(root, ".gitignore")
			initTestWrite(t, path, tc.raw)
			r := initTestSuccess(t, root)
			after := initTestRead(t, path)
			if !bytes.HasPrefix(after, []byte(tc.raw)) {
				t.Fatal("changed existing bytes")
			}
			if tc.unchanged && (r.Files[0].Status != "unchanged" || string(after) != tc.raw) {
				t.Fatal(r, string(after))
			}
			if !tc.unchanged && !bytes.Contains(after, []byte("/")) {
				t.Fatal("rules not appended")
			}
			if strings.Contains(tc.raw, "\r\n") && bytes.Contains(bytes.ReplaceAll(after[len(tc.raw):], []byte("\r\n"), nil), []byte("\n")) {
				t.Fatal("new content lost CRLF")
			}
			second := initTestSuccess(t, root)
			if second.Files[0].Status != "unchanged" || !bytes.Equal(after, initTestRead(t, path)) {
				t.Fatal("duplicate rules")
			}
		})
	}
	t.Run("external-excludes", func(t *testing.T) {
		root := t.TempDir()
		initTestGit(t, root, "init")
		global := filepath.Join(t.TempDir(), "ignore")
		initTestWrite(t, global, "AGENTS.md\n")
		initTestGit(t, root, "config", "core.excludesfile", global)
		initTestWrite(t, filepath.Join(root, ".git", "info", "exclude"), "CLAUDE.md\n")
		initTestSuccess(t, root)
		b := string(initTestRead(t, filepath.Join(root, ".gitignore")))
		if !strings.Contains(b, "/AGENTS.md") || !strings.Contains(b, "/CLAUDE.md") {
			t.Fatal(b)
		}
	})
}

func TestInitWorkspaceTrackedAndPreservedContent(t *testing.T) {
	initTestIsolate(t)
	root := t.TempDir()
	initTestGit(t, root, "init")
	for _, name := range initContextNames {
		initTestWrite(t, filepath.Join(root, name), "# User content\r\n")
	}
	initTestGit(t, root, "add", "AGENTS.md", "CLAUDE.md")
	index := initTestRead(t, filepath.Join(root, ".git", "index"))
	for _, name := range initContextNames {
		initTestWrite(t, filepath.Join(root, name), "# User content\r\nUnstaged text\r\n")
	}
	r := initTestSuccess(t, root)
	if len(r.Warnings) != 2 || !bytes.Equal(index, initTestRead(t, filepath.Join(root, ".git", "index"))) {
		t.Fatal("tracked warnings/index", r)
	}
	for _, name := range initContextNames {
		if r.Tracked[name] != "tracked" || !bytes.HasPrefix(initTestRead(t, filepath.Join(root, name)), []byte("# User content\r\nUnstaged text\r\n")) {
			t.Fatal("user content lost", r)
		}
	}
}

func TestInitWorkspacePreflightFailures(t *testing.T) {
	initTestIsolate(t)
	for _, tc := range []struct {
		name, content string
		dir           bool
	}{
		{"AGENTS.md", StartMarker, false},
		{"CLAUDE.md", StartMarker + "\n" + StartMarker + "\n" + EndMarker, false},
		{".gitignore", "\xff\xfeA\x00", false},
		{"AGENTS.md", "", true},
		{".gitignore", "", true},
	} {
		t.Run(tc.name+fmt.Sprint(tc.dir), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.name)
			if tc.dir {
				_ = os.Mkdir(path, 0700)
			} else {
				initTestWrite(t, path, tc.content)
			}
			r, err := InitWorkspace(context.Background(), root, testBlock())
			if err == nil || r.Stage != "preflight" || r.Status != "failed" {
				t.Fatal(r, err)
			}
			initTestAbsent(t, root, ".git")
			if !tc.dir && string(initTestRead(t, path)) != tc.content {
				t.Fatal("preflight changed file")
			}
		})
	}
	t.Run("reparse", func(t *testing.T) {
		root := t.TempDir()
		target := t.TempDir()
		path := filepath.Join(root, "AGENTS.md")
		if b, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
		if _, err := InitWorkspace(context.Background(), root, testBlock()); err == nil {
			t.Fatal("reparse accepted")
		}
		initTestAbsent(t, root, ".git")
	})
}

func TestInitWorkspaceEnvironmentIsolation(t *testing.T) {
	initTestIsolate(t)
	outside := t.TempDir()
	initTestGit(t, outside, "init")
	root := initTestCanonicalDir(t)
	t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(outside, "other-index"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.worktree")
	t.Setenv("GIT_CONFIG_VALUE_0", outside)
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	r := initTestSuccess(t, root)
	if !sameInitPath(root, r.WorkspaceRoot) {
		t.Fatal(r)
	}
	initTestAbsent(t, outside, ".gitignore", "AGENTS.md", "CLAUDE.md", "other-index")
}

func initTestIntercept(ops *initOperations, intercept func(context.Context, string, []byte, []string, initGit) ([]byte, error)) {
	base := ops.git
	ops.git = func() (initGit, error) {
		g, err := base()
		if err != nil {
			return g, err
		}
		return initGit{run: func(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
			return intercept(ctx, dir, input, args, g)
		}}, nil
	}
}

func TestInitWorkspaceFailuresAndRetry(t *testing.T) {
	initTestIsolate(t)
	t.Run("missing-git", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("PATH", "")
		r, err := InitWorkspace(context.Background(), root, testBlock())
		if err == nil || r.GitAction != "failed" || !strings.Contains(err.Error(), "git_not_found") {
			t.Fatal(r, err)
		}
		initTestAbsent(t, root, ".git", ".gitignore", "AGENTS.md", "CLAUDE.md")
	})
	t.Run("cancelled", func(t *testing.T) {
		root := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := InitWorkspace(ctx, root, testBlock())
		if !errors.Is(err, context.Canceled) || r.Status != "failed" {
			t.Fatal(r, err)
		}
		initTestAbsent(t, root, ".git")
	})
	t.Run("git-deadline", func(t *testing.T) {
		g, err := newInitGit(time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		_, err = g.run(context.Background(), t.TempDir(), nil, "version")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"init-error", "query-error", "untrusted", "ignore-write", "ignore-verify", "final-ignore", "skipped", "incomplete", "partial", "recovery", "root-changed", "file-conflict", "context-drift"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			ops := defaultInitOperations()
			switch mode {
			case "init-error", "query-error", "untrusted":
				initTestIntercept(&ops, func(ctx context.Context, dir string, input []byte, args []string, g initGit) ([]byte, error) {
					if mode == "init-error" && args[0] == "init" {
						if _, e := g.run(ctx, dir, input, args...); e != nil {
							return nil, e
						}
						return nil, errors.New("injected init error after metadata")
					}
					if mode != "init-error" && args[0] == "rev-parse" {
						return nil, &initGitError{128, "fatal: injected " + mode}
					}
					return g.run(ctx, dir, input, args...)
				})
			case "ignore-write", "recovery":
				real := ops.update
				count := 0
				ops.update = func(ctx context.Context, path string, fn func([]byte, bool) ([]byte, error)) (fileupdate.Result, error) {
					if filepath.Base(path) == ".gitignore" {
						count++
						if count == 2 {
							r := fileupdate.Result{Path: path, Status: "failed"}
							if mode == "recovery" {
								r.Status = "recovery_required"
								r.Recovery = []string{path + ".recovery"}
							}
							return r, errors.New("injected write failure")
						}
					}
					return real(ctx, path, fn)
				}
			case "ignore-verify", "final-ignore":
				count := 0
				failAt := 3
				if mode == "final-ignore" {
					failAt = 5
				}
				initTestIntercept(&ops, func(ctx context.Context, dir string, input []byte, args []string, g initGit) ([]byte, error) {
					if args[0] == "check-ignore" {
						count++
						if count == failAt {
							return nil, &initGitError{1, ""}
						}
					}
					return g.run(ctx, dir, input, args...)
				})
			case "skipped":
				ops.sync = func(context.Context, string, string, func() error) (SyncResult, error) {
					return SyncResult{Status: "skipped", Reason: "injected"}, nil
				}
			case "incomplete":
				ops.sync = func(context.Context, string, string, func() error) (SyncResult, error) {
					return SyncResult{Status: "updated"}, nil
				}
			case "file-conflict":
				real := ops.update
				count := 0
				ops.update = func(ctx context.Context, path string, fn func([]byte, bool) ([]byte, error)) (fileupdate.Result, error) {
					if filepath.Base(path) == ".gitignore" {
						count++
					}
					if filepath.Base(path) == ".gitignore" && count == 2 {
						return real(ctx, path, func(raw []byte, exists bool) ([]byte, error) {
							next, e := fn(raw, exists)
							if e != nil {
								return nil, e
							}
							if e := os.WriteFile(path, []byte("# external writer\n"), 0600); e != nil {
								return nil, e
							}
							return next, nil
						})
					}
					return real(ctx, path, fn)
				}
			case "context-drift":
				ops.sync = func(ctx context.Context, root, block string, guard func() error) (SyncResult, error) {
					r, e := syncWorkspace(ctx, root, block, guard)
					if e == nil {
						e = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# External edit\n"), 0600)
					}
					return r, e
				}
			case "partial":
				ops.sync = func(ctx context.Context, root, block string, guard func() error) (SyncResult, error) {
					count := 0
					return syncWorkspace(ctx, root, block, func() error {
						count++
						if count == 2 {
							return errors.New("injected second write")
						}
						return guard()
					})
				}
			case "root-changed":
				count := 0
				initTestIntercept(&ops, func(ctx context.Context, dir string, input []byte, args []string, g initGit) ([]byte, error) {
					b, e := g.run(ctx, dir, input, args...)
					if args[0] == "ls-files" {
						count++
						if count == 2 {
							moved := root + "-moved"
							if e := os.Rename(root, moved); e != nil {
								return nil, e
							}
							t.Cleanup(func() { _ = os.RemoveAll(moved) })
							if e := os.Mkdir(root, 0700); e != nil {
								return nil, e
							}
						}
					}
					return b, e
				})
			}
			r, err := initWorkspace(context.Background(), root, testBlock(), ops)
			if err == nil || r.Status == "success" {
				t.Fatal(r, err)
			}
			if mode == "partial" {
				if r.Status != "partial" || r.Files[1].Status != "created" || r.Files[2].Status != "failed" {
					t.Fatal(r)
				}
				initTestAbsent(t, root, "CLAUDE.md")
			} else if mode != "final-ignore" && mode != "context-drift" {
				initTestAbsent(t, root, "AGENTS.md", "CLAUDE.md")
			}
			if mode == "file-conflict" && string(initTestRead(t, filepath.Join(root, ".gitignore"))) != "# external writer\n" {
				t.Fatal("overwrote external edit")
			}
			if mode == "context-drift" && string(initTestRead(t, filepath.Join(root, "CLAUDE.md"))) != "# External edit\n" {
				t.Fatal("rolled back external edit")
			}
			if mode == "recovery" && (r.Files[0].Status != "recovery_required" || len(r.Files[0].Recovery) != 1) {
				t.Fatal(r)
			}
			if mode == "root-changed" {
				initTestAbsent(t, root, ".gitignore")
			}
			initTestSuccess(t, root)
		})
	}
}

func TestInitWorkspaceConcurrentWriters(t *testing.T) {
	initTestIsolate(t)
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := InitWorkspace(context.Background(), root, testBlock()); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	b := string(initTestRead(t, filepath.Join(root, ".gitignore")))
	for _, name := range initContextNames {
		if strings.Count(b, "/"+name) != 1 || strings.Count(string(initTestRead(t, filepath.Join(root, name))), StartMarker) != 1 {
			t.Fatal("duplicated content")
		}
	}
}

func TestInitWorkspaceLockTimeout(t *testing.T) {
	initTestIsolate(t)
	root := initTestCanonicalDir(t)
	ready, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- fileupdate.WithLock(context.Background(), filepath.Join(root, ".ceo-init-workspace"), func() error { close(ready); <-release; return nil })
	}()
	<-ready
	_, err := InitWorkspace(context.Background(), root, testBlock())
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if err == nil || !strings.Contains(err.Error(), "lock timeout") {
		t.Fatal(err)
	}
	initTestAbsent(t, root, ".git", ".gitignore", "AGENTS.md", "CLAUDE.md")
}

func TestInitRootIdentityBeforeFirstWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := initRootGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := guard(); err == nil || !strings.Contains(err.Error(), "workspace_changed") {
		t.Fatalf("replacement escaped first guard: %v", err)
	}
}
