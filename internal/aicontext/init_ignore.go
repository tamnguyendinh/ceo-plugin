package aicontext

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"ceo-plugin/internal/fileupdate"
)

func validateInitIgnore(raw []byte) error {
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return fmt.Errorf("unsupported_gitignore_encoding: expected UTF-8")
	}
	return nil
}

func ensureInitIgnore(ctx context.Context, root string, g initGit, guard func() error, update initUpdate) (FileResult, error) {
	v, err := update(ctx, filepath.Join(root, ".gitignore"), func(raw []byte, exists bool) ([]byte, error) {
		if err := guard(); err != nil {
			return nil, err
		}
		if err := validateInitIgnore(raw); err != nil {
			return nil, err
		}
		var missing []string
		for _, name := range initContextNames {
			ok, err := g.ignored(ctx, root, name)
			if err != nil {
				return nil, err
			}
			if !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			return raw, nil
		}
		nl := "\n"
		if at := bytes.IndexByte(raw, '\n'); at > 0 && raw[at-1] == '\r' {
			nl = "\r\n"
		}
		next := bytes.Clone(raw)
		if len(next) > 0 && !bytes.Equal(next, []byte{0xef, 0xbb, 0xbf}) && !bytes.HasSuffix(next, []byte("\n")) {
			next = append(next, nl...)
		}
		next = append(next, ("# AI Context (ceo-plugin)" + nl)...)
		for _, name := range missing {
			next = append(next, ("/" + name + nl)...)
		}
		return next, nil
	})
	result := FileResult{Result: v}
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

type initUpdate func(context.Context, string, func([]byte, bool) ([]byte, error)) (fileupdate.Result, error)
