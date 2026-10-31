// Package installstate owns installation serialization and running-image guards.
package installstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"ceo-plugin/internal/fileupdate"
	"golang.org/x/sys/windows"
)

func lockIdentity(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// The home directory exists even before installation; resolve its aliases.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return "installation:" + strings.ToLower(filepath.Join(parent, filepath.Base(abs))), nil
}

func WithInstallation(ctx context.Context, root string, fn func() error) error {
	identity, err := lockIdentity(root)
	if err != nil {
		return err
	}
	return fileupdate.WithLock(ctx, identity, fn)
}

// NoRunningEngine checks exact image locations, never kills processes by name.
func NoRunningEngine(root string) error {
	paths := []string{filepath.Join(root, "bin", "ceo-plugin.exe"), filepath.Join(root, "bin", "ceo-plugin.exe.old")}
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(h, &entry)
	for err == nil {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, "ceo-plugin.exe") || strings.EqualFold(name, "ceo-plugin.exe.old") {
			p, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, entry.ProcessID)
			if e != nil {
				if e == windows.ERROR_INVALID_PARAMETER {
					err = windows.Process32Next(h, &entry)
					continue
				} // PID exited after the snapshot.
				return fmt.Errorf("cannot identify running engine PID %d: %w", entry.ProcessID, e)
			}
			buf := make([]uint16, 32768)
			n := uint32(len(buf))
			e = windows.QueryFullProcessImageName(p, 0, &buf[0], &n)
			if e != nil {
				// During image teardown QueryFullProcessImageName can fail before
				// the process handle is signaled. Bound the grace period; a live
				// unidentifiable process still fails closed.
				state, waitErr := windows.WaitForSingleObject(p, 250)
				windows.CloseHandle(p)
				if waitErr == nil && state == windows.WAIT_OBJECT_0 {
					err = windows.Process32Next(h, &entry)
					continue
				}
				return fmt.Errorf("query engine PID %d: %w", entry.ProcessID, e)
			}
			windows.CloseHandle(p)
			image := windows.UTF16ToString(buf[:n])
			for _, path := range paths {
				a, ae := os.Stat(path)
				b, be := os.Stat(image)
				if strings.EqualFold(filepath.Clean(path), filepath.Clean(image)) || (ae == nil && be == nil && os.SameFile(a, b)) {
					return fmt.Errorf("engine PID %d is running; close its MCP connection and retry", entry.ProcessID)
				}
			}
		}
		err = windows.Process32Next(h, &entry)
	}
	if err != windows.ERROR_NO_MORE_FILES {
		return err
	}
	return nil
}

// GuardStartup runs before the engine loads assets. A new engine cannot start
// while an installer owns the mutex. Existing mapped engines are checked above.
func GuardStartup() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".ceo-plugin")
	expected := filepath.Join(root, "bin", "ceo-plugin.exe")
	a, ae := os.Stat(exe)
	b, be := os.Stat(expected)
	if ae != nil {
		return ae
	}
	if be != nil || !os.SameFile(a, b) {
		return nil
	} // Portable engine: no managed installation lease.
	return WithInstallation(context.Background(), root, func() error {
		current, err := os.Stat(expected)
		if err != nil {
			return err
		}
		if !os.SameFile(a, current) {
			return fmt.Errorf("installation changed during startup; reconnect MCP to load the installed version")
		}
		return nil
	})
}
