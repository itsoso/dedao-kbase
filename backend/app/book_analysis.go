package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	bookAnalysisVersion = "1"

	BookAnalysisPending = "pending"
	BookAnalysisRunning = "running"
	BookAnalysisReady   = "ready"
	BookAnalysisFailed  = "failed"
)

type BookAnalysisManifest struct {
	Version      string                        `json:"version"`
	BookID       string                        `json:"book_id"`
	ContentHash  string                        `json:"content_hash"`
	Status       string                        `json:"status"`
	Model        string                        `json:"model,omitempty"`
	Prompt       string                        `json:"prompt,omitempty"`
	Answer       string                        `json:"answer,omitempty"`
	Sources      []BookKnowledgeChatSource     `json:"sources,omitempty"`
	ContextStats BookKnowledgeChatContextStats `json:"context_stats,omitempty"`
	Error        string                        `json:"error,omitempty"`
	CreatedAt    string                        `json:"created_at,omitempty"`
	UpdatedAt    string                        `json:"updated_at"`
	CompletedAt  string                        `json:"completed_at,omitempty"`
}

type BookAnalysisGenerateRequest struct {
	BookID          string `json:"book_id"`
	Model           string `json:"model,omitempty"`
	MaxContextChars int    `json:"max_context_chars,omitempty"`
}

func (s *BookKnowledgeStore) BookAnalysisManifestPath(bookID string) string {
	return filepath.Join(s.BookDir(bookID), "analysis_manifest.json")
}

func (s *BookKnowledgeStore) SaveAnalysisManifest(manifest BookAnalysisManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest.BookID = sanitizeBookKnowledgeID(manifest.BookID)
	if strings.TrimSpace(manifest.BookID) == "" {
		return fmt.Errorf("analysis manifest missing book_id")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		manifest.Version = bookAnalysisVersion
	}
	if strings.TrimSpace(manifest.Status) == "" {
		manifest.Status = BookAnalysisPending
	}
	payload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.BookDir(manifest.BookID), os.ModePerm); err != nil {
		return err
	}
	return writeFileAtomically(s.BookAnalysisManifestPath(manifest.BookID), payload)
}

func (s *BookKnowledgeStore) LoadAnalysisManifest(bookID string) (*BookAnalysisManifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bookID = sanitizeBookKnowledgeID(bookID)
	if strings.TrimSpace(bookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	var manifest BookAnalysisManifest
	if err := readJSONFile(s.BookAnalysisManifestPath(bookID), &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func GenerateBookAnalysisManifest(ctx context.Context, store *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
	return GenerateBookAnalysisManifestWithClient(ctx, store, request, NewTokenPlanChatClient(nil))
}

func GenerateBookAnalysisManifestWithClient(
	ctx context.Context,
	store *BookKnowledgeStore,
	request BookAnalysisGenerateRequest,
	client BookKnowledgeLLMClient,
) (*BookAnalysisManifest, error) {
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
	if request.MaxContextChars <= 0 {
		request.MaxContextChars = 16000
	}
	pkg, err := store.LoadPackage(request.BookID)
	if err != nil {
		return nil, err
	}
	cfg, err := LoadBookTokenPlanConfig()
	if err != nil {
		return nil, err
	}
	if model := strings.TrimSpace(request.Model); model != "" {
		cfg.Model = normalizeBookTokenPlanModel(model)
	}
	cfg.Model = normalizeBookTokenPlanModel(cfg.Model)

	prompt := "请对当前文章做结构化分析，输出以下部分：核心摘要、可验证结论、风险与局限、行动建议。每个事实性结论必须引用提供的来源 ID，并明确区分原文事实与模型推理。"
	contextText, stats, sources, err := buildBookChatContext(store, pkg, prompt, request.MaxContextChars)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	manifest := BookAnalysisManifest{
		Version:      bookAnalysisVersion,
		BookID:       pkg.Book.BookID,
		ContentHash:  pkg.Book.ContentHash,
		Status:       BookAnalysisRunning,
		Model:        cfg.Model,
		Prompt:       prompt,
		Sources:      sources,
		ContextStats: stats,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if previous, loadErr := store.LoadAnalysisManifest(pkg.Book.BookID); loadErr == nil {
		manifest.CreatedAt = firstNonEmpty(previous.CreatedAt, now)
		manifest.Answer = previous.Answer
		manifest.CompletedAt = previous.CompletedAt
	}
	if err := store.SaveAnalysisManifest(manifest); err != nil {
		return nil, err
	}
	messages := []BookKnowledgeMessage{
		{
			Role:    "system",
			Content: "你是 KBase 的知识生产分析器。只使用提供的文章知识包，产出可复核的结构化分析；不得补充知识包中不存在的事实；所有事实性结论都要引用来源 ID。",
		},
		{
			Role:    "user",
			Content: buildBookChatUserPrompt(pkg.Book, "analysis", prompt, contextText),
		},
	}
	answer, err := client.Chat(ctx, cfg, messages)
	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		manifest.Status = BookAnalysisFailed
		manifest.Error = trimRunes(err.Error(), 2000)
		manifest.UpdatedAt = completedAt
		if saveErr := store.SaveAnalysisManifest(manifest); saveErr != nil {
			return nil, fmt.Errorf("%w (save failed analysis manifest: %v)", err, saveErr)
		}
		return nil, err
	}
	manifest.Status = BookAnalysisReady
	manifest.Answer = strings.TrimSpace(answer)
	manifest.Error = ""
	manifest.UpdatedAt = completedAt
	manifest.CompletedAt = completedAt
	if err := store.SaveAnalysisManifest(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
