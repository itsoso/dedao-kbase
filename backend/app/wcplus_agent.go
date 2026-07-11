package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrWCPlusTaskOutcomeUnverified = errors.New("wcplus task outcome could not be verified")
	ErrWCPlusTaskTimeout           = errors.New("wcplus task did not complete before polling stopped")
)

type WCPlusAgentBlockedError struct {
	Reason string
}

func (e *WCPlusAgentBlockedError) Error() string {
	return "wcplus operation blocked: " + strings.TrimSpace(e.Reason)
}

type WCPlusAgentConfig struct {
	Client           *SourceAgentClient
	WCPlus           *WCPlusSourceService
	Outbox           *SourceAgentOutbox
	Version          string
	Capabilities     []string
	LeaseDuration    time.Duration
	TaskPollAttempts int
	TaskPollInterval time.Duration
	OnTaskProgress   func(WCPlusTask)
}

type WCPlusAgentCycleResult struct {
	OK              bool   `json:"ok"`
	WCPlusHealthy   bool   `json:"wcplus_healthy"`
	RunID           string `json:"run_id,omitempty"`
	Status          string `json:"status,omitempty"`
	Uploaded        int    `json:"uploaded"`
	Failed          int    `json:"failed"`
	OutboxRemaining int    `json:"outbox_remaining"`
}

type WCPlusAgent struct {
	client           *SourceAgentClient
	wcplus           *WCPlusSourceService
	outbox           *SourceAgentOutbox
	version          string
	capabilities     []string
	leaseDuration    time.Duration
	taskPollAttempts int
	taskPollInterval time.Duration
	onTaskProgress   func(WCPlusTask)
}

func NewWCPlusAgent(config WCPlusAgentConfig) (*WCPlusAgent, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("source agent client is required")
	}
	if config.WCPlus == nil {
		return nil, fmt.Errorf("WC Plus source service is required")
	}
	if config.Outbox == nil {
		return nil, fmt.Errorf("source agent outbox is required")
	}
	if strings.TrimSpace(config.Version) == "" {
		config.Version = "0.1.0"
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.TaskPollAttempts <= 0 {
		config.TaskPollAttempts = 30
	}
	if config.TaskPollInterval <= 0 {
		config.TaskPollInterval = 2 * time.Second
	}
	return &WCPlusAgent{
		client:           config.Client,
		wcplus:           config.WCPlus,
		outbox:           config.Outbox,
		version:          strings.TrimSpace(config.Version),
		capabilities:     normalizeSourceCapabilities(config.Capabilities),
		leaseDuration:    config.LeaseDuration,
		taskPollAttempts: config.TaskPollAttempts,
		taskPollInterval: config.TaskPollInterval,
		onTaskProgress:   config.OnTaskProgress,
	}, nil
}

func (a *WCPlusAgent) Close() error {
	if a == nil || a.outbox == nil {
		return nil
	}
	return a.outbox.Close()
}

func (a *WCPlusAgent) RunOnce(ctx context.Context) (WCPlusAgentCycleResult, error) {
	result := WCPlusAgentCycleResult{OK: true}
	status, err := a.wcplus.Status(ctx)
	if err != nil {
		return result, fmt.Errorf("check local WC Plus: %w", err)
	}
	result.WCPlusHealthy = status != nil && status.OK
	lastError := ""
	if !result.WCPlusHealthy && status != nil {
		lastError = status.Message
	}
	if _, err := a.client.Heartbeat(ctx, SourceAgentHeartbeat{
		Version:       a.version,
		Capabilities:  a.capabilities,
		WCPlusHealthy: result.WCPlusHealthy,
		WCPlusVersion: status.Version,
		LastError:     lastError,
	}); err != nil {
		return result, fmt.Errorf("send source-agent heartbeat: %w", err)
	}
	if !result.WCPlusHealthy {
		return result, nil
	}

	run, err := a.client.Lease(ctx, a.capabilities, a.leaseDuration)
	if err != nil {
		return result, fmt.Errorf("lease source sync run: %w", err)
	}
	if run == nil {
		return result, nil
	}
	result.RunID = run.ID
	result.Status = run.Status
	if run.Subscription == nil {
		err := fmt.Errorf("leased run %s is missing its subscription snapshot", run.ID)
		return result, a.failRun(ctx, run.ID, err)
	}

	cursor := strings.TrimSpace(run.Subscription.Cursor)
	uploaded, outboxCursor, err := a.flushRunOutbox(ctx, run.ID)
	cursor = laterWCPlusAgentCursor(cursor, outboxCursor)
	result.Uploaded += uploaded
	if err != nil {
		return result, err
	}
	uploaded, runCursor, err := a.executeRun(ctx, *run)
	result.Uploaded += uploaded
	cursor = laterWCPlusAgentCursor(cursor, runCursor)
	if err != nil {
		if sourceAgentRequestRetryable(err) {
			return result, err
		}
		return result, a.failRun(ctx, run.ID, err)
	}
	pending, err := a.outbox.CountPendingForRun(run.ID)
	if err != nil {
		return result, fmt.Errorf("count pending source outbox items: %w", err)
	}
	result.OutboxRemaining = pending
	if pending != 0 {
		return result, fmt.Errorf("run %s still has %d pending outbox items", run.ID, pending)
	}
	completed, err := a.client.CompleteRun(ctx, run.ID, cursor)
	if err != nil {
		return result, fmt.Errorf("complete source sync run: %w", err)
	}
	result.Status = completed.Status
	result.Failed = completed.FailedCount
	result.OK = completed.Status == SourceRunSucceeded || completed.Status == SourceRunPartial
	return result, nil
}

func (a *WCPlusAgent) failRun(ctx context.Context, runID string, cause error) error {
	_, err := a.client.FailRun(ctx, runID, cause.Error())
	if err != nil {
		return fmt.Errorf("%w; report run failure: %v", cause, err)
	}
	return cause
}

func (a *WCPlusAgent) executeRun(ctx context.Context, run SourceSyncRun) (int, string, error) {
	switch run.RequestedOperation {
	case "existing_articles", "sync_links", "sync_content":
		return a.executeArticleRun(ctx, run)
	case "sync_reading_data":
		return 0, "", a.executeReadingDataRun(ctx, run)
	default:
		return 0, "", fmt.Errorf("unsupported source sync operation %q", run.RequestedOperation)
	}
}

func (a *WCPlusAgent) executeArticleRun(ctx context.Context, run SourceSyncRun) (int, string, error) {
	subscription := run.Subscription
	limit := sourceAgentOptionInt(subscription.Options, "limit", 20, 100)
	list, err := a.wcplus.ListAccountArticles(ctx, WCPlusArticleListOptions{
		Biz:      subscription.SourceAccountKey,
		Nickname: subscription.SourceAccount,
		Num:      limit,
	})
	if err != nil {
		return 0, "", err
	}
	needsLinkTask := run.RequestedOperation == "sync_links" ||
		(run.RequestedOperation == "sync_content" && len(list.Articles) == 0)
	if needsLinkTask {
		if err := a.runAccountTask(ctx, run.ID, *subscription, list.Account.ImageURL, "gzh_article_link", limit, func(ctx context.Context) (bool, error) {
			verified, err := a.wcplus.ListAccountArticles(ctx, WCPlusArticleListOptions{
				Biz: subscription.SourceAccountKey, Nickname: subscription.SourceAccount, Num: 1,
			})
			return err == nil && len(verified.Articles) > 0, err
		}); err != nil {
			return 0, "", err
		}
		list, err = a.wcplus.ListAccountArticles(ctx, WCPlusArticleListOptions{
			Biz: subscription.SourceAccountKey, Nickname: subscription.SourceAccount, Num: limit,
		})
		if err != nil {
			return 0, "", err
		}
	}
	if len(list.Articles) == 0 {
		return 0, "", nil
	}

	type articleResult struct {
		article WCPlusArticle
		content *WCPlusArticleContent
		err     error
	}
	results := make([]articleResult, 0, len(list.Articles))
	failedIndexes := make([]int, 0)
	for _, article := range list.Articles {
		content, contentErr := a.wcplus.getListedArticleContent(ctx, subscription.SourceAccount, article)
		results = append(results, articleResult{article: article, content: content, err: contentErr})
		if contentErr != nil {
			failedIndexes = append(failedIndexes, len(results)-1)
		}
	}
	if run.RequestedOperation == "sync_content" && len(failedIndexes) > 0 {
		sample := results[failedIndexes[0]].article
		if err := a.runAccountTask(ctx, run.ID, *subscription, list.Account.ImageURL, "article", limit, func(ctx context.Context) (bool, error) {
			content, verifyErr := a.wcplus.getListedArticleContent(ctx, subscription.SourceAccount, sample)
			return verifyErr == nil && content != nil && strings.TrimSpace(content.Content) != "", verifyErr
		}); err != nil {
			return 0, "", err
		}
		for _, index := range failedIndexes {
			content, contentErr := a.wcplus.getListedArticleContent(ctx, subscription.SourceAccount, results[index].article)
			results[index].content = content
			results[index].err = contentErr
		}
	}

	for _, articleResult := range results {
		itemKey := wcplusAgentSourceItemKey(articleResult.article)
		if articleResult.err != nil {
			idempotencyKey := wcplusAgentIdempotencyKey(run.ID, itemKey, "failure")
			if _, err := a.client.ReportItemFailure(ctx, run.ID, itemKey, idempotencyKey, articleResult.err.Error()); err != nil {
				return 0, "", fmt.Errorf("report source item failure: %w", err)
			}
			continue
		}
		envelope := wcplusAgentArticleEnvelope(run.ID, *subscription, articleResult.article, *articleResult.content)
		if _, err := a.outbox.Enqueue(run.ID, envelope); err != nil {
			return 0, "", fmt.Errorf("enqueue source article: %w", err)
		}
	}
	return a.flushRunOutbox(ctx, run.ID)
}

func (a *WCPlusAgent) executeReadingDataRun(ctx context.Context, run SourceSyncRun) error {
	subscription := *run.Subscription
	imageURL := sourceAgentOptionString(subscription.Options, "image_url", "")
	if imageURL == "" {
		list, err := a.wcplus.ListAccountArticles(ctx, WCPlusArticleListOptions{
			Biz: subscription.SourceAccountKey, Nickname: subscription.SourceAccount, Num: 1,
		})
		if err != nil {
			return err
		}
		imageURL = list.Account.ImageURL
	}
	request := WCPlusTaskRequest{
		Biz:                  subscription.SourceAccountKey,
		Nickname:             subscription.SourceAccount,
		ImageURL:             imageURL,
		CrawlerType:          "reading_data",
		ReadingDataType:      sourceAgentOptionString(subscription.Options, "reading_data_type", "all"),
		ReadingDataStartDate: int64(sourceAgentOptionInt(subscription.Options, "reading_data_start", 0, int(^uint(0)>>1))),
		ReadingDataEndDate:   int64(sourceAgentOptionInt(subscription.Options, "reading_data_end", 0, int(^uint(0)>>1))),
		ReadingDataAmount:    sourceAgentOptionInt(subscription.Options, "reading_data_amount", 1000, 5000),
		ReadingDataOnlyMain:  sourceAgentOptionBool(subscription.Options, "reading_data_only_main", true),
		ReadingDataRefresh:   sourceAgentOptionBool(subscription.Options, "reading_data_refresh", false),
	}
	return a.createAndWaitForTask(ctx, run.ID, request, func(ctx context.Context) (bool, error) {
		_, err := a.wcplus.GetJSON(ctx, "/api/report/reading_data", mapToWCPlusValues(map[string]string{
			"biz": subscription.SourceAccountKey,
		}))
		return err == nil, err
	})
}

func (a *WCPlusAgent) runAccountTask(
	ctx context.Context,
	runID string,
	subscription SourceSubscription,
	imageURL string,
	crawlerType string,
	limit int,
	verify func(context.Context) (bool, error),
) error {
	request := WCPlusTaskRequest{
		Biz:                  subscription.SourceAccountKey,
		Nickname:             subscription.SourceAccount,
		ImageURL:             sourceAgentOptionString(subscription.Options, "image_url", imageURL),
		CrawlerType:          crawlerType,
		ArticleListType:      sourceAgentOptionString(subscription.Options, "article_list_type", "amount"),
		ArticleListDate:      int64(sourceAgentOptionInt(subscription.Options, "article_list_date", 0, int(^uint(0)>>1))),
		ArticleListAmount:    limit,
		ArticleListOffset:    sourceAgentOptionInt(subscription.Options, "article_list_offset", 0, 5000),
		ArticleRefresh:       sourceAgentOptionBool(subscription.Options, "article_refresh", false),
		ArticleImageDownload: sourceAgentOptionBool(subscription.Options, "article_image_download", false),
	}
	return a.createAndWaitForTask(ctx, runID, request, verify)
}

func (a *WCPlusAgent) createAndWaitForTask(
	ctx context.Context,
	runID string,
	request WCPlusTaskRequest,
	verify func(context.Context) (bool, error),
) error {
	task, err := a.wcplus.CreateTask(ctx, request)
	if err != nil {
		return err
	}
	if task == nil || strings.TrimSpace(task.TaskID) == "" {
		return fmt.Errorf("wcplus task creation returned no task_id")
	}
	if reason := wcplusTaskBlockedReason(*task); reason != "" {
		return &WCPlusAgentBlockedError{Reason: reason}
	}
	if _, err := a.wcplus.PostJSON(ctx, "/api/task/control", map[string]any{"command": "run"}); err != nil {
		return err
	}
	return a.waitForTask(ctx, runID, task.TaskID, verify)
}

func (a *WCPlusAgent) waitForTask(
	ctx context.Context,
	runID, taskID string,
	verify func(context.Context) (bool, error),
) error {
	lastSeen := false
	for attempt := 0; attempt < a.taskPollAttempts; attempt++ {
		tasks, err := a.wcplus.ListTasks(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, task := range tasks {
			if task.TaskID != taskID {
				continue
			}
			found = true
			lastSeen = true
			if a.onTaskProgress != nil {
				a.onTaskProgress(task)
			}
			if reason := wcplusTaskBlockedReason(task); reason != "" {
				return &WCPlusAgentBlockedError{Reason: reason}
			}
			switch strings.ToLower(strings.TrimSpace(task.Status)) {
			case "succeeded", "success", "completed", "complete", "done", "ok":
				return nil
			case "failed", "failure", "error", "blocked", "canceled", "cancelled":
				return &WCPlusAgentBlockedError{Reason: strings.TrimSpace(task.Status + " " + task.Message)}
			}
			break
		}
		if !found && verify != nil {
			verified, verifyErr := verify(ctx)
			if verified {
				return nil
			}
			if verifyErr != nil && attempt == a.taskPollAttempts-1 {
				return fmt.Errorf("%w: %v", ErrWCPlusTaskOutcomeUnverified, verifyErr)
			}
		}
		if attempt < a.taskPollAttempts-1 {
			timer := time.NewTimer(a.taskPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if !lastSeen {
		return fmt.Errorf("%w: task %s for run %s", ErrWCPlusTaskOutcomeUnverified, taskID, runID)
	}
	return fmt.Errorf("%w: task %s for run %s", ErrWCPlusTaskTimeout, taskID, runID)
}

func (a *WCPlusAgent) flushRunOutbox(ctx context.Context, runID string) (int, string, error) {
	items, err := a.outbox.PeekReadyForRun(runID, 100)
	if err != nil {
		return 0, "", err
	}
	uploaded := 0
	cursor := ""
	for _, item := range items {
		if _, err := a.client.UploadArticle(ctx, runID, item.Envelope); err != nil {
			statusCode := 0
			var httpErr *SourceAgentHTTPError
			if errors.As(err, &httpErr) {
				statusCode = httpErr.StatusCode
			}
			updated, recordErr := a.outbox.RecordFailure(item.ID, statusCode, err)
			if recordErr != nil {
				return uploaded, cursor, recordErr
			}
			if statusCode == http.StatusBadRequest && updated.State == SourceOutboxDead {
				continue
			}
			return uploaded, cursor, fmt.Errorf("upload source article: %w", err)
		}
		if err := a.outbox.Acknowledge(item.ID); err != nil {
			return uploaded, cursor, err
		}
		uploaded++
		cursor = laterWCPlusAgentCursor(cursor, wcplusAgentCursorForEnvelope(item.Envelope))
	}
	return uploaded, cursor, nil
}

func sourceAgentRequestRetryable(err error) bool {
	var httpErr *SourceAgentHTTPError
	return errors.As(err, &httpErr) && httpErr.Retryable()
}

func wcplusTaskBlockedReason(task WCPlusTask) string {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{task.Status, task.StatusError, task.Message}, " ")))
	for _, marker := range []string{
		"not_max_version", "unactivated", "not activated", "throttl", "too many requests",
		"parameter expired", "parameter_expired", "request parameter expired", "req_data expired",
	} {
		if strings.Contains(text, marker) {
			reason := strings.TrimSpace(task.StatusError + " " + task.Message)
			if reason == "" {
				reason = strings.TrimSpace(task.Status)
			}
			return reason
		}
	}
	return ""
}

type wcplusAgentCursor struct {
	PublishedAt   string `json:"published_at"`
	SourceItemKey string `json:"source_item_key"`
}

func wcplusAgentCursorForEnvelope(envelope SourceArticleEnvelope) string {
	cursor := wcplusAgentCursor{
		PublishedAt:   strings.TrimSpace(envelope.PublishedAt),
		SourceItemKey: strings.TrimSpace(envelope.SourceItemID),
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func laterWCPlusAgentCursor(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	var leftCursor, rightCursor wcplusAgentCursor
	if json.Unmarshal([]byte(left), &leftCursor) != nil {
		return right
	}
	if json.Unmarshal([]byte(right), &rightCursor) != nil {
		return left
	}
	if rightCursor.PublishedAt > leftCursor.PublishedAt ||
		(rightCursor.PublishedAt == leftCursor.PublishedAt && rightCursor.SourceItemKey > leftCursor.SourceItemKey) {
		return right
	}
	return left
}

func wcplusAgentArticleEnvelope(runID string, subscription SourceSubscription, article WCPlusArticle, content WCPlusArticleContent) SourceArticleEnvelope {
	itemKey := strings.TrimSpace(content.ID)
	if itemKey == "" {
		itemKey = wcplusAgentSourceItemKey(article)
	}
	title := strings.TrimSpace(content.Title)
	if title == "" {
		title = strings.TrimSpace(article.Title)
	}
	sourceURL := strings.TrimSpace(content.URL)
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(article.URL)
	}
	publishedAt := strings.TrimSpace(content.PublishTime)
	if publishedAt == "" {
		publishedAt = strings.TrimSpace(article.PublishTime)
	}
	envelope := SourceArticleEnvelope{
		SourceType:      subscription.SourceType,
		SourceAccountID: subscription.SourceAccountKey,
		SourceAccount:   subscription.SourceAccount,
		SourceItemID:    itemKey,
		Title:           title,
		Author:          strings.TrimSpace(content.Nickname),
		SourceURL:       sourceURL,
		PublishedAt:     publishedAt,
		Content:         content.Content,
		ContentFormat:   "markdown",
	}
	envelope.IdempotencyKey = wcplusAgentIdempotencyKey(runID, itemKey, content.Content)
	return envelope
}

func wcplusAgentSourceItemKey(article WCPlusArticle) string {
	if value := strings.TrimSpace(article.ID); value != "" {
		return value
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(article.URL) + "\x00" + strings.TrimSpace(article.Title)))
	return "article-" + hex.EncodeToString(sum[:])[:16]
}

func wcplusAgentIdempotencyKey(runID, itemKey, content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(runID) + "\x00" + strings.TrimSpace(itemKey) + "\x00" + content))
	return "wcplus-" + hex.EncodeToString(sum[:])
}

func sourceAgentOptionInt(options map[string]any, key string, fallback, maximum int) int {
	value := fallback
	switch typed := options[key].(type) {
	case int:
		value = typed
	case int64:
		value = int(typed)
	case float64:
		value = int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			value = parsed
		}
	}
	if value < 0 {
		value = fallback
	}
	if maximum > 0 && value > maximum {
		value = maximum
	}
	return value
}

func sourceAgentOptionString(options map[string]any, key, fallback string) string {
	if value, ok := options[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func sourceAgentOptionBool(options map[string]any, key string, fallback bool) bool {
	if value, ok := options[key].(bool); ok {
		return value
	}
	return fallback
}
