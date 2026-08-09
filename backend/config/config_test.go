package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSetActiveUserRefreshesService(t *testing.T) {
	configs := &ConfigsData{}
	configs.setActiveUser(&Dedao{User: User{UIDHazy: "first"}})
	firstService := configs.ActiveUserService()

	configs.setActiveUser(&Dedao{User: User{UIDHazy: "second"}})
	secondService := configs.ActiveUserService()
	if firstService == secondService {
		t.Fatal("active user change reused the previous user's service")
	}
}

func TestLazyConfigLoadsAndSavesExistingDesktopFormatWithoutDeadlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := []byte(`{"AcitveUID":"user-1","Users":[{"uid_hazy":"user-1","name":"Reader","token":"test-token"}]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	configs := New(path)
	done := make(chan error, 1)
	go func() {
		active := configs.ActiveUser()
		if active == nil || active.UIDHazy != "user-1" || active.Name != "Reader" {
			done <- errors.New("legacy active user was not loaded")
			return
		}
		done <- configs.Save()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lazy config initialization deadlocked")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"AcitveUID": "user-1"`)) || !bytes.Contains(data, []byte(`"uid_hazy": "user-1"`)) {
		t.Fatalf("saved config is incompatible: %s", data)
	}
}

func TestLazyConfigMalformedFileKeepsDesktopAccessorsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"broken"`), 0o600); err != nil {
		t.Fatal(err)
	}
	configs := New(path)
	if active := configs.ActiveUser(); active == nil {
		t.Fatal("ActiveUser returned nil after lazy initialization failure")
	}
	if err := configs.Save(); err == nil {
		t.Fatal("Save hid the lazy initialization error")
	}
}

func TestActiveUserServiceConcurrentRefresh(t *testing.T) {
	configs := &ConfigsData{}
	configs.setActiveUser(&Dedao{User: User{UIDHazy: "initial"}})
	var wg sync.WaitGroup
	for index := 0; index < 50; index++ {
		wg.Add(2)
		go func(index int) {
			defer wg.Done()
			configs.setActiveUser(&Dedao{User: User{UIDHazy: "user-" + strconv.Itoa(index)}})
		}(index)
		go func() {
			defer wg.Done()
			_ = configs.ActiveUserService()
		}()
	}
	wg.Wait()
}
