package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathOwnershipIsExact(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".ceo-plugin", "bin")
	legacy := filepath.Join(home, ".anvien-ceo-plugin", "bin")
	for _, p := range []string{target, legacy} {
		if !isLegacyOrDuplicate(p, target) {
			t.Fatalf("owned path not found: %s", p)
		}
	}
	for _, p := range []string{"", `C:\tools\.anvien-ceo-plugin-backup\bin`, legacy + "-tools", "  %OTHER_TOOLS%  "} {
		if isLegacyOrDuplicate(p, target) {
			t.Fatalf("unowned path selected: %s", p)
		}
	}
}
