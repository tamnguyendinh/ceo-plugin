package aicontext

import (
	"bytes"
	"fmt"
	"strings"
)

// UpsertManagedBlock only replaces a uniquely identified, top-level managed block.
func UpsertManagedBlock(raw []byte, exists bool, block []byte) ([]byte, error) {
	nl := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		nl = "\r\n"
	}
	block = bytes.ReplaceAll(bytes.ReplaceAll(block, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte(nl))
	type marker struct{ offset, end, kind int }
	var markers []marker
	offset, fenceLen := 0, 0
	var fence byte
	for _, line := range bytes.SplitAfter(raw, []byte("\n")) {
		body := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		bom := 0
		if offset == 0 && strings.HasPrefix(body, "\ufeff") {
			body = strings.TrimPrefix(body, "\ufeff")
			bom = 3
		}
		trim := strings.TrimLeft(body, " ")
		indent := len(body) - len(trim)
		if indent < 4 && len(trim) > 0 && (trim[0] == '`' || trim[0] == '~') {
			n := 0
			for n < len(trim) && trim[n] == trim[0] {
				n++
			}
			if fenceLen == 0 && n >= 3 {
				fence, fenceLen = trim[0], n
			} else if fenceLen > 0 && trim[0] == fence && n >= fenceLen && strings.TrimSpace(trim[n:]) == "" {
				fenceLen = 0
			}
		} else if fenceLen == 0 && indent < 4 && !strings.HasPrefix(body, "\t") {
			for k, value := range []string{StartMarker, EndMarker, OldStartMarker, OldEndMarker} {
				if strings.TrimSpace(body) == value && body != value {
					return nil, fmt.Errorf("ambiguous_managed_block: marker line has unexpected whitespace")
				}
				if body == value {
					markers = append(markers, marker{offset + bom, offset + bom + len(body), k})
				}
			}
		}
		offset += len(line)
	}
	if fenceLen > 0 {
		return nil, fmt.Errorf("ambiguous_managed_block: unclosed code fence")
	}
	if len(markers) == 0 {
		out := bytes.Clone(raw)
		if len(raw) > 0 && !bytes.Equal(raw, []byte("\xef\xbb\xbf")) {
			if !bytes.HasSuffix(raw, []byte("\n")) {
				out = append(out, nl...)
			}
			out = append(out, nl...)
		}
		out = append(out, block...)
		return append(out, nl...), nil
	}
	if len(markers) != 2 || !((markers[0].kind == 0 && markers[1].kind == 1) || (markers[0].kind == 2 && markers[1].kind == 3)) {
		return nil, fmt.Errorf("ambiguous_managed_block: expected one ordered marker pair")
	}
	out := bytes.Clone(raw[:markers[0].offset])
	out = append(out, block...)
	return append(out, raw[markers[1].end:]...), nil
}
