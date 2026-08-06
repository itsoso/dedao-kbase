//go:build darwin || linux

package sourceagentupdate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "updater.json")
	config := Config{
		Schema: ConfigSchemaV1, KBaseURL: "https://kbase.example.invalid/base", AgentID: "agent-a",
	}
	if err := SaveConfig(path, config); err != nil {
		t.Fatalf("SaveConfig() error=%v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%v", info.Mode())
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error=%v", err)
	}
	if loaded != config {
		t.Fatalf("loaded=%#v want=%#v", loaded, config)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid/base","agent_id":"agent-a"}` + "\n"
	if string(payload) != want {
		t.Fatalf("payload=%q", payload)
	}
}

func TestProtectedConfigRejectsInvalidProtocol(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "updater.json")
	valid := `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid","agent_id":"agent-a"}`
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown", raw: strings.TrimSuffix(valid, "}") + `,"extra":true}`},
		{name: "duplicate", raw: `{"schema":"source-agent-updater-config.v1","schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid","agent_id":"agent-a"}`},
		{name: "trailing", raw: valid + ` {}`},
		{name: "credential url", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"https://user:pass@kbase.example.invalid","agent_id":"agent-a"}`},
		{name: "unnormalized url", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid/","agent_id":"agent-a"}`},
		{name: "query", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid?mode=admin","agent_id":"agent-a"}`},
		{name: "path traversal", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid/base/../admin","agent_id":"agent-a"}`},
		{name: "remote cleartext", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"http://kbase.example.invalid","agent_id":"agent-a"}`},
		{name: "secret field", raw: strings.TrimSuffix(valid, "}") + `,"token":"synthetic"}`},
		{name: "path field", raw: strings.TrimSuffix(valid, "}") + `,"install_path":"relative"}`},
		{name: "label field", raw: strings.TrimSuffix(valid, "}") + `,"launch_label":"worker"}`},
		{name: "environment field", raw: strings.TrimSuffix(valid, "}") + `,"environment":{"MODE":"test"}}`},
		{name: "agent whitespace", raw: `{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid","agent_id":" agent-a "}`},
		{name: "oversized", raw: strings.Repeat("x", configMaximumBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("LoadConfig() error=%v", err)
			}
		})
	}
}

func TestProtectedConfigRejectsUnsafeStorage(t *testing.T) {
	valid := Config{Schema: ConfigSchemaV1, KBaseURL: "https://kbase.example.invalid", AgentID: "agent-a"}
	t.Run("relative path", func(t *testing.T) {
		if err := SaveConfig("updater.json", valid); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("SaveConfig() error=%v", err)
		}
	})
	t.Run("parent mode", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := SaveConfig(filepath.Join(root, "updater.json"), valid); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("SaveConfig() error=%v", err)
		}
	})
	t.Run("file mode", func(t *testing.T) {
		root := privateDirectory(t)
		path := filepath.Join(root, "updater.json")
		if err := os.WriteFile(path, []byte(`{"schema":"source-agent-updater-config.v1","kbase_url":"https://kbase.example.invalid","agent_id":"agent-a"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("LoadConfig() error=%v", err)
		}
	})
	t.Run("symlink file", func(t *testing.T) {
		root := privateDirectory(t)
		target := filepath.Join(root, "target.json")
		if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "updater.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(link); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("LoadConfig() error=%v", err)
		}
		if err := SaveConfig(link, valid); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("SaveConfig() error=%v", err)
		}
	})
	t.Run("symlink parent", func(t *testing.T) {
		outer := t.TempDir()
		realParent := filepath.Join(outer, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}
		linkParent := filepath.Join(outer, "link")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Fatal(err)
		}
		if err := SaveConfig(filepath.Join(linkParent, "updater.json"), valid); !errors.Is(err, ErrUnsafeConfigStorage) {
			t.Fatalf("SaveConfig() error=%v", err)
		}
	})
}

func TestProtectedConfigInvalidSavePreservesCurrentFile(t *testing.T) {
	root := privateDirectory(t)
	path := filepath.Join(root, "updater.json")
	valid := Config{Schema: ConfigSchemaV1, KBaseURL: "https://kbase.example.invalid", AgentID: "agent-a"}
	if err := SaveConfig(path, valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.KBaseURL = "https://user:pass@kbase.example.invalid"
	if err := SaveConfig(path, invalid); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SaveConfig() error=%v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil || loaded != valid {
		t.Fatalf("loaded=%#v error=%v", loaded, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "updater.json" {
		t.Fatalf("unexpected files=%v", entries)
	}
}

func privateDirectory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
