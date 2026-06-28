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

func TestExtractBookKnowledgeFromMarkdown(t *testing.T) {
	pkg, err := ExtractBookKnowledgeFromMarkdown(BookKnowledgeBook{
		BookID:    "odob-301",
		Title:     "每天听本书",
		Extractor: "dedao-gui-odob-transcript",
	}, "# 学习闭环\n\n- 输入\n- 输出\n- 复盘\n\n## 行动建议\n\n每天记录一个问题。")
	if err != nil {
		t.Fatalf("ExtractBookKnowledgeFromMarkdown returned error: %v", err)
	}
	if pkg.Book.BookID != "odob-301" || pkg.Book.Extractor != "dedao-gui-odob-transcript" {
		t.Fatalf("book = %#v", pkg.Book)
	}
	if len(pkg.Chapters) != 2 || pkg.Chapters[0].Title != "学习闭环" || pkg.Chapters[1].Title != "行动建议" {
		t.Fatalf("chapters = %#v", pkg.Chapters)
	}
	if len(pkg.Chunks) != 2 || !strings.Contains(pkg.Chunks[0].Text, "输入") {
		t.Fatalf("chunks = %#v", pkg.Chunks)
	}
	if len(pkg.Claims) != 2 || pkg.Claims[0].ReviewStatus != "draft" {
		t.Fatalf("claims = %#v", pkg.Claims)
	}
}
