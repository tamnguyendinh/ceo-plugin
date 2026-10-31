package fileupdate

import (
	"bufio"
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReplaceFailureKeepsRecoveryData(t *testing.T) {
	p := filepath.Join(t.TempDir(), "owner.md")
	original := []byte("owner private rules")
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	r, err := Update(context.Background(), p, func(_ []byte, _ bool) ([]byte, error) { return []byte("replacement"), nil })
	if err == nil || r.Status != "recovery_required" || len(r.Recovery) == 0 {
		t.Fatalf("replacement failure hidden: %+v %v", r, err)
	}
	actual, err := os.ReadFile(p)
	if err != nil || string(actual) != string(original) {
		t.Fatal("locked original was lost")
	}
	for _, recovery := range r.Recovery {
		if _, err := os.Stat(recovery); err != nil {
			t.Fatalf("missing reported recovery %s", recovery)
		}
	}
}

func TestUpdateProcess(t *testing.T) {
	p := os.Getenv("CEO_TEST_UPDATE_PATH")
	if p == "" {
		return
	}
	if os.Getenv("CEO_TEST_LOCK_HOLD") == "1" {
		canonical, err := canonicalPath(p)
		if err != nil {
			t.Fatal(err)
		}
		err = WithLock(context.Background(), canonical, func() error { fmt.Println("ready"); _, err := bufio.NewReader(os.Stdin).ReadByte(); return err })
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	_, err := Update(context.Background(), p, func(raw []byte, exists bool) ([]byte, error) {
		if !exists || !strings.HasPrefix(string(raw), "owner\n") {
			return nil, fmt.Errorf("owner bytes missing")
		}
		n, err := strconv.Atoi(strings.TrimPrefix(string(raw), "owner\n"))
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("owner\n%d", n+1)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutexCancellationAndAbandonedOwner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "context.md")
	if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateProcess$")
	cmd.Env = append(os.Environ(), "CEO_TEST_UPDATE_PATH="+p, "CEO_TEST_LOCK_HOLD=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatal(line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Update(ctx, p, func(b []byte, _ bool) ([]byte, error) { return b, nil }); err == nil {
		t.Fatal("ignored cancellation under contention")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if _, err := Update(context.Background(), p, func(b []byte, _ bool) ([]byte, error) { return append(b, '!'), nil }); err != nil {
		t.Fatalf("abandoned mutex did not recover: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "original!" {
		t.Fatal("original bytes lost")
	}
}

func TestUpdateConcurrentProcesses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(p, []byte("owner\n0"), 0600); err != nil {
		t.Fatal(err)
	}
	var commands []*exec.Cmd
	for i := 0; i < 24; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateProcess$")
		cmd.Env = append(os.Environ(), "CEO_TEST_UPDATE_PATH="+p)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "owner\n24" {
		t.Fatalf("lost update: %q %v", b, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("unexpected residuals: %v", entries)
	}
}

func TestUpdateNoOpAndTransformFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte("owner"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	r, err := Update(context.Background(), p, func(raw []byte, _ bool) ([]byte, error) { return raw, nil })
	after, _ := os.Stat(p)
	if err != nil || r.Status != "unchanged" || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("no-op: %+v %v", r, err)
	}
	_, err = Update(context.Background(), p, func(_ []byte, _ bool) ([]byte, error) { return nil, fmt.Errorf("invalid config") })
	b, _ := os.ReadFile(p)
	if err == nil || string(b) != "owner" {
		t.Fatal("failed transform modified target")
	}
}
