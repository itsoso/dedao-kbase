package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSourceConnectorCapabilitiesNormalizeOperations(t *testing.T) {
	capabilities, err := NormalizeSourceConnectorCapabilities(SourceConnectorCapabilities{
		Name:                " wechat_mp ",
		SupportedOperations: []string{" sync_articles ", "", "sync_articles", "sync_content"},
		SupportsCheckpoint:  true,
		SupportsBackfill:    true,
		DefaultLicenseScope: " personal_use ",
	})
	if err != nil {
		t.Fatalf("normalize capabilities: %v", err)
	}
	if capabilities.Name != "wechat_mp" {
		t.Fatalf("name = %q", capabilities.Name)
	}
	if strings.Join(capabilities.SupportedOperations, ",") != "sync_articles,sync_content" {
		t.Fatalf("operations = %#v", capabilities.SupportedOperations)
	}
	if capabilities.DefaultLicenseScope != SourceLicenseScopePersonalUse {
		t.Fatalf("license scope = %q", capabilities.DefaultLicenseScope)
	}
}

func TestSourceCheckpointRejectsRegression(t *testing.T) {
	previous := SourceCheckpoint{Cursor: "page-2", Sequence: 2}
	next := SourceCheckpoint{Cursor: "page-1", Sequence: 1}

	if err := ValidateSourceCheckpointAdvance(previous, next); err == nil {
		t.Fatalf("accepted regressed checkpoint: %#v -> %#v", previous, next)
	}
	if err := ValidateSourceCheckpointAdvance(previous, SourceCheckpoint{Cursor: "page-3", Sequence: 3}); err != nil {
		t.Fatalf("rejected advanced checkpoint: %v", err)
	}
	if err := ValidateSourceCheckpointAdvance(previous, previous); err != nil {
		t.Fatalf("rejected idempotent checkpoint: %v", err)
	}
}

func TestSourceDocumentEnvelopeNormalizesIdentityAndContentHash(t *testing.T) {
	envelope, contentHash, err := NormalizeSourceDocumentEnvelope(SourceDocumentEnvelope{
		IdempotencyKey:   " idem-1 ",
		SourceType:       " wechat_mp_article ",
		SourceAccountKey: " biz-health ",
		SourceAccount:    " 健康参考 ",
		SourceItemKey:    " article-42 ",
		Title:            " 证据如何进入知识库 ",
		Author:           " 编辑部 ",
		SourceURL:        "https://mp.weixin.qq.com/s/article-42?from=timeline#rd",
		PublishedAt:      "2026-07-16T09:00:00Z",
		Content:          "# 标题\r\n\r\n第一段。\n\n\n第二段。",
		ContentFormat:    " Markdown ",
		LicenseScope:     " personal_use ",
		Metadata:         map[string]string{" category ": " health ", "empty": "   "},
	})
	if err != nil {
		t.Fatalf("normalize document: %v", err)
	}
	if envelope.SourceType != "wechat_mp_article" || envelope.SourceAccountKey != "biz-health" || envelope.SourceItemKey != "article-42" {
		t.Fatalf("identity not normalized: %#v", envelope)
	}
	if envelope.SourceURL != "https://mp.weixin.qq.com/s/article-42?from=timeline" {
		t.Fatalf("source url = %q", envelope.SourceURL)
	}
	if envelope.ContentFormat != "markdown" || envelope.LicenseScope != SourceLicenseScopePersonalUse {
		t.Fatalf("format/scope = %q/%q", envelope.ContentFormat, envelope.LicenseScope)
	}
	if envelope.Metadata["category"] != "health" {
		t.Fatalf("metadata = %#v", envelope.Metadata)
	}
	if _, ok := envelope.Metadata["empty"]; ok {
		t.Fatalf("empty metadata survived: %#v", envelope.Metadata)
	}
	sum := sha256.Sum256([]byte(envelope.Content))
	if contentHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("content hash = %q", contentHash)
	}
}

func TestSourceDocumentEnvelopeRejectsUnsafeLicenseScope(t *testing.T) {
	_, _, err := NormalizeSourceDocumentEnvelope(SourceDocumentEnvelope{
		IdempotencyKey:   "idem-unsafe",
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "biz-health",
		SourceItemKey:    "article-unsafe",
		Title:            "不安全授权",
		SourceURL:        "https://mp.weixin.qq.com/s/unsafe",
		Content:          strings.Repeat("授权边界必须清晰。", 4),
		ContentFormat:    "markdown",
		LicenseScope:     "mirror_raw_content",
	})
	if err == nil {
		t.Fatalf("accepted unsafe license scope")
	}
}

type fakeSourceConnector struct{}

func (fakeSourceConnector) Name() string { return "fake" }

func (fakeSourceConnector) Capabilities() SourceConnectorCapabilities {
	return SourceConnectorCapabilities{
		Name:                "fake",
		SupportedOperations: []string{"sync_content"},
		SupportsCheckpoint:  true,
		DefaultLicenseScope: SourceLicenseScopePersonalUse,
	}
}

func (fakeSourceConnector) Fetch(ctx context.Context, request SourceFetchRequest) (SourceFetchPage, error) {
	return SourceFetchPage{
		Checkpoint: SourceCheckpoint{Cursor: "next", Sequence: request.Checkpoint.Sequence + 1},
		Documents: []SourceDocumentEnvelope{{
			IdempotencyKey:   "fake-1",
			SourceType:       request.SourceType,
			SourceAccountKey: request.SourceAccountKey,
			SourceItemKey:    "fake-article-1",
			Title:            "fake article",
			SourceURL:        "https://example.com/fake-article-1",
			Content:          strings.Repeat("fake content ", 8),
			ContentFormat:    "markdown",
			LicenseScope:     SourceLicenseScopePersonalUse,
		}},
	}, nil
}

func TestSourceConnectorInterfaceFetchesDocuments(t *testing.T) {
	var connector SourceConnector = fakeSourceConnector{}
	page, err := connector.Fetch(context.Background(), SourceFetchRequest{
		SourceType:       "manual_article",
		SourceAccountKey: "manual",
		Operation:        "sync_content",
		Limit:            1,
		Checkpoint:       SourceCheckpoint{Cursor: "start", Sequence: 1},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(page.Documents) != 1 || page.Checkpoint.Sequence != 2 {
		t.Fatalf("page = %#v", page)
	}
}
