package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSourceAssetStorePersistsHashAddressedPrivateFile(t *testing.T) {
	store, err := NewSourceAssetStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("\x89PNG\r\n\x1a\nsanitized")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ref, err := store.Save(context.Background(), SourceAssetEnvelope{SourceItemKey: "item-1", SourceURL: "https://mmbiz.qpic.cn/image", SHA256: hash, ContentType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if ref.SHA256 != hash {
		t.Fatalf("ref=%#v", ref)
	}
	info, err := os.Stat(ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	ref2, err := store.Save(context.Background(), SourceAssetEnvelope{SourceItemKey: "item-1", SourceURL: "https://mmbiz.qpic.cn/image", SHA256: hash, ContentType: "image/png", Data: data})
	if err != nil || ref2.Path != ref.Path {
		t.Fatalf("duplicate=%#v err=%v", ref2, err)
	}
}
func TestSourceAssetStoreRejectsInvalidHashTypeAndSize(t *testing.T) {
	store, _ := NewSourceAssetStore(t.TempDir())
	cases := []SourceAssetEnvelope{{SHA256: "../bad", ContentType: "image/png", Data: []byte("x")}, {SHA256: hex.EncodeToString(make([]byte, 32)), ContentType: "text/html", Data: []byte("x")}, {SHA256: hex.EncodeToString(make([]byte, 32)), ContentType: "image/png", Data: make([]byte, (8<<20)+1)}}
	for _, input := range cases {
		if _, err := store.Save(context.Background(), input); err == nil {
			t.Fatalf("accepted invalid asset %#v", input)
		}
	}
}
func TestSourceAssetStoreReturnsNotFound(t *testing.T) {
	store, _ := NewSourceAssetStore(t.TempDir())
	_, err := store.Open(strings.Repeat("a", 64))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err=%v", err)
	}
}
