package virtualfs

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrNotFound          = errors.New("virtualfs: resource not found")
	ErrRoleNotFound      = errors.New("virtualfs: role not found")
	ErrReferenceNotFound = errors.New("virtualfs: reference not found")
)

// VirtualAssetTree quản lý cây tài sản ảo và định tuyến URI trong RAM
type VirtualAssetTree struct {
	mu sync.RWMutex

	// entriesByURI lưu trữ map URI chuẩn hóa (chữ thường) -> *AssetEntry
	// e.g. "ram://ceo/skill.md" -> AssetEntry
	entriesByURI map[string]*AssetEntry

	// allEntries danh sách phẳng chứa toàn bộ các entry gốc duy nhất (chính xác 38 entries)
	allEntries []*AssetEntry

	// skills lưu trữ map roleAlias -> *AssetEntry (chỉ trỏ đến SKILL.md)
	// Hỗ trợ cả 8 vai trò chính thức và các alias thông dụng
	skills map[string]*AssetEntry

	// references lưu trữ map composite key "role:normalized_ref_name" -> *AssetEntry
	// Đảm bảo tra cứu O(1)
	references map[string]*AssetEntry
}

// NewVirtualAssetTree khởi tạo cây tài sản rỗng
func NewVirtualAssetTree() *VirtualAssetTree {
	return &VirtualAssetTree{
		entriesByURI: make(map[string]*AssetEntry),
		allEntries:   make([]*AssetEntry, 0, 40),
		skills:       make(map[string]*AssetEntry),
		references:   make(map[string]*AssetEntry),
	}
}

// normalizeRole chuẩn hóa tên role (chữ thường, thay thế gạch dưới bằng gạch nối, giải quyết alias)
func normalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	r = strings.ReplaceAll(r, "_", "-")
	switch r {
	case "ceo", "ceo-plugin", "anvien-ceo-plugin":
		return "ceo"
	case "system-architect":
		return "system-architect"
	case "architect":
		return "architect"
	case "coder":
		return "coder"
	case "qa":
		return "qa"
	case "supervisor":
		return "supervisor"
	case "mechanical-planner":
		return "mechanical-planner"
	case "strategic-planner":
		return "strategic-planner"
	case "planner":
		return "planner"
	default:
		return r
	}
}

// generateRefVariants tạo tất cả các biến thể tra cứu cho tên reference/template
func generateRefVariants(rawName, normName string) []string {
	seen := make(map[string]struct{})
	var result []string

	add := func(k string) {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			return
		}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, k)
		}
	}

	bases := []string{rawName, normName}
	for _, b := range bases {
		if b == "" {
			continue
		}
		clean := strings.TrimSuffix(strings.ToLower(b), ".md")
		add(clean)
		add(clean + ".md")

		// Biến thể dấu gạch nối
		hyphen := strings.ReplaceAll(strings.ReplaceAll(clean, ".", "-"), "_", "-")
		add(hyphen)
		add(hyphen + ".md")

		// Biến thể dấu chấm
		dot := strings.ReplaceAll(strings.ReplaceAll(clean, "-", "."), "_", ".")
		add(dot)
		add(dot + ".md")

		// Biến thể dấu gạch dưới
		under := strings.ReplaceAll(strings.ReplaceAll(clean, ".", "_"), "-", "_")
		add(under)
		add(under + ".md")
	}

	return result
}

// registerURI đăng ký một URI và biến thể không có đuôi .md vào bảng tra cứu
func (t *VirtualAssetTree) registerURI(uri string, entry *AssetEntry) {
	norm := strings.ToLower(strings.TrimSpace(uri))
	if norm == "" {
		return
	}
	t.entriesByURI[norm] = entry
	if strings.HasSuffix(norm, ".md") {
		t.entriesByURI[strings.TrimSuffix(norm, ".md")] = entry
	}
}

// AddEntry nạp một AssetEntry vào cây và thiết lập các khóa chỉ mục tra cứu O(1)
func (t *VirtualAssetTree) AddEntry(entry *AssetEntry) error {
	staged := NewVirtualAssetTree()
	if err := staged.addEntry(entry); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range staged.entriesByURI {
		if _, ok := t.entriesByURI[k]; ok {
			return fmt.Errorf("duplicate resource URI %q", k)
		}
	}
	for k := range staged.skills {
		if _, ok := t.skills[k]; ok {
			return fmt.Errorf("duplicate skill alias %q", k)
		}
	}
	for k := range staged.references {
		if _, ok := t.references[k]; ok {
			return fmt.Errorf("duplicate reference alias %q", k)
		}
	}
	for k, v := range staged.entriesByURI {
		t.entriesByURI[k] = v
	}
	for k, v := range staged.skills {
		t.skills[k] = v
	}
	for k, v := range staged.references {
		t.references[k] = v
	}
	t.allEntries = append(t.allEntries, entry)
	return nil
}

func (t *VirtualAssetTree) addEntry(entry *AssetEntry) error {
	if entry == nil {
		return errors.New("virtualfs: nil entry")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Kiểm tra chống trùng lặp trong allEntries
	exists := false
	for _, e := range t.allEntries {
		if (entry.Path != "" && e.Path == entry.Path) || (entry.URI != "" && e.URI == entry.URI) {
			exists = true
			break
		}
	}
	if !exists {
		t.allEntries = append(t.allEntries, entry)
	}

	// 2. Đăng ký URI chuẩn hóa và các alias URI
	normURI := strings.ToLower(strings.TrimSpace(entry.URI))
	if normURI != "" {
		t.registerURI(normURI, entry)

		// A. Thiết lập alias URI cho phân làn CEO
		if entry.Role == "ceo" {
			aliasPlugin := strings.Replace(normURI, "ram://ceo/", "ram://ceo-plugin/", 1)
			t.registerURI(aliasPlugin, entry)
			aliasAnvien := strings.Replace(normURI, "ram://ceo/", "ram://anvien-ceo-plugin/", 1)
			t.registerURI(aliasAnvien, entry)
		}

		// B. Thiết lập alias URI cho System-Architect -> Architect
		if entry.Role == "system-architect" {
			aliasArch := strings.Replace(normURI, "ram://system-architect/", "ram://architect/", 1)
			t.registerURI(aliasArch, entry)
		}

		// C. Thiết lập alias URI cho Planner (strategic-planner & mechanical-planner)
		if entry.Role == "planner" || entry.Role == "strategic-planner" {
			if entry.Kind == KindSkill {
				t.registerURI("ram://planner/SKILL.md", entry)
				t.registerURI("ram://strategic-planner/SKILL.md", entry)
				t.registerURI("ram://mechanical-planner/SKILL.md", entry)
			} else if entry.Kind == KindTemplate {
				// Trích xuất tên tệp template
				var filename string
				if strings.Contains(normURI, "templates/") {
					filename = normURI[strings.Index(normURI, "templates/")+len("templates/"):]
				} else {
					filename = entry.Name + ".md"
				}
				t.registerURI("ram://strategic-planner/templates/"+filename, entry)
				t.registerURI("ram://planner/templates/"+filename, entry)
				t.registerURI("ram://mechanical-planner/templates/"+filename, entry)
			}
		}
	}

	// 3. Đăng ký Skills nếu là KindSkill
	if entry.Kind == KindSkill {
		t.skills[entry.Role] = entry
		if entry.Role == "system-architect" {
			t.skills["architect"] = entry
			t.skills["system_architect"] = entry
		} else if entry.Role == "planner" {
			t.skills["mechanical-planner"] = entry
			t.skills["mechanical_planner"] = entry
			t.skills["strategic-planner"] = entry
			t.skills["strategic_planner"] = entry
		} else if entry.Role == "ceo" {
			t.skills["ceo-plugin"] = entry
			t.skills["ceo_plugin"] = entry
			t.skills["anvien-ceo-plugin"] = entry
			t.skills["anvien_ceo_plugin"] = entry
		}
	}

	// 4. Đăng ký References/Templates nếu là KindReference hoặc KindTemplate
	if entry.Kind == KindReference || entry.Kind == KindTemplate {
		roles := []string{entry.Role}
		if entry.Role == "system-architect" {
			roles = append(roles, "architect", "system_architect")
		} else if entry.Role == "planner" || entry.Role == "strategic-planner" {
			roles = append(roles, "strategic-planner", "strategic_planner", "mechanical-planner", "mechanical_planner", "planner")
		} else if entry.Role == "ceo" {
			roles = append(roles, "ceo-plugin", "ceo_plugin", "anvien-ceo-plugin", "anvien_ceo_plugin")
		}

		variants := generateRefVariants(entry.Name, entry.NormalizedName)
		for _, r := range roles {
			normR := normalizeRole(r)
			for _, v := range variants {
				key := normR + ":" + v
				t.references[key] = entry
			}
		}
	}

	return nil
}

// FindByURI tìm kiếm tài sản chính xác theo RAM URI (không phân biệt hoa thường)
func (t *VirtualAssetTree) FindByURI(uri string) (*AssetEntry, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	normURI := strings.ToLower(strings.TrimSpace(uri))
	entry, ok := t.entriesByURI[normURI]
	if !ok || entry == nil {
		return nil, ErrNotFound
	}
	return entry, nil
}

// GetSkill lấy duy nhất nội dung SKILL.md của vai trò chỉ định
func (t *VirtualAssetTree) GetSkill(role string) (*AssetEntry, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	normRole := normalizeRole(role)
	entry, ok := t.skills[normRole]
	if !ok || entry == nil {
		return nil, ErrRoleNotFound
	}
	return entry, nil
}

// GetReference lấy duy nhất nội dung quy chế hoặc biểu mẫu chỉ định của vai trò
func (t *VirtualAssetTree) GetReference(role string, refName string) (*AssetEntry, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	refName = strings.ReplaceAll(strings.TrimSpace(refName), `\`, "/")
	refName = strings.TrimPrefix(refName, "./")
	wantKind := AssetKind("")
	if strings.HasPrefix(strings.ToLower(refName), "references/") {
		wantKind = KindReference
		refName = refName[len("references/"):]
	}
	if wantKind == "" && strings.HasPrefix(strings.ToLower(refName), "templates/") {
		wantKind = KindTemplate
		refName = refName[len("templates/"):]
	}
	if refName == "" || strings.ContainsAny(refName, "/:") || strings.Contains(refName, "..") {
		return nil, ErrReferenceNotFound
	}

	normRole := normalizeRole(role)
	// Kiểm tra nếu vai trò hoàn toàn không được biết đến
	if _, roleKnown := t.skills[normRole]; !roleKnown {
		return nil, ErrRoleNotFound
	}

	// Thử tìm theo danh sách biến thể của refName
	variants := generateRefVariants(refName, refName)
	for _, v := range variants {
		key := normRole + ":" + v
		if entry, ok := t.references[key]; ok && entry != nil && (wantKind == "" || entry.Kind == wantKind) {
			return entry, nil
		}
	}

	// Thừa hưởng vai trò (Inheritance fallback)
	if normRole == "architect" {
		for _, v := range variants {
			key := "system-architect:" + v
			if entry, ok := t.references[key]; ok && entry != nil && (wantKind == "" || entry.Kind == wantKind) {
				return entry, nil
			}
		}
	} else if normRole == "strategic-planner" || normRole == "mechanical-planner" {
		for _, v := range variants {
			key := "planner:" + v
			if entry, ok := t.references[key]; ok && entry != nil && (wantKind == "" || entry.Kind == wantKind) {
				return entry, nil
			}
		}
	}

	return nil, ErrReferenceNotFound
}

// ListEntries trả về danh sách tất cả các tài sản duy nhất (dùng cho MCP resources/list)
func (t *VirtualAssetTree) ListEntries() []*AssetEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entries := make([]*AssetEntry, len(t.allEntries))
	copy(entries, t.allEntries)
	return entries
}

// Count trả về tổng số tài sản duy nhất đang quản lý (chính xác 38 files)
func (t *VirtualAssetTree) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.allEntries)
}
