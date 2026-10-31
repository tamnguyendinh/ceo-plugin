package virtualfs

import (
	"crypto/sha256"
	"encoding/hex"
)

// AssetKind phân loại chủng loại tài sản
type AssetKind string

const (
	KindSkill     AssetKind = "skill"
	KindReference AssetKind = "reference"
	KindTemplate  AssetKind = "template"
)

// AssetEntry biểu diễn một tài sản tài liệu độc lập được lưu trữ 100% trong RAM
type AssetEntry struct {
	// URI chuẩn trong RAM: e.g. "ram://ceo/SKILL.md", "ram://system-architect/references/mode-1-spec-authoring-and-architecture.md"
	URI string `json:"uri"`

	// Đường dẫn tương đối nguồn trong embed.FS (e.g. "assets/system-architect/SKILL.md")
	Path string `json:"path"`

	// Tên vai trò đã chuẩn hóa (e.g. "ceo", "system-architect", "coder", "qa", "supervisor", "planner")
	Role string `json:"role"`

	// Loại tài sản: KindSkill ("skill"), KindReference ("reference"), KindTemplate ("template")
	Kind AssetKind `json:"kind"`

	// Tên định danh gốc của tệp bỏ đuôi mở rộng (e.g. "SKILL", "timer-rules", "plan.template")
	Name string `json:"name"`

	// Tên chuẩn hóa dùng cho tra cứu nhanh (chữ thường, chuyển dấu gạch dưới/chấm thành gạch ngang)
	NormalizedName string `json:"normalized_name"`

	// Nội dung toàn văn UTF-8 của tài liệu (bảo tồn nguyên vẹn ký tự kết thúc file EOF)
	Content string `json:"content"`

	// Dung lượng nội dung tính theo bytes
	Size int `json:"size"`
}

// String trả về toàn văn nội dung của tệp tài sản
func (e *AssetEntry) String() string {
	return e.Content
}

// ByteSize trả về kích thước byte của tài sản
func (e *AssetEntry) ByteSize() int {
	return e.Size
}

// SHA256 tính toán và trả về chuỗi băm hex SHA-256 của nội dung tài sản (bảo tồn nguyên vẹn bit-by-bit)
func (e *AssetEntry) SHA256() string {
	h := sha256.Sum256([]byte(e.Content))
	return hex.EncodeToString(h[:])
}
