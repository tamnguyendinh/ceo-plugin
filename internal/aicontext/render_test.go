package aicontext

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpDiscoveryRenderAndWorkspaceSync(t *testing.T) {
	block := testBlock()
	beforeHelp := strings.Index(block, "### How to use ceo-plugin:")
	help := strings.Index(block, "### Command Help & Discovery")
	init := strings.Index(block, "### Workspace Initialization")
	if beforeHelp < 0 || help <= beforeHelp || init <= help {
		t.Fatal("help discovery is not between invocation and initialization")
	}
	for _, want := range []string{"`ceo-plugin help`", "`--help`", "`-h`", "`ceo-plugin help <lane>`", "`ceo-plugin <lane> --help`", "`ceo-plugin init --help`", "`ceo-plugin help init`"} {
		if !strings.Contains(block[help:init], want) {
			t.Fatalf("missing help syntax %s", want)
		}
	}
	root := t.TempDir()
	prefix, suffix := []byte("owner prefix  \r\n"), []byte("\r\nowner suffix  \r\n")
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		old := append(bytes.Clone(prefix), []byte(StartMarker+"\r\nold commands\r\n"+EndMarker)...)
		old = append(old, suffix...)
		if err := os.WriteFile(filepath.Join(root, name), old, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"updated", "unchanged"} {
		r, err := SyncWorkspace(context.Background(), root, block)
		if err != nil || r.Status != status {
			t.Fatalf("sync %s: %+v %v", status, r, err)
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || !bytes.HasPrefix(raw, prefix) || !bytes.HasSuffix(raw, suffix) || bytes.Count(raw, []byte(StartMarker)) != 1 || bytes.Count(raw, []byte(EndMarker)) != 1 || bytes.Count(raw, []byte("### Command Help & Discovery")) != 1 {
				t.Fatalf("guide update damaged owner text or markers: %s %v", name, err)
			}
		}
	}
}
