package aicontext

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed instructions.md
var embeddedInstructions []byte

// EmbeddedInstructionsContent trả về slice byte nguyên bản của instructions.md
func EmbeddedInstructionsContent() []byte {
	return embeddedInstructions
}

// InstructionTarget định danh một IDE đích cần xuất instructions.md
type InstructionTarget struct {
	IDE  string
	Path string
}

// DefaultInstructionTargets trả về danh sách 4 IDE targets chuẩn dựa trên home và appData
func DefaultInstructionTargets(home, appData string) []InstructionTarget {
	targets := []InstructionTarget{
		{
			IDE:  "Antigravity",
			Path: filepath.Join(home, ".gemini", "antigravity", "mcp", "ceo-plugin", "instructions.md"),
		},
		{
			IDE:  "Codex",
			Path: filepath.Join(home, ".codex", "mcp", "ceo-plugin", "instructions.md"),
		},
		{
			IDE:  "Cursor",
			Path: filepath.Join(home, ".cursor", "mcp", "ceo-plugin", "instructions.md"),
		},
	}
	if appData != "" {
		targets = append(targets, InstructionTarget{
			IDE:  "Claude",
			Path: filepath.Join(appData, "Claude", "mcp", "ceo-plugin", "instructions.md"),
		})
	} else {
		targets = append(targets, InstructionTarget{
			IDE:  "Claude",
			Path: "",
		})
	}
	return targets
}

// ExportMCPInstructions xuất file instructions.md ra tất cả target IDE
func ExportMCPInstructions(targets []InstructionTarget, out io.Writer) error {
	if len(embeddedInstructions) == 0 {
		return fmt.Errorf("embedded instructions payload is empty")
	}

	for _, target := range targets {
		if target.Path == "" {
			if out != nil {
				fmt.Fprintf(out, "%s: instructions skipped (unresolved path)\n", target.IDE)
			}
			continue
		}

		dir := filepath.Dir(target.Path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("%s: create directory %s: %w", target.IDE, dir, err)
		}

		// Kiểm tra tính bất biến (idempotency): Nếu file đã có và nội dung trùng khớp 100%, không cần ghi lại
		if existing, err := os.ReadFile(target.Path); err == nil && bytes.Equal(existing, embeddedInstructions) {
			if out != nil {
				fmt.Fprintf(out, "%s: instructions unchanged %s\n", target.IDE, target.Path)
			}
			continue
		}

		// Ghi an toàn thông qua file tạm (Atomic Write)
		tmpPath := target.Path + ".tmp"
		if err := os.WriteFile(tmpPath, embeddedInstructions, 0644); err != nil {
			return fmt.Errorf("%s: write temp instructions %s: %w", target.IDE, tmpPath, err)
		}

		// Thay thế nguyên tử file đích
		if err := os.Rename(tmpPath, target.Path); err != nil {
			// Trên Windows, nếu rename lỗi do file đích đang tồn tại, xóa trước rồi rename
			_ = os.Remove(target.Path)
			if errRename := os.Rename(tmpPath, target.Path); errRename != nil {
				_ = os.Remove(tmpPath)
				return fmt.Errorf("%s: replace instructions %s: %w", target.IDE, target.Path, errRename)
			}
		}

		// Xác thực hậu kiểm
		written, err := os.ReadFile(target.Path)
		if err != nil || !bytes.Equal(written, embeddedInstructions) {
			return fmt.Errorf("%s: verification failed after write: %s", target.IDE, target.Path)
		}

		if out != nil {
			fmt.Fprintf(out, "%s: instructions installed %s\n", target.IDE, target.Path)
		}
	}

	return nil
}

// RemoveMCPInstructions dọn dẹp file instructions.md và xóa thư mục rỗng khi gỡ cài đặt
func RemoveMCPInstructions(targets []InstructionTarget, out io.Writer) error {
	for _, target := range targets {
		if target.Path == "" {
			continue
		}

		info, err := os.Stat(target.Path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%s: refusing to remove non-regular file: %s", target.IDE, target.Path)
			}
			if err := os.Remove(target.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("%s: remove instructions %s: %w", target.IDE, target.Path, err)
			}
			if out != nil {
				fmt.Fprintf(out, "%s: instructions removed %s\n", target.IDE, target.Path)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s: inspect instructions %s: %w", target.IDE, target.Path, err)
		}

		// Dọn dẹp thư mục cha nếu thư mục đó tên là "ceo-plugin" và đã rỗng hoàn toàn
		dir := filepath.Dir(target.Path)
		if filepath.Base(dir) == "ceo-plugin" {
			entries, err := os.ReadDir(dir)
			if err == nil && len(entries) == 0 {
				if errRemoveDir := os.Remove(dir); errRemoveDir == nil {
					if out != nil {
						fmt.Fprintf(out, "%s: empty directory removed %s\n", target.IDE, dir)
					}
				}
			}
		}
	}

	return nil
}
