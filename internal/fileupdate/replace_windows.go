package fileupdate

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func preservePermissions(original, temp string) error {
	sd, err := windows.GetNamedSecurityInfo(original, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(temp, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func replace(temp, target string, exists bool) ([]string, error) {
	src, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return nil, err
	}
	dst, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, windows.MoveFileEx(src, dst, windows.MOVEFILE_WRITE_THROUGH)
	}
	backup := temp + ".original"
	bak, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return nil, err
	}
	ok, _, callErr := replaceFile.Call(uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(bak)), 0, 0, 0)
	if ok == 0 {
		// Some ReplaceFileW failures rename the original. Preserve every surviving
		// transaction file for recovery; never erase the only remaining copy.
		var recovery []string
		for _, p := range []string{target, temp, backup} {
			if _, e := os.Stat(p); e == nil {
				recovery = append(recovery, p)
			}
		}
		return recovery, fmt.Errorf("ReplaceFileW: %w; recovery files: %v", callErr, recovery)
	}
	if err := os.Remove(backup); err != nil {
		return []string{target, backup}, fmt.Errorf("remove owned backup: %w", err)
	}
	return nil, nil
}
