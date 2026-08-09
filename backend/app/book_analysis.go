package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	bookAnalysisVersion       = "1"
	bookAnalysisPromptVersion = "structured-v2-citations"
	bookAnalysisMaxTokens     = 4096

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

func (c *BookAnalysisClaim) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          string          `json:"id"`
		Statement   string          `json:"statement"`
		CitationIDs []string        `json:"citation_ids"`
		Confidence  float64         `json:"confidence"`
		Scope       json.RawMessage `json:"scope"`
		RiskLevel   string          `json:"risk_level"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	scope := []string(nil)
	if len(raw.Scope) > 0 {
		if string(raw.Scope) == "null" {
			return fmt.Errorf("scope must be a string or string array")
		}
		var scopeList []string
		if err := json.Unmarshal(raw.Scope, &scopeList); err == nil {
			scope = scopeList
		} else {
			var scopeText string
			if err := json.Unmarshal(raw.Scope, &scopeText); err != nil {
				return err
			}
			scopeText = strings.TrimSpace(scopeText)
			if scopeText != "" {
				scope = []string{scopeText}
			}
		}
	}
	*c = BookAnalysisClaim{
		ID:          raw.ID,
		Statement:   raw.Statement,
		CitationIDs: raw.CitationIDs,
		Confidence:  raw.Confidence,
		Scope:       scope,
		RiskLevel:   raw.RiskLevel,
	}
	return nil
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
	return s.SaveAnalysisManifestContext(context.Background(), manifest)
}

func (s *BookKnowledgeStore) SaveAnalysisManifestContext(ctx context.Context, manifest BookAnalysisManifest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	rootLock, err := s.acquireBookKnowledgeRootLock(ctx)
	if err != nil {
		return err
	}
	defer rootLock.Close()
	return s.saveAnalysisManifestUnlocked(manifest)
}

func (s *BookKnowledgeStore) saveAnalysisManifestUnlocked(manifest BookAnalysisManifest) error {
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
	return s.LoadAnalysisManifestContext(context.Background(), bookID)
}

func (s *BookKnowledgeStore) LoadAnalysisManifestContext(ctx context.Context, bookID string) (*BookAnalysisManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rootLock, err := s.acquireBookKnowledgeRootReadLock(ctx)
	if err != nil {
		return nil, err
	}
	defer rootLock.Close()
	return s.loadAnalysisManifestUnlocked(bookID)
}

func (s *BookKnowledgeStore) loadAnalysisManifestUnlocked(bookID string) (*BookAnalysisManifest, error) {
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
	applyStructuredQwenThinkingPolicy(&cfg)
	if cfg.MaxTokens < bookAnalysisMaxTokens {
		cfg.MaxTokens = bookAnalysisMaxTokens
	}

	prompt := `请对当前文章做结构化分析。只输出一个 JSON 对象，不要输出解释文字或 Markdown 围栏。结构必须为：
{"summary":"核心摘要","claims":[{"id":"claim-1","statement":"可验证结论","citation_ids":["citation ID"],"confidence":0.0,"scope":["适用范围"],"risk_level":"low|medium|high"}],"risks":[{"id":"risk-1","description":"风险与局限","citation_ids":["citation ID"],"severity":"low|medium|high"}],"actions":[{"id":"action-1","description":"阅读或验证行动","citation_ids":["citation ID"],"kind":"read|verify|monitor"}]}
每个事实性结论必须引用上下文中 [citation:<id>] 提供的 citation ID。citation_ids 禁止填写 chunk、chapter、claim 或未提供的 ID。Legacy Chunk 只能帮助识别证据缺口，不能作为 citation_ids。区分原文事实与模型推理。actions 只能是阅读、核验或跟踪动作，不能给出个人医疗建议。`
	contextText, stats, sources, err := buildBookAnalysisContext(store, pkg, prompt, request.MaxContextChars)
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
	if previous, loadErr := store.LoadAnalysisManifestContext(ctx, pkg.Book.BookID); loadErr == nil {
		manifest.CreatedAt = firstNonEmpty(previous.CreatedAt, now)
		manifest.Answer = previous.Answer
		manifest.Payload = previous.Payload
		manifest.CompletedAt = previous.CompletedAt
	}
	if err := store.SaveAnalysisManifestContext(ctx, manifest); err != nil {
		return nil, err
	}
	messages := []BookKnowledgeMessage{
		{
			Role:    "system",
			Content: "你是 KBase 的知识生产分析器。只使用提供的文章知识包，产出可复核的结构化分析；不得补充知识包中不存在的事实；所有事实性结论都要引用显式 citation ID。",
		},
		{
			Role:    "user",
			Content: buildBookAnalysisUserPrompt(pkg.Book, prompt, contextText),
		},
	}
	answer, err := client.Chat(ctx, cfg, messages)
	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		manifest.Status = BookAnalysisFailed
		manifest.Error = trimRunes(err.Error(), 2000)
		manifest.UpdatedAt = completedAt
		if saveErr := store.SaveAnalysisManifestContext(ctx, manifest); saveErr != nil {
			return nil, fmt.Errorf("%w (save failed analysis manifest: %v)", err, saveErr)
		}
		return nil, err
	}
	structured, err := parseBookAnalysisPayload(answer)
	if err != nil {
		manifest.Status = BookAnalysisFailed
		manifest.Error = trimRunes(err.Error(), 2000)
		manifest.UpdatedAt = completedAt
		if saveErr := store.SaveAnalysisManifestContext(ctx, manifest); saveErr != nil {
			return nil, fmt.Errorf("%w (save failed analysis manifest: %v)", err, saveErr)
		}
		return nil, err
	}
	if err := validateGeneratedBookAnalysisCitationIDs(*pkg, sources, *structured); err != nil {
		manifest.Status = BookAnalysisFailed
		manifest.Error = trimRunes(err.Error(), 2000)
		manifest.UpdatedAt = completedAt
		if saveErr := store.SaveAnalysisManifestContext(ctx, manifest); saveErr != nil {
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
	if err := store.SaveAnalysisManifestContext(ctx, manifest); err != nil {
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

func validateGeneratedBookAnalysisCitationIDs(
	pkg BookKnowledgePackage,
	sources []BookKnowledgeChatSource,
	payload BookAnalysisPayload,
) error {
	packageCitations := make(map[string]struct{}, len(pkg.Citations))
	for _, citation := range pkg.Citations {
		if id := strings.TrimSpace(citation.CitationID); id != "" {
			packageCitations[id] = struct{}{}
		}
	}
	allowed := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.Kind != "citation" {
			continue
		}
		id := strings.TrimSpace(source.ID)
		if _, ok := packageCitations[id]; ok {
			allowed[id] = struct{}{}
		}
	}
	validate := func(kind string, index int, ids []string) error {
		if len(ids) == 0 {
			return fmt.Errorf("%s[%d].citation_ids requires at least one exposed citation", kind, index)
		}
		for _, rawID := range ids {
			id := strings.TrimSpace(rawID)
			if _, ok := allowed[id]; !ok {
				return fmt.Errorf(
					"%s[%d].citation_ids contains non-exposed package citation %q",
					kind,
					index,
					opaqueBookAnalysisReferenceID(id),
				)
			}
		}
		return nil
	}
	for index, claim := range payload.Claims {
		if err := validate("claims", index, claim.CitationIDs); err != nil {
			return err
		}
	}
	for index, risk := range payload.Risks {
		if strings.TrimSpace(risk.ID) == "" {
			return fmt.Errorf("risks[%d].id is required", index)
		}
		if strings.TrimSpace(risk.Description) == "" {
			return fmt.Errorf("risks[%d].description is required", index)
		}
		if !validBookRiskLevel(risk.Severity) {
			return fmt.Errorf("risks[%d].severity is invalid", index)
		}
		if err := validate("risks", index, risk.CitationIDs); err != nil {
			return err
		}
	}
	for index, action := range payload.Actions {
		if strings.TrimSpace(action.ID) == "" {
			return fmt.Errorf("actions[%d].id is required", index)
		}
		if strings.TrimSpace(action.Description) == "" {
			return fmt.Errorf("actions[%d].description is required", index)
		}
		if !validBookAnalysisActionKind(action.Kind) {
			return fmt.Errorf("actions[%d].kind is invalid", index)
		}
		if err := validate("actions", index, action.CitationIDs); err != nil {
			return err
		}
	}
	return nil
}

func opaqueBookAnalysisReferenceID(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256-" + hex.EncodeToString(sum[:8])
}

func validBookAnalysisActionKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "read", "verify", "monitor":
		return true
	default:
		return false
	}
}

func buildBookAnalysisUserPrompt(book BookKnowledgeBook, prompt, contextText string) string {
	return fmt.Sprintf(`请基于下面的本地书籍证据生成结构化分析。

书名: %s
任务: %s

要求:
- 只允许引用上下文中 [citation:<id>] 明示的 citation ID。
- 不得把 chunk、chapter、claim、Legacy Chunk 或其他 ID 写入 citation_ids。
- 如果显式 citation 证据不足，减少结论并明确证据缺口。
- 不要输出大段原文复刻。

本地书籍证据上下文:
%s`, book.Title, prompt, contextText)
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
