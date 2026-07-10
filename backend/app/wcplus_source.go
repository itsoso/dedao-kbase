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
	URL      string `json:"url,omitempty"`
	BookID   string `json:"book_id,omitempty"`
}

type WCPlusRawImportRequest struct {
	ID          string `json:"id,omitempty"`
	Title       string `json:"title"`
	Nickname    string `json:"nickname,omitempty"`
	URL         string `json:"url,omitempty"`
	Content     string `json:"content"`
	PublishTime string `json:"publish_time,omitempty"`
	BookID      string `json:"book_id,omitempty"`
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
	TaskID          string `json:"task_id"`
	Biz             string `json:"biz,omitempty"`
	Nickname        string `json:"nickname,omitempty"`
	CrawlerType     string `json:"crawler_type,omitempty"`
	Status          string `json:"status,omitempty"`
	StatusError     string `json:"status_error,omitempty"`
	Message         string `json:"message,omitempty"`
	ArticleTotal    int    `json:"article_total,omitempty"`
	ArticleFinished int    `json:"article_finished,omitempty"`
	ReadingTotal    int    `json:"reading_total,omitempty"`
	ReadingFinished int    `json:"reading_finished,omitempty"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}

type WCPlusTaskRequest struct {
	Biz                  string `json:"biz"`
	Nickname             string `json:"nickname,omitempty"`
	ImageURL             string `json:"img,omitempty"`
	CrawlerType          string `json:"crawlerType,omitempty"`
	ArticleListType      string `json:"articleListType,omitempty"`
	ArticleListDate      int64  `json:"articleListDate,omitempty"`
	ArticleListAmount    int    `json:"articleListAmount,omitempty"`
	ArticleListOffset    int    `json:"articleListOffset,omitempty"`
	ArticleRefresh       bool   `json:"articleRefresh,omitempty"`
	ArticleImageDownload bool   `json:"articleImgDownload,omitempty"`
	ReadingDataType      string `json:"readingDataType,omitempty"`
	ReadingDataStartDate int64  `json:"readingDataStartDate,omitempty"`
	ReadingDataEndDate   int64  `json:"readingDataEndDate,omitempty"`
	ReadingDataAmount    int    `json:"readingDataAmount,omitempty"`
	ReadingDataOnlyMain  bool   `json:"readingDataOnlyMain,omitempty"`
	ReadingDataRefresh   bool   `json:"readingDataRefresh,omitempty"`
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
	Nicknames          []string `json:"nicknames"`
	ArticleListType    string   `json:"articleListType,omitempty"`
	ArticleListAmount  int      `json:"articleListAmount,omitempty"`
	StartQueue         bool     `json:"start_queue,omitempty"`
	ExactMatch         bool     `json:"exact_match,omitempty"`
	ImportToKBase      bool     `json:"import_to_kbase,omitempty"`
	ImportLimit        int      `json:"import_limit,omitempty"`
	WaitForCompletion  bool     `json:"wait_for_completion,omitempty"`
	PollAttempts       int      `json:"poll_attempts,omitempty"`
	PollIntervalMillis int      `json:"poll_interval_millis,omitempty"`
	BookIDPrefix       string   `json:"book_id_prefix,omitempty"`
}

type WCPlusBatchImportItem struct {
	Nickname string `json:"nickname"`
	Biz      string `json:"biz,omitempty"`
	TaskID   string `json:"task_id,omitempty"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
}

type WCPlusBatchImportResult struct {
	Success       []WCPlusBatchImportItem `json:"success"`
	Failed        []WCPlusBatchImportItem `json:"failed"`
	Started       bool                    `json:"started"`
	StartResult   any                     `json:"start_result,omitempty"`
	SuccessText   string                  `json:"success_text"`
	FailedText    string                  `json:"failed_text"`
	ImportedCount int                     `json:"imported_count,omitempty"`
	ImportedBooks []BookKnowledgeBook     `json:"imported_books,omitempty"`
	ImportErrors  []string                `json:"import_errors,omitempty"`
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
	var payload any
	if err := s.get(ctx, "/api/gzh/list", values, &payload); err != nil {
		return nil, err
	}
	items := wcplusArrayValue(payload, "gzh", "Gzh", "gzhs", "Gzhs", "accounts", "Accounts", "items", "Items", "list", "List")
	accounts := make([]WCPlusAccount, 0, len(items))
	for _, item := range items {
		account := wcplusAccountFromAny(item)
		if account.Biz == "" && account.Nickname == "" {
			continue
		}
		accounts = append(accounts, account)
	}
	total := wcplusIntValue(payload, "total", "Total", "count", "Count")
	if total == 0 {
		total = len(accounts)
	}
	return &WCPlusAccountList{Accounts: accounts, Total: total}, nil
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
	var payload any
	if err := s.get(ctx, "/api/report/gzh_articles", values, &payload); err != nil {
		return nil, err
	}
	account := wcplusAccountFromAny(wcplusObjectValue(payload, "gzh", "Gzh", "account", "Account"))
	if account.Nickname == "" {
		account.Nickname = opts.Nickname
	}
	if account.Biz == "" {
		account.Biz = strings.TrimSpace(opts.Biz)
	}
	items := wcplusArrayValue(payload, "articles", "Articles", "items", "Items", "list", "List", "results", "Results")
	articles := make([]WCPlusArticle, 0, len(items))
	for _, item := range items {
		article := wcplusArticleFromAny(item)
		if article.ID == "" && article.Title == "" {
			continue
		}
		if article.Nickname == "" {
			article.Nickname = account.Nickname
		}
		articles = append(articles, article)
	}
	total := wcplusIntValue(payload, "total", "Total", "count", "Count")
	if total == 0 {
		total = len(articles)
	}
	return &WCPlusArticleList{Account: account, Articles: articles, Total: total}, nil
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
	return s.getArticleContent(ctx, values, func(content *WCPlusArticleContent) {
		if content.Nickname == "" {
			content.Nickname = nickname
		}
		if content.ID == "" {
			content.ID = id
		}
	})
}

func (s *WCPlusSourceService) GetArticleContentByURL(ctx context.Context, rawURL string) (*WCPlusArticleContent, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	values := url.Values{}
	values.Set("url", rawURL)
	return s.getArticleContent(ctx, values, func(content *WCPlusArticleContent) {
		if content.URL == "" {
			content.URL = rawURL
		}
	})
}

func (s *WCPlusSourceService) getArticleContent(ctx context.Context, values url.Values, normalize func(*WCPlusArticleContent)) (*WCPlusArticleContent, error) {
	var content WCPlusArticleContent
	if err := s.get(ctx, "/api/article/content", values, &content); err != nil {
		return nil, err
	}
	if normalize != nil {
		normalize(&content)
	}
	return &content, nil
}

func (s *WCPlusSourceService) ImportArticle(ctx context.Context, store *BookKnowledgeStore, req WCPlusImportArticleRequest) (*BookKnowledgePackage, error) {
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	var content *WCPlusArticleContent
	var err error
	if strings.TrimSpace(req.URL) != "" && strings.TrimSpace(req.ID) == "" {
		content, err = s.GetArticleContentByURL(ctx, req.URL)
	} else {
		content, err = s.GetArticleContent(ctx, req.Nickname, req.ID)
	}
	if err != nil {
		return nil, err
	}
	pkg := WCPlusArticleToPackage(*content, req.BookID)
	if err := store.SavePackage(pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (s *WCPlusSourceService) ImportRawArticle(ctx context.Context, store *BookKnowledgeStore, req WCPlusRawImportRequest) (*BookKnowledgePackage, error) {
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	articleID := strings.TrimSpace(req.ID)
	if articleID == "" {
		articleID = manualWCPlusArticleID(title, req.Nickname, content)
	}
	article := WCPlusArticleContent{
		ID:          articleID,
		Title:       title,
		Nickname:    strings.TrimSpace(req.Nickname),
		URL:         strings.TrimSpace(req.URL),
		Content:     content,
		PublishTime: strings.TrimSpace(req.PublishTime),
	}
	pkg := WCPlusArticleToPackage(article, req.BookID)
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
		content, err := s.getListedArticleContent(ctx, nickname, article)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", wcplusArticleErrorLabel(article), err))
			continue
		}
		if content.ID == "" {
			content.ID = article.ID
		}
		if content.Title == "" {
			content.Title = article.Title
		}
		if content.Nickname == "" {
			content.Nickname = nickname
		}
		if content.URL == "" {
			content.URL = article.URL
		}
		if content.PublishTime == "" {
			content.PublishTime = article.PublishTime
		}
		bookID := ""
		if strings.TrimSpace(req.BookIDPrefix) != "" {
			bookID = strings.TrimSpace(req.BookIDPrefix) + "-" + wcplusArticleStableID(article, content)
		}
		pkg := WCPlusArticleToPackage(*content, bookID)
		if err := store.SavePackage(pkg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", wcplusArticleErrorLabel(article), err))
			continue
		}
		result.ImportedCount++
		result.Books = append(result.Books, pkg.Book)
	}
	return result, nil
}

func (s *WCPlusSourceService) getListedArticleContent(ctx context.Context, nickname string, article WCPlusArticle) (*WCPlusArticleContent, error) {
	if strings.TrimSpace(article.ID) != "" {
		return s.GetArticleContent(ctx, nickname, article.ID)
	}
	if strings.TrimSpace(article.URL) != "" {
		return s.GetArticleContentByURL(ctx, article.URL)
	}
	return nil, fmt.Errorf("article id or url is required")
}

func wcplusArticleStableID(article WCPlusArticle, content *WCPlusArticleContent) string {
	if strings.TrimSpace(article.ID) != "" {
		return strings.TrimSpace(article.ID)
	}
	if content != nil && strings.TrimSpace(content.ID) != "" {
		return strings.TrimSpace(content.ID)
	}
	title := article.Title
	nickname := article.Nickname
	url := article.URL
	if content != nil {
		if strings.TrimSpace(title) == "" {
			title = content.Title
		}
		if strings.TrimSpace(nickname) == "" {
			nickname = content.Nickname
		}
		if strings.TrimSpace(url) == "" {
			url = content.URL
		}
	}
	return manualWCPlusArticleID(title, nickname, url)
}

func wcplusArticleErrorLabel(article WCPlusArticle) string {
	if strings.TrimSpace(article.ID) != "" {
		return strings.TrimSpace(article.ID)
	}
	if strings.TrimSpace(article.URL) != "" {
		return strings.TrimSpace(article.URL)
	}
	if strings.TrimSpace(article.Title) != "" {
		return strings.TrimSpace(article.Title)
	}
	return "article"
}

func (s *WCPlusSourceService) ListTasks(ctx context.Context) ([]WCPlusTask, error) {
	var payload any
	if err := s.get(ctx, "/api/task/all", nil, &payload); err != nil {
		return nil, err
	}
	items := wcplusArrayValue(payload, "tasks", "Tasks", "items", "Items", "list", "List")
	tasks := make([]WCPlusTask, 0, len(items))
	for _, item := range items {
		task := wcplusTaskFromAny(item)
		if task.TaskID == "" && task.Status == "" {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *WCPlusSourceService) CreateTask(ctx context.Context, req WCPlusTaskRequest) (*WCPlusTask, error) {
	if strings.TrimSpace(req.Biz) == "" {
		return nil, fmt.Errorf("biz is required")
	}
	if strings.TrimSpace(req.CrawlerType) == "" {
		req.CrawlerType = "gzh_article_link"
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
		created, err := s.CreateTask(ctx, WCPlusTaskRequest{
			CrawlerType:       "gzh_article_link",
			Biz:               account.Biz,
			Nickname:          account.Nickname,
			ArticleListType:   articleListType,
			ArticleListAmount: req.ArticleListAmount,
		})
		if err != nil {
			result.Failed = append(result.Failed, WCPlusBatchImportItem{Nickname: nickname, Biz: account.Biz, Error: err.Error()})
			continue
		}
		item := WCPlusBatchImportItem{
			Nickname: account.Nickname,
			Biz:      account.Biz,
			TaskID:   created.TaskID,
			Status:   created.Status,
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

func (s *WCPlusSourceService) BatchImportNicknamesToKnowledge(ctx context.Context, store *BookKnowledgeStore, req WCPlusBatchImportRequest) (*WCPlusBatchImportResult, error) {
	result, err := s.BatchImportNicknames(ctx, req)
	if err != nil {
		return nil, err
	}
	if !req.ImportToKBase {
		return result, nil
	}
	if store == nil {
		return nil, fmt.Errorf("book knowledge store is required")
	}

	completedTasks := map[string]bool{}
	if req.WaitForCompletion {
		var waitErrors []string
		completedTasks, waitErrors = s.waitForBatchTasks(ctx, result.Success, req)
		result.ImportErrors = append(result.ImportErrors, waitErrors...)
	}

	importLimit := req.ImportLimit
	if importLimit <= 0 {
		importLimit = req.ArticleListAmount
	}
	importLimit = positiveOr(importLimit, 20)
	for _, item := range result.Success {
		if req.WaitForCompletion && strings.TrimSpace(item.TaskID) != "" && !completedTasks[item.TaskID] {
			continue
		}
		imported, err := s.ImportAccountArticles(ctx, store, WCPlusImportAccountRequest{
			Biz:          item.Biz,
			Nickname:     item.Nickname,
			Limit:        importLimit,
			BookIDPrefix: req.BookIDPrefix,
		})
		if err != nil {
			result.ImportErrors = append(result.ImportErrors, fmt.Sprintf("%s: %v", item.Nickname, err))
			continue
		}
		result.ImportedCount += imported.ImportedCount
		result.ImportedBooks = append(result.ImportedBooks, imported.Books...)
		for _, importErr := range imported.Errors {
			result.ImportErrors = append(result.ImportErrors, fmt.Sprintf("%s: %s", item.Nickname, importErr))
		}
	}
	return result, nil
}

func (s *WCPlusSourceService) waitForBatchTasks(ctx context.Context, items []WCPlusBatchImportItem, req WCPlusBatchImportRequest) (map[string]bool, []string) {
	pending := map[string]WCPlusBatchImportItem{}
	completed := map[string]bool{}
	observed := map[string]bool{}
	for _, item := range items {
		taskID := strings.TrimSpace(item.TaskID)
		if taskID != "" {
			pending[taskID] = item
		}
	}
	if len(pending) == 0 {
		return completed, nil
	}
	attempts := req.PollAttempts
	if attempts <= 0 {
		attempts = 30
	}
	interval := time.Duration(req.PollIntervalMillis) * time.Millisecond
	if interval <= 0 {
		interval = 2 * time.Second
	}
	waitErrors := []string{}
	for attempt := 0; attempt < attempts && len(pending) > 0; attempt++ {
		tasks, err := s.ListTasks(ctx)
		if err != nil {
			return completed, []string{fmt.Sprintf("poll tasks: %v", err)}
		}
		visible := map[string]bool{}
		for _, task := range tasks {
			taskID := strings.TrimSpace(task.TaskID)
			if _, ok := pending[taskID]; !ok {
				continue
			}
			visible[taskID] = true
			observed[taskID] = true
			status := strings.ToLower(strings.TrimSpace(task.Status))
			if wcplusTaskStatusComplete(status) {
				completed[taskID] = true
				delete(pending, taskID)
				continue
			}
			if wcplusTaskStatusFailed(status) {
				waitErrors = append(waitErrors, fmt.Sprintf("%s: task %s %s", pending[taskID].Nickname, taskID, status))
				delete(pending, taskID)
			}
		}
		for taskID, item := range pending {
			if visible[taskID] || !observed[taskID] {
				continue
			}
			verified, verifyErr := s.verifyWCPlusTaskArticleState(ctx, item)
			if verified {
				completed[taskID] = true
				delete(pending, taskID)
				continue
			}
			if attempt == attempts-1 {
				message := fmt.Sprintf("%s: task %s outcome could not be verified", item.Nickname, taskID)
				if verifyErr != nil {
					message += ": " + verifyErr.Error()
				}
				waitErrors = append(waitErrors, message)
				delete(pending, taskID)
			}
		}
		if len(pending) == 0 || attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			waitErrors = append(waitErrors, ctx.Err().Error())
			return completed, waitErrors
		case <-timer.C:
		}
	}
	for taskID, item := range pending {
		waitErrors = append(waitErrors, fmt.Sprintf("%s: task %s did not complete", item.Nickname, taskID))
	}
	return completed, waitErrors
}

func (s *WCPlusSourceService) verifyWCPlusTaskArticleState(ctx context.Context, item WCPlusBatchImportItem) (bool, error) {
	list, err := s.ListAccountArticles(ctx, WCPlusArticleListOptions{
		Biz:      item.Biz,
		Nickname: item.Nickname,
		Num:      1,
	})
	if err != nil {
		return false, err
	}
	return len(list.Articles) > 0, nil
}

func wcplusTaskStatusComplete(status string) bool {
	switch status {
	case "done", "success", "succeeded", "finished", "completed", "complete":
		return true
	default:
		return false
	}
}

func wcplusTaskStatusFailed(status string) bool {
	switch status {
	case "failed", "failure", "error", "stopped", "cancelled", "canceled":
		return true
	default:
		return false
	}
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
		Biz:          wcplusStringValue(value, "biz", "Biz", "fakeid", "FakeID"),
		Nickname:     wcplusStringValue(value, "nickname", "Nickname", "name", "Name"),
		Alias:        wcplusStringValue(value, "alias", "Alias"),
		Desc:         wcplusStringValue(value, "desc", "Desc"),
		ArticleCount: wcplusIntValue(value, "article_count", "ArticleCount", "articleCount", "total", "Total"),
	}
}

func wcplusArticleFromAny(value any) WCPlusArticle {
	return WCPlusArticle{
		ID:          wcplusStringValue(value, "id", "ID", "article_id", "ArticleID", "ArticleId", "articleId", "appmsgid", "AppMsgID", "app_msg_id", "msgid", "MsgID", "aid", "Aid"),
		Title:       wcplusStringValue(value, "title", "Title"),
		Nickname:    wcplusStringValue(value, "nickname", "Nickname", "gzh_nickname", "GzhNickname"),
		URL:         wcplusStringValue(value, "url", "URL", "link", "Link", "content_url", "ContentURL", "source_url", "SourceURL"),
		Digest:      wcplusStringValue(value, "digest", "Digest", "summary", "Summary"),
		PublishTime: wcplusStringValue(value, "publish_time", "PublishTime", "p_date_text", "PDateText", "pDateText", "date", "Date"),
		UpdateTime:  int64(wcplusIntValue(value, "update_time", "UpdateTime", "p_date", "PDate")),
	}
}

func wcplusTaskFromAny(value any) WCPlusTask {
	return WCPlusTask{
		TaskID:          wcplusStringValue(value, "task_id", "TaskID", "id", "ID"),
		Biz:             wcplusStringValue(value, "biz", "Biz"),
		Nickname:        wcplusStringValue(value, "nickname", "Nickname", "name", "Name"),
		CrawlerType:     wcplusStringValue(value, "crawler_type", "CrawlerType", "crawlerType"),
		Status:          wcplusStringValue(value, "status", "Status"),
		StatusError:     wcplusStringValue(value, "status_error", "StatusError", "statusError"),
		Message:         wcplusStringValue(value, "message", "Message", "msg", "Msg", "error", "Error"),
		ArticleTotal:    wcplusIntValue(value, "status_article_total_amount", "StatusArticleTotalAmount"),
		ArticleFinished: wcplusIntValue(value, "status_article_finished_amount", "StatusArticleFinishedAmount"),
		ReadingTotal:    wcplusIntValue(value, "status_reading_data_total_amount", "StatusReadingDataTotalAmount"),
		ReadingFinished: wcplusIntValue(value, "status_reading_data_finished_amount", "StatusReadingDataFinishedAmount"),
		CreatedAt:       int64(wcplusIntValue(value, "created_at", "CreatedAt")),
		UpdatedAt:       int64(wcplusIntValue(value, "updated_at", "UpdatedAt")),
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

func wcplusIntValue(value any, keys ...string) int {
	current := value
	if raw, ok := current.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			current = decoded
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		return 0
	}
	for _, key := range keys {
		found, ok := object[key]
		if !ok || found == nil {
			continue
		}
		switch typed := found.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		case json.Number:
			parsed, _ := typed.Int64()
			return int(parsed)
		default:
			parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(found)))
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func wcplusObjectValue(value any, keys ...string) any {
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
			return found
		}
	}
	return nil
}

func wcplusArrayValue(value any, keys ...string) []any {
	current := value
	if raw, ok := current.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			current = decoded
		}
	}
	if array, ok := current.([]any); ok {
		return array
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

func manualWCPlusArticleID(title string, nickname string, content string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{
		strings.TrimSpace(title),
		strings.TrimSpace(nickname),
		strings.TrimSpace(content),
	}, "|")))
	return "manual-" + hex.EncodeToString(sum[:])[:12]
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
