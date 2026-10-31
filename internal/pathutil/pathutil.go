package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	moduser32              = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = moduser32.NewProc("SendMessageTimeoutW")
)

const (
	HWND_BROADCAST   = 0xffff
	WM_SETTINGCHANGE = 0x001a
	SMTO_ABORTIFHUNG = 0x0002
)

// BroadcastEnvironmentChange phát thông điệp Win32 báo cho Windows Explorer
// và toàn bộ ứng dụng cấp cao nạp lại biến môi trường mà không cần khởi động lại máy.
func BroadcastEnvironmentChange() error {
	envStr, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	var result uintptr
	ok, _, callErr := procSendMessageTimeout.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(envStr)),
		uintptr(SMTO_ABORTIFHUNG),
		uintptr(5000), // Timeout 5 giây
		uintptr(unsafe.Pointer(&result)),
	)
	if ok == 0 {
		return fmt.Errorf("PATH saved, but environment broadcast failed: %v", callErr)
	}
	return nil
}

// isLegacyOrDuplicate kiểm tra xem một đường dẫn có phải là targetDir hoặc thư mục cũ cần dọn dẹp không
func isLegacyOrDuplicate(p string, targetDir string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(p))
	if strings.EqualFold(cleaned, targetDir) {
		return true
	}
	// Dọn sạch thư mục cũ của anvien-ceo-plugin
	home, err := os.UserHomeDir()
	if err == nil && strings.EqualFold(cleaned, filepath.Join(home, ".anvien-ceo-plugin", "bin")) {
		return true
	}
	return false
}

// AddUserPath thêm targetDir vào PATH của User (HKCU\Environment) với kiểu REG_EXPAND_SZ
// đồng thời dọn sạch rác cũ và phát tín hiệu Win32 Broadcast.
func AddUserPath(targetDir string) error {
	targetDir = filepath.Clean(targetDir)
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("không thể mở Registry Environment: %w", err)
	}
	defer k.Close()

	pathVal, _, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}

	parts := strings.Split(pathVal, ";")
	if pathVal == "" {
		parts = nil
	}
	var newParts []string
	for _, p := range parts {
		if !isLegacyOrDuplicate(p, targetDir) {
			newParts = append(newParts, p)
		}
	}
	// Đưa targetDir mới vào cuối chuỗi PATH
	newParts = append(newParts, targetDir)
	newPath := strings.Join(newParts, ";")
	if newPath == pathVal {
		return nil
	}

	// BẮT BUỘC dùng SetExpandStringValue để bảo tồn kiểu REG_EXPAND_SZ chuẩn của Windows
	if err := k.SetExpandStringValue("Path", newPath); err != nil {
		return err
	}

	// Phát tín hiệu Win32 Broadcast cho Windows Explorer
	return BroadcastEnvironmentChange()
}

// RemoveUserPath gỡ targetDir khỏi PATH của User khi gỡ cài đặt và phát Win32 Broadcast
func RemoveUserPath(targetDir string) error {
	targetDir = filepath.Clean(targetDir)
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	pathVal, _, err := k.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}

	parts := strings.Split(pathVal, ";")
	var newParts []string
	for _, p := range parts {
		if !isLegacyOrDuplicate(p, targetDir) {
			newParts = append(newParts, p)
		}
	}

	newPath := strings.Join(newParts, ";")
	if newPath == pathVal {
		return nil
	}
	if err := k.SetExpandStringValue("Path", newPath); err != nil {
		return err
	}

	// Phát tín hiệu Win32 Broadcast
	return BroadcastEnvironmentChange()
}
