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
	"time"
)

type initGit struct {
	run func(context.Context, string, []byte, ...string) ([]byte, error)
}

type initGitError struct {
	code   int
	detail string
}

func (e *initGitError) Error() string { return fmt.Sprintf("git exit %d: %s", e.code, e.detail) }

func gitExit(err error, code int) bool {
	var e *initGitError
	return errors.As(err, &e) && e.code == code
}

func newInitGit(timeout time.Duration) (initGit, error) {
	exe, err := exec.LookPath("git")
	if err != nil {
		return initGit{}, fmt.Errorf("git_not_found: %w", err)
	}
	env := initGitEnv(os.Environ())
	return initGit{run: func(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		cmd.Stdin = bytes.NewReader(input)
		cmd.WaitDelay = time.Second
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			return stdout.Bytes(), fmt.Errorf("git cancelled/timeout: %w", ctx.Err())
		}
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				detail := strings.TrimSpace(stderr.String())
				if len(detail) > 8192 {
					detail = detail[:8192] + " (truncated)"
				}
				return stdout.Bytes(), &initGitError{code: ee.ExitCode(), detail: detail}
			}
			return stdout.Bytes(), fmt.Errorf("git execution: %w", err)
		}
		return stdout.Bytes(), nil
	}}, nil
}

// Remove repository redirection and command-scoped configuration. Keep normal
// user/system config, including ignore and case settings. Queries never refresh
// the index, invoke a pager, or prompt.
func initGitEnv(env []string) []string {
	blocked := map[string]bool{}
	for _, key := range strings.Fields("GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT GIT_OBJECT_DIRECTORY GIT_DIR GIT_WORK_TREE GIT_IMPLICIT_WORK_TREE GIT_GRAFT_FILE GIT_INDEX_FILE GIT_NO_REPLACE_OBJECTS GIT_REPLACE_REF_BASE GIT_PREFIX GIT_SHALLOW_FILE GIT_COMMON_DIR GIT_CEILING_DIRECTORIES GIT_DISCOVERY_ACROSS_FILESYSTEM GIT_NAMESPACE GIT_QUARANTINE_PATH GIT_OPTIONAL_LOCKS GIT_TERMINAL_PROMPT GIT_PAGER LC_ALL LANGUAGE") {
		blocked[key] = true
	}
	out := make([]string, 0, len(env)+5)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if !blocked[key] && !strings.HasPrefix(key, "GIT_CONFIG_KEY_") && !strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			out = append(out, entry)
		}
	}
	return append(out, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C", "LANGUAGE=C")
}

func (g initGit) repository(ctx context.Context, dir string) (bool, error) {
	b, err := g.run(ctx, dir, nil, "rev-parse", "--is-bare-repository", "--is-inside-git-dir")
	if err != nil {
		var e *initGitError
		if errors.As(err, &e) && e.code == 128 && strings.HasPrefix(e.detail, "fatal: not a git repository") {
			return false, nil
		}
		return false, err
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 || fields[0] != "false" || fields[1] != "false" {
		return false, fmt.Errorf("unsupported_repository: bare repository or Git metadata directory")
	}
	return true, nil
}

func (g initGit) verifyRoot(ctx context.Context, root string) error {
	repo, err := g.repository(ctx, root)
	if err != nil {
		return err
	}
	if !repo {
		return fmt.Errorf("invalid_repository: %s", root)
	}
	b, err := g.run(ctx, root, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actual, err := canonicalInitDir(strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	if !sameInitPath(actual, root) {
		return fmt.Errorf("repository_root_mismatch: %s", actual)
	}
	return nil
}

func (g initGit) tracked(ctx context.Context, root, name string) (bool, error) {
	b, err := g.run(ctx, root, nil, "ls-files", "-z", "--", ":(literal)"+name)
	if err != nil {
		return false, err
	}
	return len(b) != 0, nil
}

// Verbose NUL records expose source and the winning pattern. Exit 0 alone also
// covers negation and cannot establish the invariant.
func (g initGit) ignored(ctx context.Context, root, name string) (bool, error) {
	b, err := g.run(ctx, root, []byte(name+"\x00"), "check-ignore", "--no-index", "-v", "-z", "--stdin")
	if gitExit(err, 1) && len(b) == 0 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fields := bytes.Split(b, []byte{0})
	if len(fields) != 5 || string(fields[3]) != name || len(fields[4]) != 0 || len(fields[2]) == 0 {
		return false, fmt.Errorf("invalid check-ignore response for %s", name)
	}
	source := string(fields[0])
	if !filepath.IsAbs(source) {
		source = filepath.Join(root, source)
	}
	return sameInitPath(source, filepath.Join(root, ".gitignore")) && fields[2][0] != '!', nil
}
