package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const sourceAssetMaxBytes = 8 << 20

var sourceAssetHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SourceAssetEnvelope struct {
	SourceItemKey string `json:"source_item_key"`
	SourceURL     string `json:"source_url"`
	SHA256        string `json:"sha256"`
	ContentType   string `json:"content_type"`
	Data          []byte `json:"-"`
}
type SourceAssetReference struct {
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Path        string `json:"-"`
}
type SourceAssetStore struct{ root string }

func NewSourceAssetStore(root string) (*SourceAssetStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("source asset root is required")
	}
	root = filepath.Join(root, "source_assets")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	return &SourceAssetStore{root: root}, nil
}
func (s *SourceAssetStore) Save(_ context.Context, input SourceAssetEnvelope) (SourceAssetReference, error) {
	hash := strings.ToLower(strings.TrimSpace(input.SHA256))
	if !sourceAssetHashPattern.MatchString(hash) {
		return SourceAssetReference{}, fmt.Errorf("invalid source asset hash")
	}
	if len(input.Data) > sourceAssetMaxBytes {
		return SourceAssetReference{}, fmt.Errorf("source asset exceeds byte limit")
	}
	sum := sha256.Sum256(input.Data)
	if hex.EncodeToString(sum[:]) != hash {
		return SourceAssetReference{}, fmt.Errorf("source asset hash mismatch")
	}
	detected := http.DetectContentType(input.Data)
	if !allowedSourceAssetType(detected) || !sameSourceAssetType(input.ContentType, detected) {
		return SourceAssetReference{}, fmt.Errorf("unsupported source asset content type")
	}
	path := filepath.Join(s.root, hash)
	if info, err := os.Stat(path); err == nil {
		return SourceAssetReference{SHA256: hash, ContentType: detected, Size: info.Size(), Path: path}, nil
	}
	temp, err := os.CreateTemp(s.root, ".asset-*")
	if err != nil {
		return SourceAssetReference{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(input.Data)
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tempPath, path)
	}
	if err != nil {
		return SourceAssetReference{}, err
	}
	return SourceAssetReference{SHA256: hash, ContentType: detected, Size: int64(len(input.Data)), Path: path}, nil
}
func (s *SourceAssetStore) Open(hash string) (*os.File, error) {
	if !sourceAssetHashPattern.MatchString(hash) {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(s.root, hash))
}
func allowedSourceAssetType(value string) bool {
	return strings.HasPrefix(value, "image/jpeg") || strings.HasPrefix(value, "image/png") || strings.HasPrefix(value, "image/gif") || strings.HasPrefix(value, "image/webp")
}
func sameSourceAssetType(claimed, detected string) bool {
	claimed = strings.TrimSpace(strings.Split(claimed, ";")[0])
	detected = strings.Split(detected, ";")[0]
	return claimed == "" || claimed == detected
}
func readBoundedAsset(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, sourceAssetMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > sourceAssetMaxBytes {
		return nil, fmt.Errorf("source asset exceeds byte limit")
	}
	return data, nil
}
