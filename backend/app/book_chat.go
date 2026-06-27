package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultTokenPlanBaseURL = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	defaultTokenPlanModel   = "MiniMax-M2.5"
)

var defaultTokenPlanEnvFiles = []string{
	"/Users/liqiuhua/work/personal/health-llm-driven/backend/.env",
	"/Users/liqiuhua/work/personal/health-llm-driven/.env",
}

type BookTokenPlanConfig struct {
	APIKey  string `json:"-"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Source  string `json:"source,omitempty"`
}

type BookKnowledgeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type BookKnowledgeChatRequest struct {
	BookID          string `json:"book_id"`
	Mode            string `json:"mode"`
	Question        string `json:"question"`
	Model           string `json:"model,omitempty"`
	MaxContextChars int    `json:"max_context_chars,omitempty"`
}

type BookKnowledgeChatResponse struct {
	HistoryID    string                        `json:"history_id,omitempty"`
	Answer       string                        `json:"answer"`
	Model        string                        `json:"model"`
	Mode         string                        `json:"mode"`
	Sources      []BookKnowledgeChatSource     `json:"sources"`
	ContextStats BookKnowledgeChatContextStats `json:"context_stats"`
	CreatedAt    string                        `json:"created_at,omitempty"`
}

type BookKnowledgeChatSource struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	ChapterID string `json:"chapter_id,omitempty"`
}

type BookKnowledgeChatContextStats struct {
	Chapters int `json:"chapters"`
	Claims   int `json:"claims"`
	Chunks   int `json:"chunks"`
	Chars    int `json:"chars"`
}

type BookKnowledgeLLMClient interface {
	Chat(context.Context, BookTokenPlanConfig, []BookKnowledgeMessage) (string, error)
}

type TokenPlanChatClient struct {
	httpClient *http.Client
}

func LoadBookTokenPlanConfig() (BookTokenPlanConfig, error) {
	cfg := BookTokenPlanConfig{
		BaseURL: firstNonEmpty(os.Getenv("DEDAO_TOKENPLAN_BASE_URL"), os.Getenv("TOKENPLAN_BASE_URL"), defaultTokenPlanBaseURL),
		Model:   firstNonEmpty(os.Getenv("DEDAO_TOKENPLAN_MODEL"), os.Getenv("TOKENPLAN_MODEL"), defaultTokenPlanModel),
		APIKey:  firstNonEmpty(os.Getenv("DEDAO_TOKENPLAN_API_KEY"), os.Getenv("TOKENPLAN_API_KEY")),
		Source:  "environment",
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		return cfg, nil
	}

	envFiles := append([]string{}, defaultTokenPlanEnvFiles...)
	if path := strings.TrimSpace(os.Getenv("DEDAO_TOKENPLAN_ENV_FILE")); path != "" {
		envFiles = append([]string{path}, envFiles...)
	}
	for _, path := range envFiles {
		values, err := readEnvFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, err
		}
		if key := strings.TrimSpace(values["TOKENPLAN_API_KEY"]); key != "" {
			cfg.APIKey = key
			cfg.BaseURL = firstNonEmpty(values["TOKENPLAN_BASE_URL"], cfg.BaseURL, defaultTokenPlanBaseURL)
			cfg.Model = firstNonEmpty(values["TOKENPLAN_MODEL"], cfg.Model, defaultTokenPlanModel)
			cfg.Source = path
			return cfg, nil
		}
	}
	return cfg, fmt.Errorf("TOKENPLAN_API_KEY 未配置")
}

func BookKnowledgeChat(ctx context.Context, store *BookKnowledgeStore, request BookKnowledgeChatRequest) (*BookKnowledgeChatResponse, error) {
	return BookKnowledgeChatWithClient(ctx, store, request, NewTokenPlanChatClient(nil))
}

func BookKnowledgeChatWithClient(
	ctx context.Context,
	store *BookKnowledgeStore,
	request BookKnowledgeChatRequest,
	client BookKnowledgeLLMClient,
) (*BookKnowledgeChatResponse, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	request.BookID = strings.TrimSpace(request.BookID)
	if request.BookID == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	mode := normalizeBookChatMode(request.Mode)
	question := strings.TrimSpace(request.Question)
	if question == "" {
		question = defaultBookChatQuestion(mode)
	}
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if request.MaxContextChars <= 0 {
		request.MaxContextChars = 12000
	}

	cfg, err := LoadBookTokenPlanConfig()
	if err != nil {
		return nil, err
	}
	if model := strings.TrimSpace(request.Model); model != "" {
		cfg.Model = model
	}

	pkg, err := store.LoadPackage(request.BookID)
	if err != nil {
		return nil, err
	}
	contextText, stats, sources, err := buildBookChatContext(store, pkg, question, request.MaxContextChars)
	if err != nil {
		return nil, err
	}
	messages := []BookKnowledgeMessage{
		{
			Role:    "system",
			Content: "你是 dedao-gui 的书籍知识库对话助手。只基于用户提供的本地书籍知识包回答；不要声称读过未提供的原文；需要区分书中观点、你的推理和可执行建议；引用来源 ID。",
		},
		{
			Role:    "user",
			Content: buildBookChatUserPrompt(pkg.Book, mode, question, contextText),
		},
	}
	answer, err := client.Chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	response := &BookKnowledgeChatResponse{
		Answer:       answer,
		Model:        cfg.Model,
		Mode:         mode,
		Sources:      sources,
		ContextStats: stats,
	}
	history, err := store.SaveChatHistory(BookKnowledgeChatHistoryItem{
		BookID:       pkg.Book.BookID,
		BookTitle:    pkg.Book.Title,
		Mode:         mode,
		Question:     question,
		Model:        cfg.Model,
		Answer:       answer,
		Sources:      sources,
		ContextStats: stats,
	})
	if err != nil {
		return nil, err
	}
	response.HistoryID = history.ID
	response.CreatedAt = history.CreatedAt
	return response, nil
}

func NewTokenPlanChatClient(httpClient *http.Client) *TokenPlanChatClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	return &TokenPlanChatClient{httpClient: httpClient}
}

func (c *TokenPlanChatClient) Chat(ctx context.Context, cfg BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return "", fmt.Errorf("TOKENPLAN_API_KEY 未配置")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultTokenPlanBaseURL
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = defaultTokenPlanModel
	}
	payload := map[string]any{
		"model":       cfg.Model,
		"messages":    messages,
		"temperature": 0.2,
		"max_tokens":  2200,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("TokenPlan 调用失败: status=%d body=%s", resp.StatusCode, trimRunes(string(respBody), 600))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("TokenPlan 响应为空")
	}
	return parsed.Choices[0].Message.Content, nil
}

func buildBookChatContext(
	store *BookKnowledgeStore,
	pkg *BookKnowledgePackage,
	question string,
	maxChars int,
) (string, BookKnowledgeChatContextStats, []BookKnowledgeChatSource, error) {
	var builder strings.Builder
	stats := BookKnowledgeChatContextStats{}
	sources := make([]BookKnowledgeChatSource, 0)
	appendSection := func(text string) bool {
		text = strings.TrimSpace(text)
		if text == "" {
			return true
		}
		if builder.Len()+len([]rune(text))+2 > maxChars {
			return false
		}
		builder.WriteString(text)
		builder.WriteString("\n\n")
		return true
	}

	appendSection("## 书籍\n" + pkg.Book.Title + "\nbook_id: " + pkg.Book.BookID)
	for _, chapter := range pkg.Chapters {
		if !appendSection(fmt.Sprintf("## 章节 [%s]\n%s\n%s", chapter.ChapterID, chapter.Title, chapter.Summary)) {
			break
		}
		stats.Chapters++
		sources = append(sources, BookKnowledgeChatSource{
			Kind:      "chapter",
			ID:        chapter.ChapterID,
			Title:     chapter.Title,
			ChapterID: chapter.ChapterID,
		})
	}
	for _, claim := range pkg.Claims {
		if !appendSection(fmt.Sprintf("## Claim [%s]\n标题: %s\n内容: %s\n状态: %s", claim.ClaimID, claim.Title, claim.Summary, claim.ReviewStatus)) {
			break
		}
		stats.Claims++
		sources = append(sources, BookKnowledgeChatSource{
			Kind:      "claim",
			ID:        claim.ClaimID,
			Title:     claim.Title,
			ChapterID: claim.ChapterID,
		})
	}

	chunks := selectBookChatChunks(store, pkg, question)
	for _, chunk := range chunks {
		if !appendSection(fmt.Sprintf("## Chunk [%s]\nchapter_id: %s\n%s", chunk.ChunkID, chunk.ChapterID, chunk.Text)) {
			break
		}
		stats.Chunks++
		sources = append(sources, BookKnowledgeChatSource{
			Kind:      "chunk",
			ID:        chunk.ChunkID,
			ChapterID: chunk.ChapterID,
		})
	}
	stats.Chars = len([]rune(builder.String()))
	return strings.TrimSpace(builder.String()), stats, dedupeBookChatSources(sources), nil
}

func selectBookChatChunks(store *BookKnowledgeStore, pkg *BookKnowledgePackage, question string) []BookKnowledgeChunk {
	results, err := store.Search(BookKnowledgeSearchQuery{
		Query:  question,
		BookID: pkg.Book.BookID,
		Limit:  8,
	})
	if err != nil || len(results) == 0 {
		if len(pkg.Chunks) <= 8 {
			return append([]BookKnowledgeChunk(nil), pkg.Chunks...)
		}
		return append([]BookKnowledgeChunk(nil), pkg.Chunks[:8]...)
	}
	chunkByID := make(map[string]BookKnowledgeChunk, len(pkg.Chunks))
	for _, chunk := range pkg.Chunks {
		chunkByID[chunk.ChunkID] = chunk
	}
	var chunks []BookKnowledgeChunk
	for _, result := range results {
		if result.ChunkID == "" {
			continue
		}
		if chunk, ok := chunkByID[result.ChunkID]; ok {
			chunks = append(chunks, chunk)
		}
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].Order < chunks[j].Order
	})
	return chunks
}

func buildBookChatUserPrompt(book BookKnowledgeBook, mode, question, contextText string) string {
	return fmt.Sprintf(`请基于下面的本地书籍知识包回答。

书名: %s
任务模式: %s
问题: %s

要求:
- 先给结论，再给依据。
- 必须引用来源 ID，例如 [claim:42-claim-1] 或 [chunk:42-chunk-1]。
- 如果知识包不足以回答，明确说明缺口。
- 不要输出大段原文复刻；只做总结、分析和结构化推理。

本地知识包上下文:
%s`, book.Title, mode, question, contextText)
}

func normalizeBookChatMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "summary", "analysis", "actions", "rules", "chat":
		return strings.TrimSpace(mode)
	default:
		return "chat"
	}
}

func defaultBookChatQuestion(mode string) string {
	switch mode {
	case "summary":
		return "总结本书的核心观点、章节脉络和最重要的结论。"
	case "analysis":
		return "分析本书的论证结构、关键假设、适用边界和可迁移的方法。"
	case "actions":
		return "把本书内容提炼成可以执行的行动清单，并标注优先级和前置条件。"
	case "rules":
		return "把本书中可执行的方法提炼成规则卡，包含触发条件、执行步骤、风险约束和验证方式。"
	default:
		return ""
	}
}

func dedupeBookChatSources(sources []BookKnowledgeChatSource) []BookKnowledgeChatSource {
	seen := map[string]bool{}
	var result []BookKnowledgeChatSource
	for _, source := range sources {
		key := source.Kind + ":" + source.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, source)
	}
	return result
}

func readEnvFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			values[key] = value
		}
	}
	return values, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
