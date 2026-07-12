package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	bookAnalysisVersion       = "1"
	bookAnalysisPromptVersion = "structured-v1"

	BookAnalysisPending = "pending"
	BookAnalysisRunning = "running"
	BookAnalysisReady   = "ready"
	BookAnalysisFailed  = "failed"
)

type BookAnalysisPayload struct {
	Summary string               `json:"summary"`
	Claims  []BookAnalysisClaim  `json:"claims"`
	Risks   []BookAnalysisRisk   `json:"risks"`
	Actions []BookAnalysisAction `json:"actions"`
}

type BookAnalysisClaim struct {
	ID          string   `json:"id"`
	Statement   string   `json:"statement"`
	CitationIDs []string `json:"citation_ids"`
	Confidence  float64  `json:"confidence"`
	Scope       []string `json:"scope,omitempty"`
	RiskLevel   string   `json:"risk_level"`
}

type BookAnalysisRisk struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	CitationIDs []string `json:"citation_ids,omitempty"`
	Severity    string   `json:"severity"`
}

type BookAnalysisAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	CitationIDs []string `json:"citation_ids,omitempty"`
	Kind        string   `json:"kind"`
}

type BookAnalysisManifest struct {
	Version       string                        `json:"version"`
	BookID        string                        `json:"book_id"`
	ContentHash   string                        `json:"content_hash"`
	Status        string                        `json:"status"`
	Model         string                        `json:"model,omitempty"`
	PromptVersion string                        `json:"prompt_version,omitempty"`
	Prompt        string                        `json:"prompt,omitempty"`
	Answer        string                        `json:"answer,omitempty"`
	Payload       *BookAnalysisPayload          `json:"payload,omitempty"`
	Sources       []BookKnowledgeChatSource     `json:"sources,omitempty"`
	ContextStats  BookKnowledgeChatContextStats `json:"context_stats,omitempty"`
	Error         string                        `json:"error,omitempty"`
	CreatedAt     string                        `json:"created_at,omitempty"`
	UpdatedAt     string                        `json:"updated_at"`
	CompletedAt   string                        `json:"completed_at,omitempty"`
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

	prompt := `请对当前文章做结构化分析。只输出一个 JSON 对象，不要输出解释文字或 Markdown 围栏。结构必须为：
{"summary":"核心摘要","claims":[{"id":"claim-1","statement":"可验证结论","citation_ids":["来源 ID"],"confidence":0.0,"scope":["适用范围"],"risk_level":"low|medium|high"}],"risks":[{"id":"risk-1","description":"风险与局限","citation_ids":["来源 ID"],"severity":"low|medium|high"}],"actions":[{"id":"action-1","description":"阅读或验证行动","citation_ids":["来源 ID"],"kind":"read|verify|monitor"}]}
每个事实性结论必须引用提供的来源 ID。区分原文事实与模型推理。actions 只能是阅读、核验或跟踪动作，不能给出个人医疗建议。`
	contextText, stats, sources, err := buildBookChatContext(store, pkg, prompt, request.MaxContextChars)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	manifest := BookAnalysisManifest{
		Version:       bookAnalysisVersion,
		BookID:        pkg.Book.BookID,
		ContentHash:   pkg.Book.ContentHash,
		Status:        BookAnalysisRunning,
		Model:         cfg.Model,
		PromptVersion: bookAnalysisPromptVersion,
		Prompt:        prompt,
		Sources:       sources,
		ContextStats:  stats,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if previous, loadErr := store.LoadAnalysisManifest(pkg.Book.BookID); loadErr == nil {
		manifest.CreatedAt = firstNonEmpty(previous.CreatedAt, now)
		manifest.Answer = previous.Answer
		manifest.Payload = previous.Payload
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
	structured, err := parseBookAnalysisPayload(answer)
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
	manifest.Payload = structured
	manifest.Answer = renderBookAnalysisMarkdown(*structured)
	manifest.Error = ""
	manifest.UpdatedAt = completedAt
	manifest.CompletedAt = completedAt
	if err := store.SaveAnalysisManifest(manifest); err != nil {
		return nil, err
	}
	if _, err := EvaluateBookAnalysisQuality(store, manifest.BookID); err != nil {
		return nil, fmt.Errorf("evaluate structured analysis quality: %w", err)
	}
	return &manifest, nil
}

func parseBookAnalysisPayload(answer string) (*BookAnalysisPayload, error) {
	raw := strings.TrimSpace(answer)
	if strings.HasPrefix(raw, "```") {
		firstNewline := strings.IndexByte(raw, '\n')
		lastFence := strings.LastIndex(raw, "```")
		if firstNewline < 0 || lastFence <= firstNewline {
			return nil, fmt.Errorf("structured analysis response is not valid JSON")
		}
		raw = strings.TrimSpace(raw[firstNewline+1 : lastFence])
	}
	var payload BookAnalysisPayload
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("structured analysis response is not valid JSON: %w", err)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return nil, fmt.Errorf("structured analysis summary is required")
	}
	return &payload, nil
}

func renderBookAnalysisMarkdown(payload BookAnalysisPayload) string {
	var builder strings.Builder
	builder.WriteString("# 核心摘要\n\n")
	builder.WriteString(strings.TrimSpace(payload.Summary))
	if len(payload.Claims) > 0 {
		builder.WriteString("\n\n## 可验证结论\n")
		for _, claim := range payload.Claims {
			builder.WriteString("\n- ")
			builder.WriteString(strings.TrimSpace(claim.Statement))
			if len(claim.CitationIDs) > 0 {
				builder.WriteString(" [")
				builder.WriteString(strings.Join(claim.CitationIDs, ", "))
				builder.WriteString("]")
			}
		}
	}
	if len(payload.Risks) > 0 {
		builder.WriteString("\n\n## 风险与局限\n")
		for _, risk := range payload.Risks {
			builder.WriteString("\n- ")
			builder.WriteString(strings.TrimSpace(risk.Description))
		}
	}
	if len(payload.Actions) > 0 {
		builder.WriteString("\n\n## 后续行动\n")
		for _, action := range payload.Actions {
			builder.WriteString("\n- ")
			builder.WriteString(strings.TrimSpace(action.Description))
		}
	}
	return strings.TrimSpace(builder.String())
}
