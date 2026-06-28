package config

import (
	"path/filepath"
	"testing"
)

func TestSetActiveUserRefreshesServiceCache(t *testing.T) {
	cfg := New(filepath.Join(t.TempDir(), "config.json"))
	cfg.activeUser = &Dedao{
		User: User{UIDHazy: "old-user"},
	}

	oldService := cfg.ActiveUserService()
	cfg.setActiveUser(&Dedao{
		User: User{UIDHazy: "new-user"},
	})
	newService := cfg.ActiveUserService()

	if newService == oldService {
		t.Fatal("ActiveUserService reused stale service after active user changed")
	}
}
