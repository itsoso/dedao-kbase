package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yann0917/dedao-gui/backend/app"
)

func main() {
	addr := flag.String("addr", envDefault("KBASE_HTTP_ADDR", "127.0.0.1:8719"), "HTTP listen address")
	root := flag.String("root", envDefault("KBASE_BOOK_KNOWLEDGE_ROOT", app.DefaultBookKnowledgeRoot()), "book_knowledge root directory")
	exportPath := flag.String("system-kb-export", defaultSystemKBExportPath(), "system_kb_export.json path")
	authToken := flag.String("auth-token", os.Getenv("KBASE_AUTH_TOKEN"), "bearer token for /api/* routes")
	webDir := flag.String("web-dir", defaultKBaseWebDir(), "web UI static asset directory")
	flag.Parse()

	store := app.NewBookKnowledgeStore(*root)
	if count, err := store.FailRunningBookKnowledgeJobs("interrupted by kbase-server restart"); err != nil {
		log.Printf("failed to recover interrupted jobs: %v", err)
	} else if count > 0 {
		log.Printf("marked %d interrupted running jobs as failed", count)
	}

	handler := app.NewKBaseHTTPHandler(app.KBaseHTTPConfig{
		Store:              store,
		AuthToken:          *authToken,
		SystemKBExportPath: *exportPath,
		StaticDir:          *webDir,
	})

	log.Printf("dedao kbase server listening on %s", *addr)
	log.Printf("book knowledge root: %s", *root)
	log.Printf("system kb export: %s", *exportPath)
	if strings.TrimSpace(*webDir) != "" {
		log.Printf("web UI dir: %s", *webDir)
	}
	if strings.TrimSpace(*authToken) == "" {
		log.Printf("warning: KBASE_AUTH_TOKEN is empty; /api/* routes will reject requests")
	}
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	return fallback
}

func defaultSystemKBExportPath() string {
	if value := strings.TrimSpace(os.Getenv("KBASE_SYSTEM_KB_EXPORT_PATH")); value != "" {
		return value
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_KBASE_ROOT")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		return filepath.Join(cwd, "artifacts", "system_kb_export.json")
	}
	return filepath.Join(os.TempDir(), "dedao-kbase", "artifacts", "system_kb_export.json")
}

func defaultKBaseWebDir() string {
	if value := strings.TrimSpace(os.Getenv("KBASE_WEB_DIR")); value != "" {
		return value
	}
	candidate := filepath.Join("frontend-web", "dist")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}
