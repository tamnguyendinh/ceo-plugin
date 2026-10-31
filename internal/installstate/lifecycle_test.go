package installstate

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRunningEngineGuard(t *testing.T) {
	engine, err := filepath.Abs("../../dist/bin/ceo-plugin.exe")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(engine)
	if err != nil {
		t.Fatal("run build.ps1 first: ", err)
	}
	root := filepath.Join(t.TempDir(), ".ceo-plugin")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(bin, "ceo-plugin.exe")
	if err := os.WriteFile(copyPath, data, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(copyPath, "mcp")
	cmd.Dir = t.TempDir()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	if _, err := io.WriteString(stdin, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 39)
	if _, err := stdout.Read(response); err != nil {
		t.Fatal(err)
	}
	if err := NoRunningEngine(root); err == nil {
		t.Fatal("did not detect exact running image")
	}
	if err := NoRunningEngine(t.TempDir()); err != nil {
		t.Fatalf("blocked unrelated installation: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := NoRunningEngine(root); err != nil {
		t.Fatalf("stopped process still blocks: %v", err)
	}
}

func TestIdentityAndBootstrapEncoding(t *testing.T) {
	root := t.TempDir()
	id, err := ReadIdentity(root)
	if err != nil || id != "legacy" {
		t.Fatal(id, err)
	}
	if err := NewIdentity(root); err != nil {
		t.Fatal(err)
	}
	first, _ := ReadIdentity(root)
	if err := NewIdentity(root); err != nil {
		t.Fatal(err)
	}
	second, _ := ReadIdentity(root)
	if first == second {
		t.Fatal("reinstall reused identity")
	}
	script := BootstrapScript(`C:\User ' $(unsafe)\ceo-plugin-uninstall.exe`, true, 0, 0, first)
	encoded := EncodedScript(script)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	if string(utf16.Decode(units)) != script || strings.Contains(script, "$(unsafe)") {
		t.Fatal("path escaped into script syntax")
	}
	command, err := UninstallCommand(`C:\User Space\ceo-plugin-uninstall.exe`, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(command) > 30000 {
		t.Fatal("uninstall command exceeds CreateProcess budget")
	}
	if err := WithInstallation(context.Background(), root, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
