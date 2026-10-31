package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/fileupdate"
	"ceo-plugin/internal/installstate"
	"ceo-plugin/internal/patcher"
	"ceo-plugin/internal/pathutil"
	"ceo-plugin/internal/registry"
	"golang.org/x/sys/windows"
)

//go:embed dist_bin/ceo-plugin.exe.gz dist_bin/ceo-plugin-uninstall.exe.gz
var embeddedBinaries embed.FS

func readPayload(name string) ([]byte, error) {
	packed, err := embeddedBinaries.ReadFile("dist_bin/" + name + ".gz")
	if err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("embedded payload %s: %w", name, err)
	}
	return data, nil
}

func safeWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Do not rename mapped binaries aside and leave an obsolete engine serving assets.
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(ptr, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		defer windows.CloseHandle(h)
	} else if err != windows.ERROR_FILE_NOT_FOUND {
		return fmt.Errorf("binary unavailable %s: %w", path, err)
	}
	_, err = fileupdate.Update(context.Background(), path, func(_ []byte, _ bool) ([]byte, error) { return data, nil })
	if err != nil {
		return err
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, data) {
		return fmt.Errorf("binary verification failed: %s", path)
	}
	return nil
}

type setupOps struct {
	addPath  func(string) error
	register func(string, string) error
}

func runSetup(root string, targets []patcher.Target, engine, uninstaller []byte, out io.Writer, ops setupOps) error {
	if len(engine) == 0 || len(uninstaller) == 0 {
		return fmt.Errorf("empty embedded binary payload")
	}
	return installstate.WithInstallation(context.Background(), root, func() error {
		enginePath := filepath.Join(root, "bin", "ceo-plugin.exe")
		uninstallPath := filepath.Join(root, "bin", "ceo-plugin-uninstall.exe")
		if err := patcher.Preflight(targets, enginePath, false); err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
		if err := installstate.NoRunningEngine(root); err != nil {
			return err
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			return err
		}
		if err := installstate.NewIdentity(root); err != nil {
			return fmt.Errorf("installation identity: %w", err)
		}
		if err := safeWriteFile(uninstallPath, uninstaller, 0755); err != nil {
			return err
		}
		if err := safeWriteFile(enginePath, engine, 0755); err != nil {
			return fmt.Errorf("partial: uninstaller installed; engine: %w", err)
		}
		if err := ops.register(root, uninstallPath); err != nil {
			return fmt.Errorf("partial: binaries installed; registration: %w", err)
		}
		if err := ops.addPath(filepath.Dir(enginePath)); err != nil {
			return fmt.Errorf("partial: binaries registered; PATH: %w", err)
		}
		results, err := patcher.Apply(targets, enginePath, false)
		for _, r := range results {
			fmt.Fprintf(out, "%s: %s %s %s\n", r.IDE, r.Status, r.Path, r.Error)
		}
		if err != nil {
			return fmt.Errorf("partial: IDE configuration: %w", err)
		}
		// [Bước bổ sung]: Xuất file instructions.md ra tất cả IDE đích
		home := filepath.Dir(root)
		appData := os.Getenv("APPDATA")
		instructionTargets := aicontext.DefaultInstructionTargets(home, appData)
		if err := aicontext.ExportMCPInstructions(instructionTargets, out); err != nil {
			return fmt.Errorf("partial: IDE configuration installed; instructions export: %w", err)
		}
		fmt.Fprintln(out, "complete: binaries, PATH, registration and IDE configuration installed. Reconnect MCP to start the new engine; open a new terminal for PATH changes.")
		return nil
	})
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	targets, err := patcher.GlobalTargets()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	engine, err := readPayload("ceo-plugin.exe")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	uninstaller, err := readPayload("ceo-plugin-uninstall.exe")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	err = runSetup(filepath.Join(home, ".ceo-plugin"), targets, engine, uninstaller, os.Stdout, setupOps{pathutil.AddUserPath, registry.RegisterApp})
	if err != nil {
		fmt.Fprintf(os.Stderr, "installation not complete: %v\n", err)
		os.Exit(1)
	}
}
