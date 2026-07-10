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
	webDir := flag.String("web-dir", defaultWebDir(), "static web UI directory")
	authToken := flag.String("auth-token", os.Getenv("KBASE_AUTH_TOKEN"), "bearer token for /api/* routes")
	sourceAgentToken := flag.String("source-agent-token", defaultSourceAgentToken(), "bearer token for /api/source-agent/* routes")
	flag.Parse()
	sourceSync, err := app.NewSourceSyncStore(*root)
	if err != nil {
		log.Fatalf("initialize source sync store: %v", err)
	}
	defer sourceSync.Close()

	handler := app.NewKBaseHTTPHandler(app.KBaseHTTPConfig{
		Store:              app.NewBookKnowledgeStore(*root),
		AuthToken:          *authToken,
		SystemKBExportPath: *exportPath,
		StaticDir:          *webDir,
		WeChat:             app.NewWeChatSourceService(app.WeChatSourceConfigFromEnv()),
		WCPlus:             app.NewWCPlusSourceService(app.WCPlusSourceConfigFromEnv()),
		SourceSync:         sourceSync,
		SourceAgentToken:   *sourceAgentToken,
	})

	log.Printf("dedao kbase server listening on %s", *addr)
	log.Printf("book knowledge root: %s", *root)
	log.Printf("system kb export: %s", *exportPath)
	if strings.TrimSpace(*webDir) != "" {
		log.Printf("web dir: %s", *webDir)
	}
	if strings.TrimSpace(*authToken) == "" {
		log.Printf("warning: KBASE_AUTH_TOKEN is empty; /api/* routes will reject requests")
	}
	if strings.TrimSpace(*sourceAgentToken) == "" {
		log.Printf("source agent API disabled until KBASE_SOURCE_AGENT_TOKEN is configured")
	} else {
		log.Printf("source agent API enabled")
	}
	if strings.TrimSpace(os.Getenv("WECHAT_MP_TOKEN")) == "" || strings.TrimSpace(os.Getenv("WECHAT_MP_COOKIE")) == "" {
		log.Printf("wechat source: official account search/list disabled until WECHAT_MP_TOKEN and WECHAT_MP_COOKIE are configured")
	}
	if !wcplusBaseURLConfiguredFromEnv() {
		log.Printf("wcplus source: using default local API base http://127.0.0.1:5001")
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
	if root := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO_DIR")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	if root := strings.TrimSpace(os.Getenv("DEDAO_WIKI_REPO")); root != "" {
		return filepath.Join(root, "artifacts", "system_kb_export.json")
	}
	return filepath.Join("artifacts", "system_kb_export.json")
}

func defaultWebDir() string {
	if value := strings.TrimSpace(os.Getenv("KBASE_WEB_DIR")); value != "" {
		return value
	}
	for _, candidate := range []string{
		filepath.Join("frontend-web", "dist"),
		"frontend-web",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func wcplusBaseURLConfiguredFromEnv() bool {
	return strings.TrimSpace(os.Getenv("WCPLUS_BASE_URL")) != "" || strings.TrimSpace(os.Getenv("WCPLUSPRO_BASE_URL")) != ""
}

func defaultSourceAgentToken() string {
	return strings.TrimSpace(os.Getenv("KBASE_SOURCE_AGENT_TOKEN"))
}
