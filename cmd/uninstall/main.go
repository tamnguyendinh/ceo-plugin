package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/installstate"
	"ceo-plugin/internal/patcher"
	"ceo-plugin/internal/pathutil"
	"ceo-plugin/internal/registry"
	"golang.org/x/sys/windows"
)

type uninstallOps struct {
	removePath         func(string) error
	unregister         func() error
	expectedHash       string
	expectedStamp      string
	expectedGeneration string
}

// runUninstall is synchronous: it never reports completion for a background deletion.
// The external bootstrap executes a temporary copy, so no mapped image must delete itself.
func runUninstall(root, executingPath string, targets []patcher.Target, out io.Writer, ops uninstallOps) error {
	return installstate.WithInstallation(context.Background(), root, func() (operationErr error) {
		bin := filepath.Join(root, "bin")
		self := filepath.Join(bin, "ceo-plugin-uninstall.exe")
		running, runErr := os.Stat(executingPath)
		installed, installErr := os.Stat(self)
		if runErr != nil {
			return runErr
		}
		if installErr != nil && !os.IsNotExist(installErr) {
			return installErr
		}
		if installErr == nil && os.SameFile(running, installed) {
			return fmt.Errorf("cannot remove the executing installation image; launch through the uninstall bootstrap")
		}
		if ops.expectedHash != "" {
			b, err := os.ReadFile(self)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(b)
			generation, err := installstate.ReadIdentity(root)
			if err != nil {
				return err
			}
			stamp := strconv.FormatInt(installed.ModTime().UnixNano()/100+621355968000000000, 10)
			if !strings.EqualFold(hex.EncodeToString(hash[:]), ops.expectedHash) || stamp != ops.expectedStamp || generation != ops.expectedGeneration {
				return fmt.Errorf("installation changed after uninstall launch; no files or settings changed, retry")
			}
		}
		if err := patcher.Preflight(targets, "", true); err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
		if err := installstate.NoRunningEngine(root); err != nil {
			return err
		}
		// Validate owned targets before any configuration changes.
		for _, name := range []string{"ceo-plugin.exe", "ceo-plugin-uninstall.exe"} {
			p := filepath.Join(bin, name)
			info, err := os.Lstat(p)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refusing non-regular owned binary: %s", p)
			}
		}
		results, err := patcher.Apply(targets, "", true)
		for _, r := range results {
			fmt.Fprintf(out, "%s: %s %s %s\n", r.IDE, r.Status, r.Path, r.Error)
		}
		if err != nil {
			return fmt.Errorf("partial: config cleanup failed; binaries and registration retained: %w", err)
		}
		// [Bước bổ sung]: Dọn dẹp file instructions.md tại tất cả IDE đích
		home := filepath.Dir(root)
		appData := os.Getenv("APPDATA")
		instructionTargets := aicontext.DefaultInstructionTargets(home, appData)
		if err := aicontext.RemoveMCPInstructions(instructionTargets, out); err != nil {
			return fmt.Errorf("partial: configs unpatched; instructions cleanup: %w", err)
		}
		if err := ops.removePath(bin); err != nil {
			return fmt.Errorf("partial: configs unpatched; PATH: %w", err)
		}
		// Keep a recovery copy in memory until the final registration step succeeds.
		recovery, err := os.ReadFile(self)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		identityPath := filepath.Join(root, installstate.IdentityFile)
		identityBytes, err := os.ReadFile(identityPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		removedSelf := false
		defer func() {
			if operationErr == nil || !removedSelf || recovery == nil {
				return
			}
			if err := os.MkdirAll(bin, 0700); err != nil {
				operationErr = errors.Join(operationErr, err)
				return
			}
			for p, data := range map[string][]byte{self: recovery, identityPath: identityBytes} {
				if data == nil {
					continue
				}
				f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
				if os.IsExist(err) {
					continue
				} // Never overwrite a concurrent external change.
				if err == nil {
					_, err = f.Write(data)
					if err == nil {
						err = f.Sync()
					}
					err = errors.Join(err, f.Close())
				}
				if err != nil {
					operationErr = errors.Join(operationErr, fmt.Errorf("restore retry file %s: %w", p, err))
				}
			}
		}()
		for _, name := range []string{"ceo-plugin.exe", "ceo-plugin-uninstall.exe"} {
			p := filepath.Join(bin, name)
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("partial: cannot remove %s; registration retained: %w", p, err)
			}
			if name == "ceo-plugin-uninstall.exe" {
				removedSelf = true
			}
		}
		if err := os.Remove(filepath.Join(root, installstate.IdentityFile)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("partial: remove installation identity: %w", err)
		}
		// Empty directories only. Files not in the exact owned set, including legacy
		// .old files without provenance, remain visible as residuals for review.
		for _, dir := range []string{bin, root} {
			entries, err := os.ReadDir(dir)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("partial: inspect residuals: %w", err)
			}
			if len(entries) == 0 {
				if err := os.Remove(dir); err != nil {
					return fmt.Errorf("partial: remove empty directory: %w", err)
				}
			} else {
				for _, entry := range entries {
					fmt.Fprintf(out, "preserved unowned residual: %s\n", filepath.Join(dir, entry.Name()))
				}
			}
		}
		if err := ops.unregister(); err != nil {
			return fmt.Errorf("partial: registration cleanup failed: %w", err)
		}
		fmt.Fprintln(out, "complete: owned binaries, MCP entries, PATH and registration removed; any unowned files listed above were preserved.")
		return nil
	})
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	executing, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	targets, err := patcher.GlobalTargets()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := filepath.Join(home, ".ceo-plugin")
	ops := uninstallOps{removePath: pathutil.RemoveUserPath, unregister: registry.UnregisterApp}
	if len(os.Args) > 1 {
		if len(os.Args) != 5 || os.Args[1] != "--internal-remove" {
			fmt.Fprintln(os.Stderr, "Unexpected uninstall arguments")
			os.Exit(1)
		}
		ops.expectedHash, ops.expectedStamp, ops.expectedGeneration = os.Args[2], os.Args[3], os.Args[4]
	} else {
		installed, statErr := os.Stat(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe"))
		current, currentErr := os.Stat(executing)
		if statErr == nil && currentErr == nil && os.SameFile(installed, current) {
			if err := launchBootstrap(executing); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stdout, "Uninstall continues in the result window. For automation use the registered QuietUninstallString and wait for its exit status.")
			os.Exit(2) // Handoff only; the bootstrap owns the visible terminal result.
		}
	}
	err = runUninstall(root, executing, targets, os.Stdout, ops)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uninstall not complete: %v\n", err)
		os.Exit(1)
	}
}

func launchBootstrap(source string) error {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return err
	}
	start := int64(uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime))
	powershell, err := installstate.PowerShellPath()
	if err != nil {
		return err
	}
	generation, err := installstate.ReadIdentity(filepath.Dir(filepath.Dir(source)))
	if err != nil {
		return err
	}
	cmd := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", installstate.EncodedScript(installstate.BootstrapScript(source, false, os.Getpid(), start, generation)))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
