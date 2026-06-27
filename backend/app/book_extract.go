package app

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const bookKnowledgeMaxChunkRunes = 1200

var whitespaceRegexp = regexp.MustCompile(`\s+`)

type extractedBookBlock struct {
	Kind string
	Text string
}

func BuildBookKnowledgeFromHTMLFile(book BookKnowledgeBook, htmlPath string, store *BookKnowledgeStore) (*BookKnowledgePackage, error) {
	if strings.TrimSpace(htmlPath) == "" {
		return nil, fmt.Errorf("html path is required")
	}
	content, err := os.ReadFile(htmlPath)
	if err != nil {
		return nil, err
	}
	book.SourceHTML = htmlPath
	pkg, err := ExtractBookKnowledgeFromHTML(book, string(content))
	if err != nil {
		return nil, err
	}
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if err := store.SavePackage(*pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func ExtractBookKnowledgeFromHTML(book BookKnowledgeBook, htmlContent string) (*BookKnowledgePackage, error) {
	if strings.TrimSpace(book.BookID) == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}
	doc.Find("script, style, noscript, svg").Remove()

	blocks := extractBookBlocks(doc)
	if len(blocks) == 0 {
		text := normalizeBookText(doc.Find("body").Text())
		if text == "" {
			text = normalizeBookText(doc.Text())
		}
		if text == "" {
			return nil, fmt.Errorf("book html contains no readable text: book_id=%s", book.BookID)
		}
		blocks = []extractedBookBlock{{Kind: "paragraph", Text: text}}
	}

	now := time.Now().Format(time.RFC3339)
	if strings.TrimSpace(book.Status) == "" {
		book.Status = "draft"
	}
	if strings.TrimSpace(book.Extractor) == "" {
		book.Extractor = defaultBookKnowledgeExtractor
	}
	if strings.TrimSpace(book.CreatedAt) == "" {
		book.CreatedAt = now
	}
	book.UpdatedAt = now

	builder := bookKnowledgePackageBuilder{book: book}
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		if block.Kind == "heading" {
			builder.startChapter(block.Text)
			continue
		}
		builder.addParagraph(block.Text)
	}
	return builder.build(), nil
}

func extractBookBlocks(doc *goquery.Document) []extractedBookBlock {
	selector := "h1,h2,h3,h4,h5,h6,div[class^='header'],p"
	var blocks []extractedBookBlock
	doc.Find(selector).Each(func(_ int, selection *goquery.Selection) {
		if selectionIsHeader(selection) && selection.Find("h1,h2,h3,h4,h5,h6").Length() > 0 {
			return
		}
		text := normalizeBookText(selection.Text())
		if text == "" || text == "目 录" {
			return
		}
		tag := goquery.NodeName(selection)
		kind := "paragraph"
		if strings.HasPrefix(tag, "h") || selectionIsHeader(selection) {
			kind = "heading"
		}
		blocks = append(blocks, extractedBookBlock{Kind: kind, Text: text})
	})
	return blocks
}

func selectionIsHeader(selection *goquery.Selection) bool {
	className, _ := selection.Attr("class")
	for _, item := range strings.Fields(className) {
		if strings.HasPrefix(item, "header") {
			return true
		}
	}
	return false
}

type bookKnowledgePackageBuilder struct {
	book          BookKnowledgeBook
	chapters      []BookKnowledgeChapter
	chunks        []BookKnowledgeChunk
	claims        []BookKnowledgeClaim
	citations     []BookKnowledgeCitation
	current       *BookKnowledgeChapter
	currentText   []string
	chapterNumber int
	chunkNumber   int
}

func (b *bookKnowledgePackageBuilder) startChapter(title string) {
	b.flushCurrentChapter()
	b.chapterNumber++
	chapterID := b.book.BookID + "-chapter-" + strconv.Itoa(b.chapterNumber)
	chapter := BookKnowledgeChapter{
		ChapterID: chapterID,
		BookID:    b.book.BookID,
		Order:     b.chapterNumber,
		Title:     title,
		ChunkIDs:  []string{},
	}
	b.chapters = append(b.chapters, chapter)
	b.current = &b.chapters[len(b.chapters)-1]
	b.currentText = nil
}

func (b *bookKnowledgePackageBuilder) addParagraph(text string) {
	if b.current == nil {
		b.startChapter("正文")
	}
	b.currentText = append(b.currentText, text)
}

func (b *bookKnowledgePackageBuilder) flushCurrentChapter() {
	if b.current == nil {
		return
	}
	paragraphs := append([]string(nil), b.currentText...)
	if len(paragraphs) == 0 {
		return
	}
	chapterText := strings.Join(paragraphs, "\n\n")
	parts := splitBookKnowledgeText(chapterText, bookKnowledgeMaxChunkRunes)
	for _, part := range parts {
		b.chunkNumber++
		chunkID := b.book.BookID + "-chunk-" + strconv.Itoa(b.chunkNumber)
		chunk := BookKnowledgeChunk{
			ChunkID:   chunkID,
			BookID:    b.book.BookID,
			ChapterID: b.current.ChapterID,
			Order:     b.chunkNumber,
			Text:      part,
			Tokens:    estimateBookTokens(part),
		}
		b.chunks = append(b.chunks, chunk)
		b.current.ChunkIDs = append(b.current.ChunkIDs, chunkID)
	}

	summary := trimRunes(chapterText, 240)
	b.current.Summary = summary
	if len(b.current.ChunkIDs) > 0 {
		citationID := b.book.BookID + "-citation-" + strconv.Itoa(len(b.citations)+1)
		b.citations = append(b.citations, BookKnowledgeCitation{
			CitationID: citationID,
			BookID:     b.book.BookID,
			ChapterID:  b.current.ChapterID,
			ChunkID:    b.current.ChunkIDs[0],
			SourceHTML: b.book.SourceHTML,
			Anchor:     b.current.Title,
			Note:       "自动提取，待人工复核",
		})
		b.claims = append(b.claims, BookKnowledgeClaim{
			ClaimID:       b.book.BookID + "-claim-" + strconv.Itoa(len(b.claims)+1),
			BookID:        b.book.BookID,
			ChapterID:     b.current.ChapterID,
			Title:         b.current.Title,
			Summary:       summary,
			Body:          summary,
			EvidenceLevel: "D",
			Confidence:    0.4,
			ReviewStatus:  "draft",
			Citations:     []string{citationID},
		})
	}
	b.currentText = nil
}

func (b *bookKnowledgePackageBuilder) build() *BookKnowledgePackage {
	b.flushCurrentChapter()
	if len(b.chapters) == 0 {
		b.startChapter("正文")
	}
	return &BookKnowledgePackage{
		Book:      b.book,
		Chapters:  b.chapters,
		Chunks:    b.chunks,
		Claims:    b.claims,
		Citations: b.citations,
	}
}

func splitBookKnowledgeText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}
	var parts []string
	for len(runes) > 0 {
		end := maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		part := strings.TrimSpace(string(runes[:end]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = runes[end:]
	}
	return parts
}

func estimateBookTokens(text string) int {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}
	return len(runes) / 2
}

func normalizeBookText(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = whitespaceRegexp.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func trimRunes(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "..."
}
