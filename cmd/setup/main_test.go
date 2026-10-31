package main

import (
	"bytes"
	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/patcher"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestSafeWriteFile_NewFile(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "subdir", "test_new.exe")
	data := []byte("binary payload content v1")

	err := safeWriteFile(targetPath, data, 0755)
	if err != nil {
		t.Fatalf("safeWriteFile failed on new file: %v", err)
	}

	readBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read back created file: %v", err)
	}
	if !bytes.Equal(readBytes, data) {
		t.Fatalf("expected data %q, got %q", string(data), string(readBytes))
	}
}

func TestSafeWriteFile_Overwrite(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "test_overwrite.exe")
	dataV1 := []byte("binary payload v1")
	dataV2 := []byte("binary payload v2 updated")

	if err := safeWriteFile(targetPath, dataV1, 0755); err != nil {
		t.Fatalf("safeWriteFile v1 failed: %v", err)
	}

	if err := safeWriteFile(targetPath, dataV2, 0755); err != nil {
		t.Fatalf("safeWriteFile v2 overwrite failed: %v", err)
	}

	readBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read overwritten file: %v", err)
	}
	if !bytes.Equal(readBytes, dataV2) {
		t.Fatalf("expected data %q, got %q", string(dataV2), string(readBytes))
	}
}

func TestSafeWriteFile_LockedImagePreserved(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "running_app.exe")
	initialData := []byte("initial running executable payload")
	updatedData := []byte("new upgraded executable payload")

	if err := safeWriteFile(targetPath, initialData, 0755); err != nil {
		t.Fatalf("setup initial file failed: %v", err)
	}

	if runtime.GOOS == "windows" {
		// On Windows, simulate a running executable held open by the OS loader:
		// Opened with FILE_SHARE_READ | FILE_SHARE_DELETE (no FILE_SHARE_WRITE).
		pathPtr, err := syscall.UTF16PtrFromString(targetPath)
		if err != nil {
			t.Fatalf("UTF16PtrFromString failed: %v", err)
		}

		handle, err := syscall.CreateFile(
			pathPtr,
			syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE,
			nil,
			syscall.OPEN_EXISTING,
			syscall.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err != nil {
			t.Fatalf("CreateFile to lock failed: %v", err)
		}
		defer func() {
			_ = syscall.CloseHandle(handle)
		}()

		// Direct os.WriteFile must fail because the file is open without write-sharing
		directErr := os.WriteFile(targetPath, updatedData, 0755)
		if directErr == nil {
			t.Log("Note: direct write did not fail in this environment, continuing safeWriteFile check")
		}

		// A locked image must remain unchanged; no .old fallback is permitted.
		err = safeWriteFile(targetPath, updatedData, 0755)
		if err == nil {
			t.Fatal("overwrote a locked image")
		}
		actual, readErr := os.ReadFile(targetPath)
		if readErr != nil || !bytes.Equal(actual, initialData) {
			t.Fatal("locked original changed")
		}
		if _, err := os.Stat(targetPath + ".old"); !os.IsNotExist(err) {
			t.Fatal("created .old fallback")
		}
	} else {
		// Non-Windows environment fallback validation
		err := safeWriteFile(targetPath, updatedData, 0755)
		if err != nil {
			t.Fatalf("safeWriteFile failed: %v", err)
		}
		readNew, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("failed to read updated file: %v", err)
		}
		if !bytes.Equal(readNew, updatedData) {
			t.Fatalf("expected updated data %q, got %q", string(updatedData), string(readNew))
		}
	}
}

func TestEmbeddedBinaries(t *testing.T) {
	// Verify embedded binaries are present and have non-zero size
	engineBytes, err := readPayload("ceo-plugin.exe")
	if err != nil {
		t.Fatalf("failed to read embedded dist_bin/ceo-plugin.exe: %v", err)
	}
	if len(engineBytes) == 0 {
		t.Fatalf("embedded dist_bin/ceo-plugin.exe is empty")
	}

	uninstallBytes, err := readPayload("ceo-plugin-uninstall.exe")
	if err != nil {
		t.Fatalf("failed to read embedded dist_bin/ceo-plugin-uninstall.exe: %v", err)
	}
	if len(uninstallBytes) == 0 {
		t.Fatalf("embedded dist_bin/ceo-plugin-uninstall.exe is empty")
	}
	for name, payload := range map[string][]byte{"ceo-plugin.exe": engineBytes, "ceo-plugin-uninstall.exe": uninstallBytes} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "dist", "bin", name))
		if err != nil {
			t.Fatal("run build.ps1 first: ", err)
		}
		if !bytes.Equal(raw, payload) {
			t.Fatalf("compressed payload differs from release binary: %s", name)
		}
	}
}
func TestSetupPreflightAndSystemFailure(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "install")
	config := filepath.Join(base, "fixture.json")
	if err := os.WriteFile(config, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	targets := []patcher.Target{{IDE: "fixture", Path: config}}
	var out bytes.Buffer
	called := false
	ops := setupOps{addPath: func(string) error { called = true; return nil }, register: func(string, string) error { called = true; return nil }}
	if err := runSetup(root, targets, []byte("engine"), []byte("uninstall"), &out, ops); err == nil {
		t.Fatal("accepted broken config")
	}
	if called {
		t.Fatal("mutated system before preflight")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("installed binaries before preflight")
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	ops.addPath = func(string) error { return fmt.Errorf("fixture denied") }
	if err := runSetup(root, targets, []byte("engine"), []byte("uninstall"), &out, ops); err == nil {
		t.Fatal("ignored PATH failure")
	}
	if strings.Contains(out.String(), "complete:") {
		t.Fatal("false complete")
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe")); err != nil {
		t.Fatal("no retry entrypoint")
	}
}

func TestSetupRepeat(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "install")
	targets := []patcher.Target{{IDE: "fixture", Path: filepath.Join(base, "config.json")}}
	ops := setupOps{addPath: func(string) error { return nil }, register: func(string, string) error { return nil }}
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		if err := runSetup(root, targets, []byte("engine"), []byte("uninstall"), &out, ops); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "complete:") {
			t.Fatal(out.String())
		}
		if i == 0 {
			if !strings.Contains(out.String(), "Antigravity: instructions installed") {
				t.Fatalf("expected initial install output, got: %s", out.String())
			}
		} else {
			if !strings.Contains(out.String(), "Antigravity: instructions unchanged") {
				t.Fatalf("expected unchanged output on repeat, got: %s", out.String())
			}
		}
	}

	expectedContent := aicontext.EmbeddedInstructionsContent()
	for _, sub := range []string{
		filepath.Join(".gemini", "antigravity", "mcp", "ceo-plugin", "instructions.md"),
		filepath.Join(".codex", "mcp", "ceo-plugin", "instructions.md"),
		filepath.Join(".cursor", "mcp", "ceo-plugin", "instructions.md"),
	} {
		p := filepath.Join(base, sub)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("expected instructions.md at %s: %v", p, err)
		}
		if !bytes.Equal(b, expectedContent) {
			t.Fatalf("content mismatch at %s", p)
		}
	}
}
