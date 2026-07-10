package app

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSourceAgentOutboxPersistsOrderedLifecycle(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC))
	stateDir := t.TempDir()
	outbox, err := newSourceAgentOutbox(stateDir, clock.Now, func(time.Duration) time.Duration { return 0 }, 3)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	first, err := outbox.Enqueue("run-1", sourceAgentOutboxEnvelope("idem-1", "article-1", "第一篇文章"))
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	clock.Advance(time.Millisecond)
	second, err := outbox.Enqueue("run-1", sourceAgentOutboxEnvelope("idem-2", "article-2", "第二篇文章"))
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	duplicate, err := outbox.Enqueue("run-1", sourceAgentOutboxEnvelope("idem-1", "article-1", "第一篇文章"))
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("duplicate enqueue = %#v, err=%v", duplicate, err)
	}

	ready, err := outbox.PeekReady(10)
	if err != nil || len(ready) != 2 || ready[0].ID != first.ID || ready[1].ID != second.ID {
		t.Fatalf("ready items = %#v, err=%v", ready, err)
	}
	if strings.Contains(ready[0].Envelope.Content, "\r") || ready[0].Envelope.SourceURL != "https://mp.weixin.qq.com/s/article-1" {
		t.Fatalf("envelope was not normalized: %#v", ready[0].Envelope)
	}
	if err := outbox.Acknowledge(first.ID); err != nil {
		t.Fatalf("acknowledge first: %v", err)
	}
	retried, err := outbox.RecordFailure(second.ID, 0, errors.New("transport unavailable"))
	if err != nil {
		t.Fatalf("record retry: %v", err)
	}
	if retried.AttemptCount != 1 || retried.State != SourceOutboxPending {
		t.Fatalf("retried item = %#v", retried)
	}
	wantNext := clock.Now().Add(time.Second).Format(time.RFC3339Nano)
	if retried.NextAttemptAt != wantNext {
		t.Fatalf("next attempt = %q, want %q", retried.NextAttemptAt, wantNext)
	}
	ready, err = outbox.PeekReady(10)
	if err != nil || len(ready) != 0 {
		t.Fatalf("backoff item was ready: %#v, err=%v", ready, err)
	}
	if err := outbox.Close(); err != nil {
		t.Fatalf("close outbox: %v", err)
	}

	clock.Advance(time.Second)
	reopened, err := newSourceAgentOutbox(stateDir, clock.Now, func(time.Duration) time.Duration { return 0 }, 3)
	if err != nil {
		t.Fatalf("reopen outbox: %v", err)
	}
	defer reopened.Close()
	ready, err = reopened.PeekReady(10)
	if err != nil || len(ready) != 1 || ready[0].ID != second.ID || ready[0].AttemptCount != 1 {
		t.Fatalf("reopened ready items = %#v, err=%v", ready, err)
	}
	if info, err := os.Stat(reopened.DBPath()); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("outbox permissions = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestSourceAgentOutboxMovesTerminalAndExhaustedFailuresToDeadLetter(t *testing.T) {
	clock := newSourceSyncTestClock(time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC))
	outbox, err := newSourceAgentOutbox(t.TempDir(), clock.Now, func(time.Duration) time.Duration { return 0 }, 2)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	defer outbox.Close()

	terminal, err := outbox.Enqueue("run-terminal", sourceAgentOutboxEnvelope("idem-terminal", "article-terminal", "终态失败文章"))
	if err != nil {
		t.Fatalf("enqueue terminal: %v", err)
	}
	terminal, err = outbox.RecordFailure(terminal.ID, 400, errors.New("validation failed"))
	if err != nil || terminal.State != SourceOutboxDead || terminal.LastError != "validation failed" {
		t.Fatalf("terminal failure = %#v, err=%v", terminal, err)
	}

	exhausted, err := outbox.Enqueue("run-retry", sourceAgentOutboxEnvelope("idem-retry", "article-retry", "重试失败文章"))
	if err != nil {
		t.Fatalf("enqueue retry: %v", err)
	}
	exhausted, err = outbox.RecordFailure(exhausted.ID, 503, errors.New("server unavailable"))
	if err != nil || exhausted.State != SourceOutboxPending {
		t.Fatalf("first retry = %#v, err=%v", exhausted, err)
	}
	clock.Advance(time.Second)
	exhausted, err = outbox.RecordFailure(exhausted.ID, 503, errors.New("server still unavailable"))
	if err != nil || exhausted.State != SourceOutboxDead || exhausted.AttemptCount != 2 {
		t.Fatalf("exhausted retry = %#v, err=%v", exhausted, err)
	}

	dead, err := outbox.ListDeadLetters(10)
	if err != nil || len(dead) != 2 {
		t.Fatalf("dead letters = %#v, err=%v", dead, err)
	}
}

func TestSourceAgentOutboxRejectsUnnormalizedArticle(t *testing.T) {
	outbox, err := NewSourceAgentOutbox(t.TempDir())
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	defer outbox.Close()
	_, err = outbox.Enqueue("run-1", SourceArticleEnvelope{
		IdempotencyKey: "idem-short",
		SourceType:     "wcplus_wechat_article",
		SourceItemID:   "article-short",
		Content:        "短文",
	})
	if err == nil {
		t.Fatalf("invalid envelope was queued")
	}
}

func sourceAgentOutboxEnvelope(idempotencyKey, itemID, title string) SourceArticleEnvelope {
	return SourceArticleEnvelope{
		IdempotencyKey:  idempotencyKey,
		SourceType:      "wcplus_wechat_article",
		SourceAccountID: "biz-test",
		SourceAccount:   "测试账号",
		SourceItemID:    itemID,
		Title:           title,
		SourceURL:       "https://mp.weixin.qq.com/s/" + itemID + "#fragment",
		Content:         "# " + title + "\r\n\r\n" + strings.Repeat("这是用于验证本地持久化、重试和恢复行为的正文内容。", 5),
		ContentFormat:   "markdown",
	}
}
