package app

import (
	"context"
	"errors"
	"testing"
)

func TestSourceMemorySecretStoreLifecycle(t *testing.T) {
	store := NewMemorySourceSecretStore()
	if err := store.Save(context.Background(), "wechat-session", []byte("private-value")); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), "wechat-session")
	if err != nil || string(got) != "private-value" {
		t.Fatalf("load=%q err=%v", got, err)
	}
	if err := store.Delete(context.Background(), "wechat-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "wechat-session"); !errors.Is(err, ErrSourceSecretNotFound) {
		t.Fatalf("load deleted err=%v", err)
	}
}

func TestSourceMemorySecretStoreRejectsInvalidKey(t *testing.T) {
	store := NewMemorySourceSecretStore()
	if err := store.Save(context.Background(), "../secret", []byte("value")); err == nil {
		t.Fatal("invalid key accepted")
	}
}
