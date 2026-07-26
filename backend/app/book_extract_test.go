package app

import (
	"strings"
	"testing"
)

func TestExtractBookKnowledgeFromHTML(t *testing.T) {
	html := `<!doctype html>
<html>
  <head><title>测试书</title><style>.x{}</style><script>ignore()</script></head>
  <body>
    <h1>第一章 趋势</h1>
    <p>趋势过滤是量化规则的前置条件。MACD 背离不能脱离市场结构。</p>
    <p>第二段补充证据来源。</p>
    <h2>第二章 风控</h2>
    <p>仓位管理需要设置单笔风险和组合风险预算。</p>
  </body>
</html>`
	book := BookKnowledgeBook{
		BookID:     "42",
		DedaoID:    42,
		Title:      "42_测试书_作者",
		SourceHTML: "/tmp/book.html",
	}

	pkg, err := ExtractBookKnowledgeFromHTML(book, html)
	if err != nil {
		t.Fatalf("ExtractBookKnowledgeFromHTML returned error: %v", err)
	}

	if pkg.Book.BookID != "42" {
		t.Fatalf("book id = %q, want 42", pkg.Book.BookID)
	}
	if pkg.Book.Status != "draft" {
		t.Fatalf("status = %q, want draft", pkg.Book.Status)
	}
	if pkg.Book.Extractor != "dedao-gui-fallback" {
		t.Fatalf("extractor = %q, want fallback extractor", pkg.Book.Extractor)
	}
	if len(pkg.Chapters) != 2 {
		t.Fatalf("chapters = %#v, want 2 chapters", pkg.Chapters)
	}
	if pkg.Chapters[0].Title != "第一章 趋势" || pkg.Chapters[1].Title != "第二章 风控" {
		t.Fatalf("chapter titles = %#v", pkg.Chapters)
	}
	if len(pkg.Chunks) != 2 {
		t.Fatalf("chunks = %#v, want one chunk per chapter", pkg.Chunks)
	}
	if len(pkg.Claims) != 2 {
		t.Fatalf("claims = %#v, want one draft claim per chapter", pkg.Claims)
	}
	if pkg.Claims[0].ReviewStatus != "draft" || pkg.Claims[0].EvidenceLevel != "D" {
		t.Fatalf("claim governance fields = %#v", pkg.Claims[0])
	}
	if len(pkg.Citations) != 2 {
		t.Fatalf("citations = %#v, want one citation per chapter", pkg.Citations)
	}
}

func TestExtractBookKnowledgeFromHTMLWithoutHeadings(t *testing.T) {
	html := `<html><body><p>没有标题时应该落入正文章节，并保留正文内容。</p></body></html>`
	book := BookKnowledgeBook{BookID: "empty-heading", Title: "无标题书"}

	pkg, err := ExtractBookKnowledgeFromHTML(book, html)
	if err != nil {
		t.Fatalf("ExtractBookKnowledgeFromHTML returned error: %v", err)
	}
	if len(pkg.Chapters) != 1 {
		t.Fatalf("chapters = %#v, want fallback chapter", pkg.Chapters)
	}
	if pkg.Chapters[0].Title != "正文" {
		t.Fatalf("fallback chapter title = %q, want 正文", pkg.Chapters[0].Title)
	}
	if len(pkg.Chunks) != 1 || pkg.Chunks[0].Text == "" {
		t.Fatalf("chunks = %#v, want extracted body text", pkg.Chunks)
	}
}

func TestExtractBookKnowledgeFromGeneratedHeaderHTML(t *testing.T) {
	html := `<html><body>
<div class="header0"><h1>第一章</h1></div>
<p>第一章正文。</p>
</body></html>`
	book := BookKnowledgeBook{BookID: "generated-header", Title: "生成 HTML"}

	pkg, err := ExtractBookKnowledgeFromHTML(book, html)
	if err != nil {
		t.Fatalf("ExtractBookKnowledgeFromHTML returned error: %v", err)
	}
	if len(pkg.Chapters) != 1 {
		t.Fatalf("chapters = %#v, want deduplicated generated heading", pkg.Chapters)
	}
	if pkg.Chapters[0].Title != "第一章" {
		t.Fatalf("chapter title = %q, want 第一章", pkg.Chapters[0].Title)
	}
}

func TestExtractBookKnowledgeCreatesCitationForEveryChunk(t *testing.T) {
	html := `<html><body><h1>长章节</h1><p>` +
		strings.Repeat("证据内容", 400) +
		`</p></body></html>`
	book := BookKnowledgeBook{
		BookID:        "citation-book",
		Title:         "引用迁移",
		SourceHTML:    "https://example.test/book",
		SourceType:    "dedao_ebook",
		SourceAccount: "publisher",
		SourceKey:     "book-key",
		PublishedAt:   "2026-07-26T00:00:00Z",
	}

	pkg, err := ExtractBookKnowledgeFromHTML(book, html)
	if err != nil {
		t.Fatalf("ExtractBookKnowledgeFromHTML returned error: %v", err)
	}
	if len(pkg.Chunks) < 2 {
		t.Fatalf("chunks = %d, want multiple chunks", len(pkg.Chunks))
	}
	if len(pkg.Citations) != len(pkg.Chunks) {
		t.Fatalf("citations = %d, chunks = %d", len(pkg.Citations), len(pkg.Chunks))
	}
	seen := make(map[string]bool, len(pkg.Citations))
	for index, citation := range pkg.Citations {
		if citation.CitationID == "" || seen[citation.CitationID] {
			t.Fatalf("citation[%d] has invalid identity: %#v", index, citation)
		}
		seen[citation.CitationID] = true
		if citation.ChunkID != pkg.Chunks[index].ChunkID ||
			citation.ChapterID != pkg.Chunks[index].ChapterID ||
			citation.SourceType != book.SourceType ||
			citation.SourceAccount != book.SourceAccount ||
			citation.SourceItemKey != book.SourceKey ||
			citation.PublishedAt != book.PublishedAt {
			t.Fatalf("citation[%d] = %#v, chunk = %#v", index, citation, pkg.Chunks[index])
		}
	}
	if len(pkg.Claims) != 1 || len(pkg.Claims[0].Citations) != len(pkg.Citations) {
		t.Fatalf("chapter claim citations = %#v", pkg.Claims)
	}
	for _, citationID := range pkg.Claims[0].Citations {
		if !seen[citationID] {
			t.Fatalf("chapter claim references unknown citation %q", citationID)
		}
	}
}

func TestExtractBookKnowledgeCitationIdentityDoesNotShiftAfterEarlierInsertion(t *testing.T) {
	book := BookKnowledgeBook{BookID: "stable-book", Title: "稳定引用"}
	original, err := ExtractBookKnowledgeFromHTML(book, `<html><body>
		<h1>第一章</h1><p>原始第一段证据。</p>
		<h1>第二章</h1><p>需要保持引用身份的证据。</p>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := ExtractBookKnowledgeFromHTML(book, `<html><body>
		<h1>新增章节</h1><p>后来插入的不同证据。</p>
		<h1>第一章</h1><p>原始第一段证据。</p>
		<h1>第二章</h1><p>需要保持引用身份的证据。</p>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}

	originalID := citationIDForChunkText(t, original, "需要保持引用身份的证据。")
	insertedID := citationIDForChunkText(t, inserted, "需要保持引用身份的证据。")
	if originalID != insertedID {
		t.Fatalf("citation identity shifted after earlier insertion: %q != %q", originalID, insertedID)
	}
}

func TestExtractBookKnowledgeCitationIdentitySeparatesDuplicateTextByChapter(t *testing.T) {
	book := BookKnowledgeBook{BookID: "duplicate-book", Title: "重复正文"}
	original, err := ExtractBookKnowledgeFromHTML(book, `<html><body>
		<h1>甲章节</h1><p>跨章节重复证据。</p>
		<h1>乙章节</h1><p>跨章节重复证据。</p>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := ExtractBookKnowledgeFromHTML(book, `<html><body>
		<h1>新增章节</h1><p>跨章节重复证据。</p>
		<h1>甲章节</h1><p>跨章节重复证据。</p>
		<h1>乙章节</h1><p>跨章节重复证据。</p>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}

	for _, title := range []string{"甲章节", "乙章节"} {
		originalID := citationIDForChapterTitle(t, original, title)
		insertedID := citationIDForChapterTitle(t, inserted, title)
		if originalID != insertedID {
			t.Fatalf("%s citation shifted after duplicate insertion: %q != %q", title, originalID, insertedID)
		}
	}
}

func citationIDForChunkText(t *testing.T, pkg *BookKnowledgePackage, text string) string {
	t.Helper()
	chunkID := ""
	for _, chunk := range pkg.Chunks {
		if chunk.Text == text {
			chunkID = chunk.ChunkID
			break
		}
	}
	if chunkID == "" {
		t.Fatalf("chunk text %q not found", text)
	}
	for _, citation := range pkg.Citations {
		if citation.ChunkID == chunkID {
			return citation.CitationID
		}
	}
	t.Fatalf("citation for chunk %q not found", chunkID)
	return ""
}

func citationIDForChapterTitle(t *testing.T, pkg *BookKnowledgePackage, title string) string {
	t.Helper()
	chapterID := ""
	for _, chapter := range pkg.Chapters {
		if chapter.Title == title {
			chapterID = chapter.ChapterID
			break
		}
	}
	if chapterID == "" {
		t.Fatalf("chapter title %q not found", title)
	}
	for _, citation := range pkg.Citations {
		if citation.ChapterID == chapterID {
			return citation.CitationID
		}
	}
	t.Fatalf("citation for chapter %q not found", title)
	return ""
}
