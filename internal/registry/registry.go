package registry

import (
	"fmt"

	"ceo-plugin/internal/installstate"
	"ceo-plugin/internal/version"
	"golang.org/x/sys/windows/registry"
)

const (
	UninstallKeyPath    = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ceo-plugin`
	OldUninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\anvien-ceo-plugin`
)

// RegisterApp đăng ký ứng dụng vào Windows Installed Apps
func RegisterApp(installDir string, uninstallerPath string) error {
	uninstallCmd, err := installstate.UninstallCommand(uninstallerPath, false)
	if err != nil {
		return err
	}
	quietCmd, err := installstate.UninstallCommand(uninstallerPath, true)
	if err != nil {
		return err
	}
	// Xóa key cũ nếu tồn tại
	if err := registry.DeleteKey(registry.CURRENT_USER, OldUninstallKeyPath); err != nil && err != registry.ErrNotExist {
		return err
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, UninstallKeyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("không thể tạo registry key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue("DisplayName", "CEO Plugin"); err != nil {
		return err
	}
	if err := k.SetStringValue("DisplayVersion", version.Value); err != nil {
		return err
	}
	if err := k.SetStringValue("Publisher", "CEO Plugin"); err != nil {
		return err
	}
	if err := k.SetStringValue("InstallLocation", installDir); err != nil {
		return err
	}
	// Bootstrap owns the final result and removes its temporary worker after exit.
	if err := k.SetStringValue("UninstallString", uninstallCmd); err != nil {
		return err
	}
	if err := k.SetStringValue("QuietUninstallString", quietCmd); err != nil {
		return err
	}
	if err := k.SetDWordValue("NoModify", 1); err != nil {
		return err
	}
	if err := k.SetDWordValue("NoRepair", 1); err != nil {
		return err
	}

	return nil
}

// UnregisterApp xóa đăng ký khỏi Windows Installed Apps
func UnregisterApp() error {
	if err := registry.DeleteKey(registry.CURRENT_USER, OldUninstallKeyPath); err != nil && err != registry.ErrNotExist {
		return err
	}
	err := registry.DeleteKey(registry.CURRENT_USER, UninstallKeyPath)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("không thể xóa registry key: %w", err)
	}
	return nil
}
