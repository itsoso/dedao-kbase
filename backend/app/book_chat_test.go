package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBookTokenPlanConfigFromEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"TOKENPLAN_API_KEY=sk-test-token",
		"TOKENPLAN_BASE_URL=https://token-plan.example.test/compatible-mode/v1",
		"TOKENPLAN_MODEL=MiniMax-M2.5",
	}, "\n")), 0600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "")
	t.Setenv("TOKENPLAN_API_KEY", "")
	t.Setenv("TOKENPLAN_BASE_URL", "")
	t.Setenv("TOKENPLAN_MODEL", "")
	t.Setenv("DEDAO_TOKENPLAN_ENV_FILE", envFile)

	cfg, err := LoadBookTokenPlanConfig()
	if err != nil {
		t.Fatalf("LoadBookTokenPlanConfig returned error: %v", err)
	}
	if cfg.APIKey != "sk-test-token" {
		t.Fatalf("APIKey = %q, want fake env key", cfg.APIKey)
	}
	if cfg.BaseURL != "https://token-plan.example.test/compatible-mode/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "MiniMax-M2.5" {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.Source != envFile {
		t.Fatalf("Source = %q, want env file path", cfg.Source)
	}
}

func TestDefaultTokenPlanEnvFilesDoNotContainPrivatePaths(t *testing.T) {
	privatePathToken := "/" + "Users" + "/"
	privateUserToken := "li" + "qiuhua"
	privateProjectToken := "health" + "-llm-driven"
	for _, path := range defaultTokenPlanEnvFiles {
		if strings.Contains(path, privatePathToken) || strings.Contains(path, privateUserToken) || strings.Contains(path, privateProjectToken) {
			t.Fatalf("default token plan env file leaks a private path: %q", path)
		}
	}
}

func TestBookKnowledgeChatBuildsGroundedPrompt(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "MiniMax-M2.5")
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "这本书强调先做趋势过滤，再做 MACD 背离规则。"}

	resp, err := BookKnowledgeChatWithClient(context.Background(), store, BookKnowledgeChatRequest{
		BookID:   "42",
		Mode:     "analysis",
		Question: "MACD 背离如何落成规则？",
		Model:    "qwen3.7-max",
	}, client)
	if err != nil {
		t.Fatalf("BookKnowledgeChatWithClient returned error: %v", err)
	}
	if resp.Answer != client.answer {
		t.Fatalf("Answer = %q, want fake answer", resp.Answer)
	}
	if resp.Model != "qwen3.7-max" {
		t.Fatalf("Model = %q, want request model", resp.Model)
	}
	if resp.ContextStats.Chapters != 1 || resp.ContextStats.Claims != 1 || resp.ContextStats.Chunks == 0 {
		t.Fatalf("ContextStats = %#v, want book context", resp.ContextStats)
	}
	if len(resp.Sources) == 0 {
		t.Fatalf("Sources = %#v, want source ids", resp.Sources)
	}
	if len(client.messages) != 2 {
		t.Fatalf("messages = %#v, want system and user messages", client.messages)
	}
	combined := client.messages[0].Content + "\n" + client.messages[1].Content
	for _, want := range []string{"42_量化分析_作者", "趋势过滤", "MACD 规则需要趋势过滤", "MACD 背离需要趋势过滤"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("prompt missing %q:\n%s", want, combined)
		}
	}
}

func TestBookKnowledgeChatCanonicalizesQwenDisplayLabel(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "answer"}

	response, err := BookKnowledgeChatWithClient(context.Background(), store, BookKnowledgeChatRequest{
		BookID: "42", Question: "总结", Model: "Qwen-3.7-Max",
	}, client)
	if err != nil {
		t.Fatalf("BookKnowledgeChatWithClient returned error: %v", err)
	}
	if client.cfg.Model != "qwen3.7-max" || response.Model != "qwen3.7-max" {
		t.Fatalf("models = client %q response %q, want qwen3.7-max", client.cfg.Model, response.Model)
	}
}

func TestContextKnowledgeChatBuildsGroundedPrompt(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	client := &fakeBookKnowledgeLLMClient{answer: "文章强调供给侧心态可以改善合作。"}

	response, err := ContextKnowledgeChatWithClient(context.Background(), ContextKnowledgeChatRequest{
		Title:           "供给侧心态",
		SourceType:      "dedao_course_article",
		Question:        "提炼这篇文章的方法论",
		Content:         strings.Repeat("合作需要先提供价值。", 80),
		Model:           "Qwen-3.7-Max",
		MaxContextChars: 40,
	}, client)
	if err != nil {
		t.Fatalf("ContextKnowledgeChatWithClient returned error: %v", err)
	}
	if response.Answer != client.answer || response.Model != "qwen3.7-max" {
		t.Fatalf("response = %#v", response)
	}
	if response.ContextStats.Chars > 40 || response.ContextStats.Chunks != 1 {
		t.Fatalf("ContextStats = %#v, want trimmed single context", response.ContextStats)
	}
	combined := client.messages[0].Content + "\n" + client.messages[1].Content
	for _, want := range []string{"dedao_course_article", "供给侧心态", "提炼这篇文章的方法论", "context:article"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("prompt missing %q:\n%s", want, combined)
		}
	}
}

func TestBookKnowledgeChatPersistsHistory(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	t.Setenv("DEDAO_TOKENPLAN_MODEL", "MiniMax-M2.5")
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	client := &fakeBookKnowledgeLLMClient{answer: "历史记录中的分析答案"}

	resp, err := BookKnowledgeChatWithClient(context.Background(), store, BookKnowledgeChatRequest{
		BookID:   "42",
		Mode:     "analysis",
		Question: "如何把这本书变成交易规则？",
		Model:    "qwen3.7-max",
	}, client)
	if err != nil {
		t.Fatalf("BookKnowledgeChatWithClient returned error: %v", err)
	}
	if strings.TrimSpace(resp.HistoryID) == "" {
		t.Fatalf("HistoryID is empty, want persisted chat id")
	}

	history, err := store.ListChatHistory("42", 20)
	if err != nil {
		t.Fatalf("ListChatHistory returned error: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	item := history[0]
	if item.ID != resp.HistoryID {
		t.Fatalf("history id = %q, want response history id %q", item.ID, resp.HistoryID)
	}
	if item.BookID != "42" || item.BookTitle == "" {
		t.Fatalf("history book fields = %#v, want book identity", item)
	}
	if item.Mode != "analysis" || item.Question != "如何把这本书变成交易规则？" || item.Model != "qwen3.7-max" {
		t.Fatalf("history request fields = %#v", item)
	}
	if item.Answer != "历史记录中的分析答案" {
		t.Fatalf("history answer = %q", item.Answer)
	}
	if len(item.Sources) == 0 {
		t.Fatalf("history sources = %#v, want persisted sources", item.Sources)
	}
	if item.ContextStats.Claims != resp.ContextStats.Claims || item.ContextStats.Chunks != resp.ContextStats.Chunks {
		t.Fatalf("history stats = %#v, response stats = %#v", item.ContextStats, resp.ContextStats)
	}
}

func TestTokenPlanChatClientUsesOpenAICompatibleRequest(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	var gotEnableThinking *bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var payload struct {
			Model          string                 `json:"model"`
			Messages       []BookKnowledgeMessage `json:"messages"`
			EnableThinking *bool                  `json:"enable_thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode request returned error: %v", err)
		}
		gotModel = payload.Model
		gotEnableThinking = payload.EnableThinking
		if len(payload.Messages) != 2 {
			t.Fatalf("messages = %#v, want 2", payload.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"测试回答"}}]}`))
	}))
	defer server.Close()

	client := NewTokenPlanChatClient(server.Client())
	enableThinking := false
	answer, err := client.Chat(context.Background(), BookTokenPlanConfig{
		APIKey:         "sk-test-token",
		BaseURL:        server.URL + "/compatible-mode/v1",
		Model:          "qwen3.7-max",
		EnableThinking: &enableThinking,
	}, []BookKnowledgeMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "user"},
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if answer != "测试回答" {
		t.Fatalf("answer = %q", answer)
	}
	if gotPath != "/compatible-mode/v1/chat/completions" {
		t.Fatalf("path = %q, want OpenAI-compatible chat completions path", gotPath)
	}
	if gotAuth != "Bearer sk-test-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotModel != "qwen3.7-max" {
		t.Fatalf("model = %q", gotModel)
	}
	if gotEnableThinking == nil || *gotEnableThinking {
		t.Fatalf("enable_thinking = %v, want explicit false", gotEnableThinking)
	}
}

type fakeBookKnowledgeLLMClient struct {
	answer   string
	err      error
	cfg      BookTokenPlanConfig
	messages []BookKnowledgeMessage
}

func (c *fakeBookKnowledgeLLMClient) Chat(_ context.Context, cfg BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.cfg = cfg
	c.messages = messages
	return c.answer, c.err
}
