package installstate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ceo-plugin/internal/fileupdate"
)

const IdentityFile = ".installation-id"

func ReadIdentity(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, IdentityFile))
	if os.IsNotExist(err) {
		return "legacy", nil
	}
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) != 16 {
		return "", fmt.Errorf("invalid installation identity")
	}
	return s, nil
}

func NewIdentity(root string) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	_, err := fileupdate.Update(context.Background(), filepath.Join(root, IdentityFile), func(_ []byte, _ bool) ([]byte, error) { return []byte(hex.EncodeToString(nonce[:]) + "\n"), nil })
	return err
}
