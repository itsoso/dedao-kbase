package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type WCPlusSourceConfig struct {
	HTTPClient *http.Client
	BaseURL    string
}

type WCPlusSourceService struct {
	client  *http.Client
	baseURL string
}

type WCPlusListOptions struct {
	Offset    int
	Num       int
	Sort      string
	Direction string
	Query     string
}

type WCPlusArticleListOptions struct {
	Biz       string
	Nickname  string
	Offset    int
	Num       int
	Sort      string
	Direction string
}

type WCPlusAccount struct {
	Biz          string `json:"biz"`
	Nickname     string `json:"nickname"`
	Alias        string `json:"alias,omitempty"`
	Desc         string `json:"desc,omitempty"`
	ArticleCount int    `json:"article_count,omitempty"`
}

type WCPlusAccountList struct {
	Accounts []WCPlusAccount `json:"accounts"`
	Total    int             `json:"total"`
}

type WCPlusArticle struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Nickname    string `json:"nickname,omitempty"`
	URL         string `json:"url,omitempty"`
	Digest      string `json:"digest,omitempty"`
	PublishTime string `json:"publish_time,omitempty"`
	UpdateTime  int64  `json:"update_time,omitempty"`
}

type WCPlusArticleList struct {
	Account  WCPlusAccount   `json:"account"`
	Articles []WCPlusArticle `json:"articles"`
	Total    int             `json:"total"`
}

type WCPlusArticleContent struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Nickname    string `json:"nickname"`
	URL         string `json:"url,omitempty"`
	Content     string `json:"content"`
	PublishTime string `json:"publish_time,omitempty"`
}

type WCPlusImportArticleRequest struct {
	Nickname string `json:"nickname"`
	ID       string `json:"id"`
	BookID   string `json:"book_id,omitempty"`
}

type WCPlusImportAccountRequest struct {
	Biz          string `json:"biz"`
	Nickname     string `json:"nickname"`
	Limit        int    `json:"limit,omitempty"`
	BookIDPrefix string `json:"book_id_prefix,omitempty"`
}

type WCPlusImportAccountResult struct {
	ImportedCount int                 `json:"imported_count"`
	Books         []BookKnowledgeBook `json:"books"`
	Errors        []string            `json:"errors,omitempty"`
}

type WCPlusTask struct {
	TaskID   string `json:"task_id"`
	Biz      string `json:"biz,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
}

type WCPlusTaskRequest struct {
	Biz      string `json:"biz"`
	Nickname string `json:"nickname,omitempty"`
}

type WCPlusTaskControlRequest struct {
	TaskID string `json:"task_id"`
	Action string `json:"action"`
}

type WCPlusStatus struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type WCPlusEnvCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type WCPlusEnvCheckResult struct {
	OK      bool             `json:"ok"`
	BaseURL string           `json:"base_url"`
	Checks  []WCPlusEnvCheck `json:"checks"`
	Advice  []string         `json:"advice,omitempty"`
}

type WCPlusBatchImportRequest struct {
	Nicknames         []string `json:"nicknames"`
	ArticleListType   string   `json:"articleListType,omitempty"`
	ArticleListAmount int      `json:"articleListAmount,omitempty"`
	StartQueue        bool     `json:"start_queue,omitempty"`
	ExactMatch        bool     `json:"exact_match,omitempty"`
}

type WCPlusBatchImportItem struct {
	Nickname string `json:"nickname"`
	Biz      string `json:"biz,omitempty"`
	TaskID   string `json:"task_id,omitempty"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
}

type WCPlusBatchImportResult struct {
	Success     []WCPlusBatchImportItem `json:"success"`
	Failed      []WCPlusBatchImportItem `json:"failed"`
	Started     bool                    `json:"started"`
	StartResult any                     `json:"start_result,omitempty"`
	SuccessText string                  `json:"success_text"`
	FailedText  string                  `json:"failed_text"`
}

func WCPlusSourceConfigFromEnv() WCPlusSourceConfig {
	baseURL := strings.TrimSpace(os.Getenv("WCPLUS_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("WCPLUSPRO_BASE_URL"))
	}
	return WCPlusSourceConfig{
		BaseURL: baseURL,
	}
}

func NewWCPlusSourceService(cfg WCPlusSourceConfig) *WCPlusSourceService {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:5001"
	}
	return &WCPlusSourceService{client: client, baseURL: baseURL}
}

func (s *WCPlusSourceService) ListAccounts(ctx context.Context, opts WCPlusListOptions) (*WCPlusAccountList, error) {
	values := url.Values{}
	values.Set("offset", strconv.Itoa(nonNegative(opts.Offset)))
	values.Set("num", strconv.Itoa(positiveOr(opts.Num, 20)))
	setOptionalQuery(values, "sort", opts.Sort)
	setOptionalQuery(values, "direction", opts.Direction)
	setOptionalQuery(values, "q", opts.Query)
	var payload struct {
		Accounts []WCPlusAccount `json:"gzhs"`
		Total    int             `json:"total"`
	}
	if err := s.get(ctx, "/api/gzh/list", values, &payload); err != nil {
		return nil, err
	}
	return &WCPlusAccountList{Accounts: payload.Accounts, Total: payload.Total}, nil
}

func (s *WCPlusSourceService) ListAccountArticles(ctx context.Context, opts WCPlusArticleListOptions) (*WCPlusArticleList, error) {
	if strings.TrimSpace(opts.Biz) == "" {
		return nil, fmt.Errorf("biz is required")
	}
	values := url.Values{}
	values.Set("biz", strings.TrimSpace(opts.Biz))
	values.Set("offset", strconv.Itoa(nonNegative(opts.Offset)))
	values.Set("num", strconv.Itoa(positiveOr(opts.Num, 20)))
	setOptionalQuery(values, "nickname", opts.Nickname)
	setOptionalQuery(values, "sort", opts.Sort)
	setOptionalQuery(values, "direction", opts.Direction)
	var payload struct {
		Account  WCPlusAccount   `json:"gzh"`
		Articles []WCPlusArticle `json:"articles"`
		Total    int             `json:"total"`
	}
	if err := s.get(ctx, "/api/report/gzh_articles", values, &payload); err != nil {
		return nil, err
	}
	if payload.Account.Nickname == "" {
		payload.Account.Nickname = opts.Nickname
	}
	return &WCPlusArticleList{Account: payload.Account, Articles: payload.Articles, Total: payload.Total}, nil
}

func (s *WCPlusSourceService) GetArticleContent(ctx context.Context, nickname string, id string) (*WCPlusArticleContent, error) {
	nickname = strings.TrimSpace(nickname)
	id = strings.TrimSpace(id)
	if nickname == "" {
		return nil, fmt.Errorf("nickname is required")
	}
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	values := url.Values{}
	values.Set("nickname", nickname)
	values.Set("id", id)
	var content WCPlusArticleContent
	if err := s.get(ctx, "/api/article/content", values, &content); err != nil {
		return nil, err
	}
	if content.Nickname == "" {
		content.Nickname = nickname
	}
	if content.ID == "" {
		content.ID = id
	}
	return &content, nil
}

func (s *WCPlusSourceService) ImportArticle(ctx context.Context, store *BookKnowledgeStore, req WCPlusImportArticleRequest) (*BookKnowledgePackage, error) {
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	content, err := s.GetArticleContent(ctx, req.Nickname, req.ID)
	if err != nil {
		return nil, err
	}
	pkg := WCPlusArticleToPackage(*content, req.BookID)
	if err := store.SavePackage(pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (s *WCPlusSourceService) ImportAccountArticles(ctx context.Context, store *BookKnowledgeStore, req WCPlusImportAccountRequest) (*WCPlusImportAccountResult, error) {
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	limit := positiveOr(req.Limit, 20)
	if limit > 100 {
		limit = 100
	}
	list, err := s.ListAccountArticles(ctx, WCPlusArticleListOptions{
		Biz:      req.Biz,
		Nickname: req.Nickname,
		Num:      limit,
	})
	if err != nil {
		return nil, err
	}
	result := &WCPlusImportAccountResult{Books: []BookKnowledgeBook{}}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" {
		nickname = list.Account.Nickname
	}
	for _, article := range list.Articles {
		content, err := s.GetArticleContent(ctx, nickname, article.ID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", article.ID, err))
			continue
		}
		if content.Title == "" {
			content.Title = article.Title
		}
		if content.URL == "" {
			content.URL = article.URL
		}
		if content.PublishTime == "" {
			content.PublishTime = article.PublishTime
		}
		bookID := ""
		if strings.TrimSpace(req.BookIDPrefix) != "" {
			bookID = strings.TrimSpace(req.BookIDPrefix) + "-" + article.ID
		}
		pkg := WCPlusArticleToPackage(*content, bookID)
		if err := store.SavePackage(pkg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", article.ID, err))
			continue
		}
		result.ImportedCount++
		result.Books = append(result.Books, pkg.Book)
	}
	return result, nil
}

func (s *WCPlusSourceService) ListTasks(ctx context.Context) ([]WCPlusTask, error) {
	var payload struct {
		Tasks []WCPlusTask `json:"tasks"`
	}
	if err := s.get(ctx, "/api/task/all", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Tasks, nil
}

func (s *WCPlusSourceService) CreateTask(ctx context.Context, req WCPlusTaskRequest) (*WCPlusTask, error) {
	if strings.TrimSpace(req.Biz) == "" {
		return nil, fmt.Errorf("biz is required")
	}
	var task WCPlusTask
	if err := s.post(ctx, "/api/task/new", req, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *WCPlusSourceService) ControlTask(ctx context.Context, req WCPlusTaskControlRequest) (*WCPlusTask, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(req.Action) == "" {
		return nil, fmt.Errorf("action is required")
	}
	var task WCPlusTask
	if err := s.post(ctx, "/api/task/control", req, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *WCPlusSourceService) Status(ctx context.Context) (*WCPlusStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return &WCPlusStatus{OK: false, Message: err.Error()}, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return &WCPlusStatus{
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 400,
		StatusCode: resp.StatusCode,
		Message:    resp.Status,
	}, nil
}

func (s *WCPlusSourceService) GetJSON(ctx context.Context, path string, values url.Values) (any, error) {
	var payload any
	if err := s.get(ctx, path, values, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *WCPlusSourceService) PostJSON(ctx context.Context, path string, payload any) (any, error) {
	var result any
	if err := s.post(ctx, path, payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *WCPlusSourceService) PostRaw(ctx context.Context, path string, payload any) ([]byte, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, "", readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("wcplus request failed: %s", resp.Status)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (s *WCPlusSourceService) CheckEnvironment(ctx context.Context) WCPlusEnvCheckResult {
	result := WCPlusEnvCheckResult{
		OK:      true,
		BaseURL: s.baseURL,
	}
	status, err := s.Status(ctx)
	if err != nil {
		result.OK = false
		result.Checks = append(result.Checks, WCPlusEnvCheck{Name: "service", OK: false, Message: err.Error()})
	} else {
		result.Checks = append(result.Checks, WCPlusEnvCheck{Name: "service", OK: status.OK, Message: status.Message})
		if !status.OK {
			result.OK = false
		}
	}
	if _, err := s.GetJSON(ctx, "/api/gzh/list", mapToWCPlusValues(map[string]string{
		"offset": "0",
		"num":    "1",
	})); err != nil {
		result.OK = false
		result.Checks = append(result.Checks, WCPlusEnvCheck{Name: "gzh_list", OK: false, Message: err.Error()})
	} else {
		result.Checks = append(result.Checks, WCPlusEnvCheck{Name: "gzh_list", OK: true, Message: "ok"})
	}
	if !result.OK {
		result.Advice = []string{
			"确认 wcplusPro 本地服务已启动。",
			"确认 WCPLUS_BASE_URL 或 WCPLUSPRO_BASE_URL 指向可访问的 5001 服务。",
			"确认当前授权版本支持本地 API。",
		}
	}
	return result
}

func (s *WCPlusSourceService) BatchImportNicknames(ctx context.Context, req WCPlusBatchImportRequest) (*WCPlusBatchImportResult, error) {
	result := &WCPlusBatchImportResult{}
	seen := map[string]bool{}
	articleListType := strings.TrimSpace(req.ArticleListType)
	if articleListType == "" {
		articleListType = "all"
	}
	exactMatch := req.ExactMatch
	for _, raw := range req.Nicknames {
		nickname := strings.TrimSpace(raw)
		if nickname == "" || seen[nickname] {
			continue
		}
		seen[nickname] = true
		account, err := s.findCandidateAccount(ctx, nickname, exactMatch)
		if err != nil {
			result.Failed = append(result.Failed, WCPlusBatchImportItem{Nickname: nickname, Error: err.Error()})
			continue
		}
		payload := map[string]any{
			"crawlerType":     "gzh_article_link",
			"biz":             account.Biz,
			"nickname":        account.Nickname,
			"articleListType": articleListType,
		}
		if req.ArticleListAmount > 0 {
			payload["articleListAmount"] = req.ArticleListAmount
		}
		created, err := s.PostJSON(ctx, "/api/batch_task/create_task", payload)
		if err != nil {
			result.Failed = append(result.Failed, WCPlusBatchImportItem{Nickname: nickname, Biz: account.Biz, Error: err.Error()})
			continue
		}
		item := WCPlusBatchImportItem{
			Nickname: account.Nickname,
			Biz:      account.Biz,
			TaskID:   wcplusStringValue(created, "task_id", "TaskID", "id", "ID"),
			Status:   wcplusStringValue(created, "status", "Status"),
		}
		result.Success = append(result.Success, item)
	}
	if req.StartQueue && len(result.Success) > 0 {
		started, err := s.PostJSON(ctx, "/api/task/control", map[string]any{"command": "run"})
		if err != nil {
			result.Failed = append(result.Failed, WCPlusBatchImportItem{Nickname: "_queue", Error: err.Error()})
		} else {
			result.Started = true
			result.StartResult = started
		}
	}
	result.SuccessText = wcplusBatchImportText(result.Success)
	result.FailedText = wcplusBatchImportText(result.Failed)
	return result, nil
}

func (s *WCPlusSourceService) findCandidateAccount(ctx context.Context, nickname string, exactMatch bool) (WCPlusAccount, error) {
	values := mapToWCPlusValues(map[string]string{
		"q":       nickname,
		"keyword": nickname,
		"offset":  "0",
		"num":     "20",
	})
	payload, err := s.GetJSON(ctx, "/api/search_gzh/search", values)
	if err != nil {
		return WCPlusAccount{}, err
	}
	candidates := wcplusArrayValue(payload, "candidates", "Candidates", "gzhs", "Gzhs", "accounts", "Accounts", "items", "Items")
	for _, candidate := range candidates {
		account := wcplusAccountFromAny(candidate)
		if account.Nickname == "" || account.Biz == "" {
			continue
		}
		if exactMatch && account.Nickname != nickname {
			continue
		}
		return account, nil
	}
	if exactMatch {
		return WCPlusAccount{}, fmt.Errorf("no exact candidate for %q", nickname)
	}
	return WCPlusAccount{}, fmt.Errorf("no candidate for %q", nickname)
}

func wcplusAccountFromAny(value any) WCPlusAccount {
	return WCPlusAccount{
		Biz:      wcplusStringValue(value, "biz", "Biz", "fakeid", "FakeID"),
		Nickname: wcplusStringValue(value, "nickname", "Nickname", "name", "Name"),
		Alias:    wcplusStringValue(value, "alias", "Alias"),
		Desc:     wcplusStringValue(value, "desc", "Desc"),
	}
}

func wcplusStringValue(value any, keys ...string) string {
	current := value
	if raw, ok := current.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			current = decoded
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range keys {
		if found, ok := object[key]; ok && found != nil {
			text := strings.TrimSpace(fmt.Sprint(found))
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func wcplusArrayValue(value any, keys ...string) []any {
	current := value
	if raw, ok := current.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			current = decoded
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if found, ok := object[key]; ok {
			if array, ok := found.([]any); ok {
				return array
			}
		}
	}
	return nil
}

func wcplusBatchImportText(items []WCPlusBatchImportItem) string {
	lines := []string{}
	for _, item := range items {
		if item.Error != "" {
			lines = append(lines, strings.TrimSpace(item.Nickname+"\t"+item.Error))
			continue
		}
		lines = append(lines, strings.TrimSpace(item.Nickname+"\t"+item.Biz+"\t"+item.TaskID))
	}
	return strings.Join(lines, "\n")
}

func WCPlusArticleToPackage(article WCPlusArticleContent, bookID string) BookKnowledgePackage {
	now := time.Now().Format(time.RFC3339)
	if strings.TrimSpace(bookID) == "" {
		source := article.URL
		if strings.TrimSpace(source) == "" {
			source = article.Nickname + ":" + article.ID
		}
		sum := sha1.Sum([]byte(source))
		bookID = "wcplus-" + hex.EncodeToString(sum[:])[:12]
	}
	bookID = sanitizeBookKnowledgeID(bookID)
	title := strings.TrimSpace(article.Title)
	if title == "" {
		title = bookID
	}
	text := strings.TrimSpace(article.Content)
	chapterID := bookID + "-article"
	chunkID := bookID + "-chunk-1"
	citationID := bookID + "-citation-1"
	return BookKnowledgePackage{
		Book: BookKnowledgeBook{
			BookID:     bookID,
			Title:      title,
			Author:     article.Nickname,
			SourceHTML: article.URL,
			CreatedAt:  now,
			UpdatedAt:  now,
			Status:     "draft",
			Extractor:  "wcplus-source-adapter",
		},
		Chapters: []BookKnowledgeChapter{{
			ChapterID: chapterID,
			BookID:    bookID,
			Order:     1,
			Title:     title,
			ChunkIDs:  []string{chunkID},
		}},
		Chunks: []BookKnowledgeChunk{{
			ChunkID:   chunkID,
			BookID:    bookID,
			ChapterID: chapterID,
			Order:     1,
			Text:      text,
		}},
		Claims: []BookKnowledgeClaim{},
		Citations: []BookKnowledgeCitation{{
			CitationID: citationID,
			BookID:     bookID,
			ChapterID:  chapterID,
			ChunkID:    chunkID,
			SourceHTML: article.URL,
			Anchor:     article.PublishTime,
			Note:       "wcplus public account article",
		}},
	}
}

func (s *WCPlusSourceService) get(ctx context.Context, path string, values url.Values, target any) error {
	endpoint := s.baseURL + path
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return s.do(req, target)
}

func (s *WCPlusSourceService) post(ctx context.Context, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req, target)
}

func (s *WCPlusSourceService) do(req *http.Request, target any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wcplus request failed: %s", resp.Status)
	}
	var raw json.RawMessage
	var envelope struct {
		Success *bool           `json:"success"`
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Data != nil {
		if envelope.Success != nil && !*envelope.Success {
			if strings.TrimSpace(envelope.Message) != "" {
				return fmt.Errorf("wcplus rejected request: %s", envelope.Message)
			}
			if strings.TrimSpace(envelope.Error) != "" {
				return fmt.Errorf("wcplus rejected request: %s", envelope.Error)
			}
			return fmt.Errorf("wcplus rejected request: code=%d", envelope.Code)
		}
		return json.Unmarshal(envelope.Data, target)
	}
	return json.Unmarshal(raw, target)
}

func setOptionalQuery(values url.Values, key string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		values.Set(key, value)
	}
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func positiveOr(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func mapToWCPlusValues(values map[string]string) url.Values {
	query := url.Values{}
	for key, value := range values {
		query.Set(key, value)
	}
	return query
}
