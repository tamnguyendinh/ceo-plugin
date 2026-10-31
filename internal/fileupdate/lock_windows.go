package fileupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func rejectReparse(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	a, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("refusing reparse point: %s", path)
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	f, err := os.Open(parent)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n >= uint32(len(buf)) {
		return "", fmt.Errorf("canonical path too long")
	}
	return filepath.Join(windows.UTF16ToString(buf[:n]), filepath.Base(abs)), nil
}

// WithLock pins the goroutine: Win32 mutex ownership belongs to an OS thread.
// Names are per-user and global across Windows sessions, without workspace lock files.
func WithLock(ctx context.Context, identity string, fn func() error) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + sid + ")")
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	hash := sha256.Sum256([]byte(sid + ":" + strings.ToLower(identity)))
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Global\ceo-plugin-%x`, hash))
	if err != nil {
		return err
	}
	h, err := windows.CreateMutex(&sa, false, name)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return err
	}
	defer windows.CloseHandle(h)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock timeout: %s", identity)
		}
		status, err := windows.WaitForSingleObject(h, 50)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 || status == windows.WAIT_ABANDONED {
			defer windows.ReleaseMutex(h)
			return fn()
		}
		if status != uint32(windows.WAIT_TIMEOUT) {
			return fmt.Errorf("unexpected wait status %d", status)
		}
	}
}
