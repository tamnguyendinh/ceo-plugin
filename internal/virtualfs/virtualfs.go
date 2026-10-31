package virtualfs

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// NewTreeFromFS duyệt đệ quy cây thư mục fs.FS và xây dựng VirtualAssetTree trên RAM.
// Toàn bộ 38 tài sản được nạp trực tiếp vào Heap RAM của tiến trình, tuyệt đối 0 bytes disk I/O.
func NewTreeFromFS(fsys fs.FS) (*VirtualAssetTree, error) {
	if fsys == nil {
		return nil, errors.New("virtualfs: nil filesystem")
	}

	tree := NewVirtualAssetTree()

	walkErr := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		cleanPath := filepath.ToSlash(p)
		if !strings.HasSuffix(strings.ToLower(cleanPath), ".md") {
			return nil
		}

		// Loại bỏ tiền tố "assets/" hoặc "./" nếu có để phân tích đường dẫn logic
		logicalPath := cleanPath
		logicalPath = strings.TrimPrefix(logicalPath, "./")
		logicalPath = strings.TrimPrefix(logicalPath, "assets/")

		parts := strings.Split(logicalPath, "/")
		if len(parts) == 0 {
			return nil
		}

		role := strings.ToLower(parts[0])
		var kind AssetKind
		var name string
		var uri string

		if len(parts) == 2 && strings.EqualFold(parts[1], "SKILL.md") {
			kind = KindSkill
			name = "SKILL"
			uri = fmt.Sprintf("ram://%s/SKILL.md", role)
		} else if len(parts) == 3 && strings.EqualFold(parts[1], "references") {
			kind = KindReference
			filename := parts[2]
			name = strings.TrimSuffix(filename, filepath.Ext(filename))
			uri = fmt.Sprintf("ram://%s/references/%s", role, filename)
		} else if len(parts) == 3 && strings.EqualFold(parts[1], "templates") {
			kind = KindTemplate
			filename := parts[2]
			name = strings.TrimSuffix(filename, filepath.Ext(filename))
			// Gán URI chính thức ram://strategic-planner/templates/<filename> theo đặc tả
			if role == "planner" {
				role = "strategic-planner"
			}
			uri = fmt.Sprintf("ram://%s/templates/%s", role, filename)
		} else {
			return fmt.Errorf("virtualfs: unsupported asset layout: %s", p)
		}

		normalizedName := strings.ToLower(name)
		normalizedName = strings.ReplaceAll(normalizedName, ".", "-")
		normalizedName = strings.ReplaceAll(normalizedName, "_", "-")

		contentBytes, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return fmt.Errorf("virtualfs: error reading %s: %w", p, readErr)
		}

		entry := &AssetEntry{
			URI:            uri,
			Path:           cleanPath,
			Role:           role,
			Kind:           kind,
			Name:           name,
			NormalizedName: normalizedName,
			Content:        string(contentBytes),
			Size:           len(contentBytes),
		}

		if addErr := tree.AddEntry(entry); addErr != nil {
			return addErr
		}

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("virtualfs: walk error: %w", walkErr)
	}

	// Sắp xếp allEntries theo Path để đảm bảo danh sách trả về luôn có thứ tự xác định
	sort.Slice(tree.allEntries, func(i, j int) bool {
		return tree.allEntries[i].Path < tree.allEntries[j].Path
	})

	return tree, nil
}
