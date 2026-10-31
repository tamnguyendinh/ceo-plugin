package virtualfs_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ceo-plugin/internal/virtualfs"
)

const assetsRelPath = "../../cmd/engine/assets"

func initTestTree(t testing.TB) *virtualfs.VirtualAssetTree {
	fsys := os.DirFS(assetsRelPath)
	tree, err := virtualfs.NewTreeFromFS(fsys)
	if err != nil {
		t.Fatalf("failed to initialize VirtualAssetTree from FS: %v", err)
	}
	return tree
}

// TestNewTreeFromFS_Count39 xác nhận VirtualAssetTree quản lý chính xác 39 files độc lập
func TestNewTreeFromFS_Count39(t *testing.T) {
	tree := initTestTree(t)

	if count := tree.Count(); count != 39 {
		t.Fatalf("expected exactly 39 assets, got %d", count)
	}

	entries := tree.ListEntries()
	if len(entries) != 39 {
		t.Fatalf("expected exactly 39 entries from ListEntries(), got %d", len(entries))
	}

	for _, entry := range entries {
		if entry.URI == "" {
			t.Errorf("entry has empty URI: %+v", entry)
		}
		if entry.Size <= 0 || len(entry.Content) <= 0 {
			t.Errorf("entry has invalid size/content: %s (size: %d)", entry.URI, entry.Size)
		}
		if entry.Role == "" {
			t.Errorf("entry has empty role: %s", entry.URI)
		}
	}
}

// TestGetSkill_ExactAndUnchangedContent xác nhận 8 lệnh tổng trả về duy nhất SKILL.md nguyên bản 100%
func TestGetSkill_ExactAndUnchangedContent(t *testing.T) {
	tree := initTestTree(t)

	cases := []struct {
		role         string
		expectedPath string
		expectedSize int
	}{
		{"ceo", "ceo/SKILL.md", 13294},
		{"CEO", "ceo/SKILL.md", 13294},
		{"ceo-plugin", "ceo/SKILL.md", 13294},
		{"anvien_ceo_plugin", "ceo/SKILL.md", 13294},
		{"system-architect", "system-architect/SKILL.md", 7911},
		{"system_architect", "system-architect/SKILL.md", 7911},
		{"architect", "system-architect/SKILL.md", 7911},
		{"coder", "coder/SKILL.md", 16647},
		{"qa", "qa/SKILL.md", 5922},
		{"supervisor", "supervisor/SKILL.md", 4713},
		{"planner", "planner/SKILL.md", 10499},
		{"mechanical-planner", "planner/SKILL.md", 10499},
		{"strategic-planner", "planner/SKILL.md", 10499},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			entry, err := tree.GetSkill(tc.role)
			if err != nil {
				t.Fatalf("GetSkill(%q) returned error: %v", tc.role, err)
			}

			if entry.Kind != virtualfs.KindSkill {
				t.Errorf("expected KindSkill, got %q", entry.Kind)
			}

			if entry.ByteSize() != tc.expectedSize {
				t.Errorf("expected size %d, got %d", tc.expectedSize, entry.ByteSize())
			}

			// Đọc file đối chứng trên đĩa để kiểm tra tính toàn vẹn 100% bit-by-bit
			diskBytes, readErr := os.ReadFile(filepath.Join(assetsRelPath, tc.expectedPath))
			if readErr != nil {
				t.Fatalf("failed to read disk file for verification: %v", readErr)
			}

			diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
			if entry.SHA256() != diskSHA {
				t.Errorf("SHA-256 mismatch for %q: entry=%s disk=%s", tc.role, entry.SHA256(), diskSHA)
			}

			if entry.String() != string(diskBytes) {
				t.Errorf("content string mismatch for %q", tc.role)
			}
		})
	}

	// Kiểm tra trường hợp vai trò không tồn tại
	_, err := tree.GetSkill("unknown_lane")
	if !errors.Is(err, virtualfs.ErrRoleNotFound) {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

// TestGetReference_PunctuationAndAliases kiểm tra tra cứu quy chế/biểu mẫu với chuẩn hóa dấu câu và alias
func TestGetReference_PunctuationAndAliases(t *testing.T) {
	tree := initTestTree(t)

	cases := []struct {
		role         string
		refName      string
		expectedPath string
		expectedSize int
	}{
		{
			"ceo",
			"Template-Prompt-for-Opening-a-Session",
			"ceo/references/Template-Prompt-for-Opening-a-Session.md",
			9395,
		},
		{
			"CEO",
			"TEMPLATE-PROMPT-FOR-OPENING-A-SESSION.MD",
			"ceo/references/Template-Prompt-for-Opening-a-Session.md",
			9395,
		},
		{
			"ceo",
			"timer-rules",
			"ceo/references/timer-rules.md",
			17598,
		},
		{
			"strategic-planner",
			"plan-template",
			"planner/templates/plan.template.md",
			15479,
		},
		{
			"strategic-planner",
			"plan.template",
			"planner/templates/plan.template.md",
			15479,
		},
		{
			"strategic-planner",
			"plan.template.md",
			"planner/templates/plan.template.md",
			15479,
		},
		{
			"strategic-planner",
			"actual-status-template",
			"planner/templates/actual-status.template.md",
			7581,
		},
		{
			"strategic-planner",
			"benchmark-template",
			"planner/templates/benchmark.template.md",
			2547,
		},
		{
			"strategic-planner",
			"evidence-template",
			"planner/templates/evidence.template.md",
			2970,
		},
		{
			"system-architect",
			"mode-1-spec-authoring-and-architecture",
			"system-architect/references/mode-1-spec-authoring-and-architecture.md",
			11762,
		},
		{
			"architect",
			"mode-1-spec-authoring-and-architecture",
			"system-architect/references/mode-1-spec-authoring-and-architecture.md",
			11762,
		},
		{
			"qa",
			"backend-qa-protocol",
			"qa/references/backend-qa-protocol.md",
			4275,
		},
		{
			"supervisor",
			"evidence-and-source-inspection",
			"supervisor/references/evidence-and-source-inspection.md",
			2736,
		},
	}

	for _, tc := range cases {
		name := tc.role + "_" + tc.refName
		t.Run(name, func(t *testing.T) {
			entry, err := tree.GetReference(tc.role, tc.refName)
			if err != nil {
				t.Fatalf("GetReference(%q, %q) returned error: %v", tc.role, tc.refName, err)
			}

			if entry.ByteSize() != tc.expectedSize {
				t.Errorf("expected size %d, got %d", tc.expectedSize, entry.ByteSize())
			}

			diskBytes, readErr := os.ReadFile(filepath.Join(assetsRelPath, tc.expectedPath))
			if readErr != nil {
				t.Fatalf("failed to read disk file: %v", readErr)
			}

			diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
			if entry.SHA256() != diskSHA {
				t.Errorf("SHA-256 mismatch for %s:%s", tc.role, tc.refName)
			}
		})
	}

	// Lỗi khi role không tồn tại
	_, err := tree.GetReference("nonexistent_role", "any")
	if !errors.Is(err, virtualfs.ErrRoleNotFound) {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}

	// Lỗi khi reference không tồn tại trong role hợp lệ
	_, err = tree.GetReference("ceo", "nonexistent_ref")
	if !errors.Is(err, virtualfs.ErrReferenceNotFound) {
		t.Errorf("expected ErrReferenceNotFound, got %v", err)
	}
}

// TestFindByURI kiểm tra phân giải chính xác các URI ram://
func TestFindByURI(t *testing.T) {
	tree := initTestTree(t)

	uris := []string{
		"ram://ceo/SKILL.md",
		"ram://ceo/skill.md",
		"ram://ceo/references/Template-Prompt-for-Opening-a-Session.md",
		"ram://system-architect/SKILL.md",
		"ram://architect/SKILL.md",
		"ram://system-architect/references/mode-1-spec-authoring-and-architecture.md",
		"ram://architect/references/mode-1-spec-authoring-and-architecture.md",
		"ram://coder/SKILL.md",
		"ram://qa/SKILL.md",
		"ram://supervisor/SKILL.md",
		"ram://planner/SKILL.md",
		"ram://strategic-planner/SKILL.md",
		"ram://mechanical-planner/SKILL.md",
		"ram://strategic-planner/templates/plan.template.md",
		"ram://planner/templates/plan.template.md",
	}

	for _, uri := range uris {
		t.Run(uri, func(t *testing.T) {
			entry, err := tree.FindByURI(uri)
			if err != nil {
				t.Fatalf("FindByURI(%q) error: %v", uri, err)
			}
			if entry.Content == "" {
				t.Errorf("entry for %q has empty content", uri)
			}
		})
	}

	// URI không tồn tại
	_, err := tree.FindByURI("ram://invalid/uri.md")
	if !errors.Is(err, virtualfs.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestEOFPreservation xác nhận 100% tài sản bảo toàn nguyên vẹn ranh giới EOF và khớp SHA-256 bit-by-bit
func TestEOFPreservation(t *testing.T) {
	tree := initTestTree(t)

	entries := tree.ListEntries()
	if len(entries) != 39 {
		t.Fatalf("expected 39 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		// Đường dẫn tương đối từ vị trí test
		diskPath := filepath.Join(assetsRelPath, strings.TrimPrefix(entry.Path, "assets/"))
		diskBytes, err := os.ReadFile(diskPath)
		if err != nil {
			t.Fatalf("failed to read disk file %s: %v", diskPath, err)
		}

		if len(entry.Content) != len(diskBytes) {
			t.Errorf("[%s] byte length mismatch: entry=%d, disk=%d", entry.URI, len(entry.Content), len(diskBytes))
		}

		// Kiểm tra ký tự kết thúc file EOF
		if len(diskBytes) > 0 {
			lastDiskByte := diskBytes[len(diskBytes)-1]
			lastEntryByte := entry.Content[len(entry.Content)-1]
			if lastDiskByte != lastEntryByte {
				t.Errorf("[%s] EOF byte mismatch: entry=%d, disk=%d", entry.URI, lastEntryByte, lastDiskByte)
			}
		}

		// Kiểm tra băm SHA-256 bit-by-bit
		diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
		if entry.SHA256() != diskSHA {
			t.Errorf("[%s] SHA-256 mismatch: entry=%s, disk=%s", entry.URI, entry.SHA256(), diskSHA)
		}
	}
}

// BenchmarkNewTreeFromFS đo lường thời gian nạp cây tài sản vào RAM (Mục tiêu: < 5 ms)
func BenchmarkNewTreeFromFS(b *testing.B) {
	fsys := os.DirFS(assetsRelPath)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree, err := virtualfs.NewTreeFromFS(fsys)
		if err != nil {
			b.Fatalf("NewTreeFromFS failed: %v", err)
		}
		if tree.Count() != 39 {
			b.Fatalf("expected 39 assets, got %d", tree.Count())
		}
	}
}

// BenchmarkLookupO1 đo lường tốc độ tra cứu O(1) trong RAM (Mục tiêu: < 5 µs)
func BenchmarkLookupO1(b *testing.B) {
	tree := initTestTree(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = tree.GetSkill("ceo")
		_, _ = tree.GetSkill("architect")
		_, _ = tree.GetReference("ceo", "Template-Prompt-for-Opening-a-Session")
		_, _ = tree.GetReference("strategic-planner", "plan-template")
		_, _ = tree.FindByURI("ram://strategic-planner/templates/plan.template.md")
	}
}

// TestAll39CanonicalRAMURIs xác nhận toàn bộ 39 Canonical RAM URIs theo đúng bảng báo cáo kiến trúc
func TestAll39CanonicalRAMURIs(t *testing.T) {
	tree := initTestTree(t)

	canonicalURIs := []struct {
		uri          string
		diskPath     string
		expectedSize int
		role         string
		kind         virtualfs.AssetKind
	}{
		// A. Phân làn CEO (12 tài sản)
		{"ram://ceo/SKILL.md", "ceo/SKILL.md", 13294, "ceo", virtualfs.KindSkill},
		{"ram://ceo/references/authority-and-command.md", "ceo/references/authority-and-command.md", 3655, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/how-to-handle-fast-block-fast-reject.md", "ceo/references/how-to-handle-fast-block-fast-reject.md", 2071, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/how-to-spawn-lane-in-antigravity.md", "ceo/references/how-to-spawn-lane-in-antigravity.md", 2845, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/how-to-spawn-lane-in-codex.md", "ceo/references/how-to-spawn-lane-in-codex.md", 3356, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/lane-and-skill-coordination.md", "ceo/references/lane-and-skill-coordination.md", 5832, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/recovery-and-documentation.md", "ceo/references/recovery-and-documentation.md", 4071, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/standby-rules.md", "ceo/references/standby-rules.md", 3382, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/subagent-reporting-and-handoff.md", "ceo/references/subagent-reporting-and-handoff.md", 2917, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/Template-Prompt-for-Opening-a-Session.md", "ceo/references/Template-Prompt-for-Opening-a-Session.md", 9395, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/timer-rules.md", "ceo/references/timer-rules.md", 17598, "ceo", virtualfs.KindReference},
		{"ram://ceo/references/user-intervention.md", "ceo/references/user-intervention.md", 1322, "ceo", virtualfs.KindReference},

		// B. Phân làn System-Architect (6 tài sản)
		{"ram://system-architect/SKILL.md", "system-architect/SKILL.md", 7911, "system-architect", virtualfs.KindSkill},
		{"ram://system-architect/references/architect-review-coordination.md", "system-architect/references/architect-review-coordination.md", 3505, "system-architect", virtualfs.KindReference},
		{"ram://system-architect/references/mode-0-interactive-architecture-discussion.md", "system-architect/references/mode-0-interactive-architecture-discussion.md", 6653, "system-architect", virtualfs.KindReference},
		{"ram://system-architect/references/mode-1-spec-authoring-and-architecture.md", "system-architect/references/mode-1-spec-authoring-and-architecture.md", 11762, "system-architect", virtualfs.KindReference},
		{"ram://system-architect/references/mode-2-execution-planning-and-jobs.md", "system-architect/references/mode-2-execution-planning-and-jobs.md", 9891, "system-architect", virtualfs.KindReference},
		{"ram://system-architect/references/reporting-and-artifact-lifecycle.md", "system-architect/references/reporting-and-artifact-lifecycle.md", 2280, "system-architect", virtualfs.KindReference},

		// C. Phân làn Coder (1 tài sản)
		{"ram://coder/SKILL.md", "coder/SKILL.md", 16647, "coder", virtualfs.KindSkill},

		// D. Phân làn QA (10 tài sản)
		{"ram://qa/SKILL.md", "qa/SKILL.md", 5922, "qa", virtualfs.KindSkill},
		{"ram://qa/references/backend-qa-protocol.md", "qa/references/backend-qa-protocol.md", 4275, "qa", virtualfs.KindReference},
		{"ram://qa/references/e2e-data-flow.md", "qa/references/e2e-data-flow.md", 1189, "qa", virtualfs.KindReference},
		{"ram://qa/references/evidence-and-blockers.md", "qa/references/evidence-and-blockers.md", 1814, "qa", virtualfs.KindReference},
		{"ram://qa/references/frontend-automation-and-control-sweep.md", "qa/references/frontend-automation-and-control-sweep.md", 2725, "qa", virtualfs.KindReference},
		{"ram://qa/references/frontend-coverage-model.md", "qa/references/frontend-coverage-model.md", 2649, "qa", virtualfs.KindReference},
		{"ram://qa/references/frontend-i18n-and-locale.md", "qa/references/frontend-i18n-and-locale.md", 490, "qa", virtualfs.KindReference},
		{"ram://qa/references/frontend-runtime-and-build-rules.md", "qa/references/frontend-runtime-and-build-rules.md", 5957, "qa", virtualfs.KindReference},
		{"ram://qa/references/reporting.md", "qa/references/reporting.md", 4477, "qa", virtualfs.KindReference},
		{"ram://qa/references/source-of-truth-rules.md", "qa/references/source-of-truth-rules.md", 1321, "qa", virtualfs.KindReference},

		// E. Phân làn Supervisor (5 tài sản)
		{"ram://supervisor/SKILL.md", "supervisor/SKILL.md", 4713, "supervisor", virtualfs.KindSkill},
		{"ram://supervisor/references/claim-and-authority.md", "supervisor/references/claim-and-authority.md", 2536, "supervisor", virtualfs.KindReference},
		{"ram://supervisor/references/evidence-and-source-inspection.md", "supervisor/references/evidence-and-source-inspection.md", 2736, "supervisor", virtualfs.KindReference},
		{"ram://supervisor/references/invariant-and-resubmission.md", "supervisor/references/invariant-and-resubmission.md", 2004, "supervisor", virtualfs.KindReference},
		{"ram://supervisor/references/reporting.md", "supervisor/references/reporting.md", 4207, "supervisor", virtualfs.KindReference},

		// F. Phân làn Planner (5 tài sản)
		{"ram://planner/SKILL.md", "planner/SKILL.md", 10499, "planner", virtualfs.KindSkill},
		{"ram://strategic-planner/templates/actual-status.template.md", "planner/templates/actual-status.template.md", 7581, "strategic-planner", virtualfs.KindTemplate},
		{"ram://strategic-planner/templates/benchmark.template.md", "planner/templates/benchmark.template.md", 2547, "strategic-planner", virtualfs.KindTemplate},
		{"ram://strategic-planner/templates/evidence.template.md", "planner/templates/evidence.template.md", 2970, "strategic-planner", virtualfs.KindTemplate},
		{"ram://strategic-planner/templates/plan.template.md", "planner/templates/plan.template.md", 15479, "strategic-planner", virtualfs.KindTemplate},
	}

	if len(canonicalURIs) != 39 {
		t.Fatalf("expected exactly 39 canonical URIs in test table, got %d", len(canonicalURIs))
	}

	for _, item := range canonicalURIs {
		t.Run(item.uri, func(t *testing.T) {
			entry, err := tree.FindByURI(item.uri)
			if err != nil {
				t.Fatalf("FindByURI(%q) failed: %v", item.uri, err)
			}
			if entry == nil {
				t.Fatalf("FindByURI(%q) returned nil entry", item.uri)
			}

			if entry.Kind != item.kind {
				t.Errorf("kind mismatch for %s: got %s, want %s", item.uri, entry.Kind, item.kind)
			}

			if entry.ByteSize() != item.expectedSize {
				t.Errorf("size mismatch for %s: got %d, want %d", item.uri, entry.ByteSize(), item.expectedSize)
			}

			diskBytes, readErr := os.ReadFile(filepath.Join(assetsRelPath, item.diskPath))
			if readErr != nil {
				t.Fatalf("failed to read disk file %s: %v", item.diskPath, readErr)
			}

			diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
			if entry.SHA256() != diskSHA {
				t.Errorf("SHA-256 mismatch for %s: entry=%s disk=%s", item.uri, entry.SHA256(), diskSHA)
			}

			if entry.String() != string(diskBytes) {
				t.Errorf("content mismatch for %s", item.uri)
			}
		})
	}
}

// TestAllAliasRAMURIs kiểm tra tất cả các Alias RAM URIs phân giải chính xác đến đúng tài sản RAM
func TestAllAliasRAMURIs(t *testing.T) {
	tree := initTestTree(t)

	aliasCases := []struct {
		aliasURI     string
		targetDisk   string
		expectedSize int
	}{
		// CEO Aliases
		{"ram://ceo-plugin/SKILL.md", "ceo/SKILL.md", 13294},
		{"ram://anvien-ceo-plugin/SKILL.md", "ceo/SKILL.md", 13294},
		{"ram://ceo-plugin/references/authority-and-command.md", "ceo/references/authority-and-command.md", 3655},
		{"ram://anvien-ceo-plugin/references/timer-rules.md", "ceo/references/timer-rules.md", 17598},
		{"ram://ceo-plugin/references/Template-Prompt-for-Opening-a-Session.md", "ceo/references/Template-Prompt-for-Opening-a-Session.md", 9395},

		// Architect Aliases
		{"ram://architect/SKILL.md", "system-architect/SKILL.md", 7911},
		{"ram://architect/references/architect-review-coordination.md", "system-architect/references/architect-review-coordination.md", 3505},
		{"ram://architect/references/mode-0-interactive-architecture-discussion.md", "system-architect/references/mode-0-interactive-architecture-discussion.md", 6653},
		{"ram://architect/references/mode-1-spec-authoring-and-architecture.md", "system-architect/references/mode-1-spec-authoring-and-architecture.md", 11762},
		{"ram://architect/references/mode-2-execution-planning-and-jobs.md", "system-architect/references/mode-2-execution-planning-and-jobs.md", 9891},
		{"ram://architect/references/reporting-and-artifact-lifecycle.md", "system-architect/references/reporting-and-artifact-lifecycle.md", 2280},

		// Planner Aliases
		{"ram://strategic-planner/SKILL.md", "planner/SKILL.md", 10499},
		{"ram://mechanical-planner/SKILL.md", "planner/SKILL.md", 10499},
		{"ram://planner/templates/actual-status.template.md", "planner/templates/actual-status.template.md", 7581},
		{"ram://planner/templates/benchmark.template.md", "planner/templates/benchmark.template.md", 2547},
		{"ram://planner/templates/evidence.template.md", "planner/templates/evidence.template.md", 2970},
		{"ram://planner/templates/plan.template.md", "planner/templates/plan.template.md", 15479},
		{"ram://mechanical-planner/templates/actual-status.template.md", "planner/templates/actual-status.template.md", 7581},
		{"ram://mechanical-planner/templates/benchmark.template.md", "planner/templates/benchmark.template.md", 2547},
		{"ram://mechanical-planner/templates/evidence.template.md", "planner/templates/evidence.template.md", 2970},
		{"ram://mechanical-planner/templates/plan.template.md", "planner/templates/plan.template.md", 15479},

		// Stripped Extension Aliases (without .md)
		{"ram://ceo/SKILL", "ceo/SKILL.md", 13294},
		{"ram://architect/SKILL", "system-architect/SKILL.md", 7911},
		{"ram://strategic-planner/templates/plan.template", "planner/templates/plan.template.md", 15479},
		{"ram://planner/templates/plan.template", "planner/templates/plan.template.md", 15479},
		{"ram://mechanical-planner/templates/plan.template", "planner/templates/plan.template.md", 15479},
	}

	for _, tc := range aliasCases {
		t.Run(tc.aliasURI, func(t *testing.T) {
			entry, err := tree.FindByURI(tc.aliasURI)
			if err != nil {
				t.Fatalf("FindByURI(%q) failed: %v", tc.aliasURI, err)
			}
			if entry == nil {
				t.Fatalf("FindByURI(%q) returned nil", tc.aliasURI)
			}

			if entry.ByteSize() != tc.expectedSize {
				t.Errorf("expected size %d, got %d", tc.expectedSize, entry.ByteSize())
			}

			diskBytes, readErr := os.ReadFile(filepath.Join(assetsRelPath, tc.targetDisk))
			if readErr != nil {
				t.Fatalf("failed to read disk file %s: %v", tc.targetDisk, readErr)
			}

			diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
			if entry.SHA256() != diskSHA {
				t.Errorf("SHA-256 mismatch for %s: entry=%s disk=%s", tc.aliasURI, entry.SHA256(), diskSHA)
			}
		})
	}
}

// Test8CanonicalRoles_Matrix xác nhận cả 8 vai trò vận hành chính thức lấy được SKILL.md nguyên bản
func Test8CanonicalRoles_Matrix(t *testing.T) {
	tree := initTestTree(t)

	roles := []struct {
		role         string
		expectedPath string
		expectedSize int
	}{
		// 8 Canonical Roles
		{"ceo", "ceo/SKILL.md", 13294},
		{"system-architect", "system-architect/SKILL.md", 7911},
		{"architect", "system-architect/SKILL.md", 7911},
		{"coder", "coder/SKILL.md", 16647},
		{"qa", "qa/SKILL.md", 5922},
		{"supervisor", "supervisor/SKILL.md", 4713},
		{"mechanical-planner", "planner/SKILL.md", 10499},
		{"strategic-planner", "planner/SKILL.md", 10499},

		// Canonical Aliases
		{"ceo-plugin", "ceo/SKILL.md", 13294},
		{"anvien-ceo-plugin", "ceo/SKILL.md", 13294},
		{"planner", "planner/SKILL.md", 10499},
		{"system_architect", "system-architect/SKILL.md", 7911},
		{"mechanical_planner", "planner/SKILL.md", 10499},
		{"strategic_planner", "planner/SKILL.md", 10499},
	}

	for _, r := range roles {
		t.Run(r.role, func(t *testing.T) {
			entry, err := tree.GetSkill(r.role)
			if err != nil {
				t.Fatalf("GetSkill(%q) returned error: %v", r.role, err)
			}

			if entry.Kind != virtualfs.KindSkill {
				t.Errorf("expected KindSkill, got %q", entry.Kind)
			}

			if entry.ByteSize() != r.expectedSize {
				t.Errorf("expected size %d, got %d", r.expectedSize, entry.ByteSize())
			}

			diskBytes, readErr := os.ReadFile(filepath.Join(assetsRelPath, r.expectedPath))
			if readErr != nil {
				t.Fatalf("failed to read disk file: %v", readErr)
			}

			diskSHA := hex.EncodeToString(func() []byte { h := sha256.Sum256(diskBytes); return h[:] }())
			if entry.SHA256() != diskSHA {
				t.Errorf("SHA-256 mismatch for %s: entry=%s disk=%s", r.role, entry.SHA256(), diskSHA)
			}

			if entry.String() != string(diskBytes) {
				t.Errorf("content string mismatch for %s", r.role)
			}
		})
	}
}

// TestZeroDiskWritesRuntime xác nhận 100% không có bất kỳ thao tác ghi đĩa nào lúc runtime (Zero Disk Footprint)
func TestZeroDiskWritesRuntime(t *testing.T) {
	// 1. Quét toàn bộ thư mục assets, ghi nhận timestamp và dung lượng trước khi chạy
	type fileMeta struct {
		modTime time.Time
		size    int64
	}
	beforeMap := make(map[string]fileMeta)

	err := filepath.Walk(assetsRelPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			beforeMap[p] = fileMeta{modTime: info.ModTime(), size: info.Size()}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to inspect assets directory before run: %v", err)
	}

	if len(beforeMap) != 39 {
		t.Fatalf("expected 39 assets on disk before run, found %d", len(beforeMap))
	}

	// 2. Thực thi toàn bộ chu trình runtime: Khởi tạo cây, tra cứu 8 roles, tra cứu 38 URIs, tra cứu references
	fsys := os.DirFS(assetsRelPath)
	tree, err := virtualfs.NewTreeFromFS(fsys)
	if err != nil {
		t.Fatalf("NewTreeFromFS failed: %v", err)
	}

	for _, role := range []string{"ceo", "system-architect", "architect", "coder", "qa", "supervisor", "mechanical-planner", "strategic-planner"} {
		_, _ = tree.GetSkill(role)
	}
	_, _ = tree.GetReference("strategic-planner", "plan-template")
	_, _ = tree.GetReference("ceo", "Template-Prompt-for-Opening-a-Session")
	_, _ = tree.GetReference("architect", "mode-1-spec-authoring-and-architecture")
	for _, entry := range tree.ListEntries() {
		_, _ = tree.FindByURI(entry.URI)
	}

	// 3. Quét lại thư mục assets sau khi thực thi
	afterMap := make(map[string]fileMeta)
	err = filepath.Walk(assetsRelPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			afterMap[p] = fileMeta{modTime: info.ModTime(), size: info.Size()}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to inspect assets directory after run: %v", err)
	}

	if len(afterMap) != len(beforeMap) {
		t.Errorf("file count changed: before=%d, after=%d (disk was modified!)", len(beforeMap), len(afterMap))
	}

	for path, metaBefore := range beforeMap {
		metaAfter, ok := afterMap[path]
		if !ok {
			t.Errorf("file disappeared: %s", path)
			continue
		}
		if !metaAfter.modTime.Equal(metaBefore.modTime) {
			t.Errorf("file modTime changed for %s: before=%v, after=%v (disk write detected!)", path, metaBefore.modTime, metaAfter.modTime)
		}
		if metaAfter.size != metaBefore.size {
			t.Errorf("file size changed for %s: before=%d, after=%d (disk write detected!)", path, metaBefore.size, metaAfter.size)
		}
	}
}
func TestReferencePrefixesAndCollisions(t *testing.T) {
	tree, err := virtualfs.NewTreeFromFS(os.DirFS(assetsRelPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"references/timer-rules.md", "./references/timer-rules.md", `references\timer-rules.md`} {
		e, err := tree.GetReference("ceo", ref)
		if err != nil || e.Name != "timer-rules" {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	for _, ref := range []string{"templates/timer-rules.md", "references/templates/timer-rules.md", "../timer-rules.md", "C:/timer-rules.md", "ram://ceo/references/timer-rules.md", "/timer-rules.md"} {
		if _, err := tree.GetReference("ceo", ref); err == nil {
			t.Fatalf("accepted %s", ref)
		}
	}
	if _, err := tree.GetReference("mechanical-planner", "templates/plan.template.md"); err != nil {
		t.Fatal(err)
	}
	a := virtualfs.NewVirtualAssetTree()
	for i, name := range []string{"a-b", "a.b"} {
		err := a.AddEntry(&virtualfs.AssetEntry{Role: "ceo", Kind: virtualfs.KindReference, Name: name, URI: "ram://ceo/references/" + name + ".md", Path: name})
		if (i == 0 && err != nil) || (i == 1 && err == nil) {
			t.Fatalf("collision %d: %v", i, err)
		}
	}
	if a.Count() != 1 {
		t.Fatal("failed registration partially mutated tree")
	}
}
