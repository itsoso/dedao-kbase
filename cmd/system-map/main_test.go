package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSystemMapDiscoversStableArchitectureInventory(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cmd/kbase-server/main.go", `package main
func main() {}
`)
	writeFixture(t, root, "backend/app/kbase_http.go", `package app
import "net/http"
func Serve(path string) {}
func routes() {
	Serve("/api/books")
	http.HandleFunc("/health", nil)
}
`)
	writeFixture(t, root, "backend/app/source_sync.go", `package app
type SourceSyncRun struct { ID string }
const sourceOperation = "sync_articles"
func (a Adapter) Operations() []string { return []string{"sync_content"} }
type Adapter struct{}
`)

	first, err := GenerateSystemMap(root)
	if err != nil {
		t.Fatalf("GenerateSystemMap() error = %v", err)
	}
	second, err := GenerateSystemMap(root)
	if err != nil {
		t.Fatalf("GenerateSystemMap() second error = %v", err)
	}

	if first.SchemaVersion != "1" {
		t.Fatalf("schema version = %q, want 1", first.SchemaVersion)
	}
	if len(first.Commands) != 1 || first.Commands[0].Name != "kbase-server" || first.Commands[0].Path != "cmd/kbase-server/main.go" {
		t.Fatalf("commands = %#v", first.Commands)
	}
	assertHasRoute(t, first.HTTPRoutes, "/api/books", true)
	assertHasRoute(t, first.HTTPRoutes, "/health", false)
	assertHasOperation(t, first.SourceOperations, "sync_articles")
	assertHasOperation(t, first.SourceOperations, "sync_content")
	assertHasDurableObject(t, first.DurableObjects, "SourceSyncRun")

	encodedFirst, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	encodedSecond, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedFirst) != string(encodedSecond) {
		t.Fatalf("generated map is not stable:\nfirst=%s\nsecond=%s", encodedFirst, encodedSecond)
	}
	for _, command := range first.Commands {
		if filepath.IsAbs(command.Path) {
			t.Fatalf("command path is absolute: %s", command.Path)
		}
	}
}

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertHasRoute(t *testing.T, routes []HTTPRoute, path string, authenticated bool) {
	t.Helper()
	for _, route := range routes {
		if route.Path == path && route.Authenticated == authenticated {
			return
		}
	}
	t.Fatalf("missing route path=%s authenticated=%v in %#v", path, authenticated, routes)
}

func assertHasOperation(t *testing.T, operations []SourceOperation, name string) {
	t.Helper()
	for _, operation := range operations {
		if operation.Name == name {
			return
		}
	}
	t.Fatalf("missing operation %s in %#v", name, operations)
}

func assertHasDurableObject(t *testing.T, objects []DurableObject, name string) {
	t.Helper()
	for _, object := range objects {
		if object.Name == name {
			return
		}
	}
	t.Fatalf("missing durable object %s in %#v", name, objects)
}
