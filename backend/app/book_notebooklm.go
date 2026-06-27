package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const NotebookLMHomeURL = "https://notebooklm.google.com/"

type BookKnowledgeNotebookLMBridge struct {
	BookID          string   `json:"book_id"`
	NotebookURL     string   `json:"notebook_url,omitempty"`
	LastExportDir   string   `json:"last_export_dir,omitempty"`
	LastExportFiles []string `json:"last_export_files,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
}

func ExportNotebookLMBridgePackage(store *BookKnowledgeStore, bookID string) (*BookKnowledgeNotebookLMBridge, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	result, err := ExportBookKnowledgePackage(store, bookID, BookKnowledgeExportNotebookLMBridge)
	if err != nil {
		return nil, err
	}
	bridge, err := store.LoadNotebookLMBridge(result.BookID)
	if err != nil {
		return nil, err
	}
	bridge.LastExportDir = result.OutputDir
	bridge.LastExportFiles = result.Files
	bridge.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := store.saveNotebookLMBridge(*bridge); err != nil {
		return nil, err
	}
	return bridge, nil
}

func (s *BookKnowledgeStore) NotebookLMBridgePath(bookID string) string {
	return filepath.Join(s.BookDir(bookID), "notebooklm.json")
}

func (s *BookKnowledgeStore) LoadNotebookLMBridge(bookID string) (*BookKnowledgeNotebookLMBridge, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	bookID = sanitizeBookKnowledgeID(bookID)
	if bookID == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	bridge := &BookKnowledgeNotebookLMBridge{BookID: bookID}
	path := s.NotebookLMBridgePath(bookID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return bridge, nil
		}
		return nil, err
	}
	if err := readJSONFile(path, bridge); err != nil {
		return nil, err
	}
	if strings.TrimSpace(bridge.BookID) == "" {
		bridge.BookID = bookID
	}
	return bridge, nil
}

func (s *BookKnowledgeStore) SaveNotebookLMLink(bookID, notebookURL string) (*BookKnowledgeNotebookLMBridge, error) {
	if s == nil {
		s = DefaultBookKnowledgeStore()
	}
	bookID = sanitizeBookKnowledgeID(bookID)
	if bookID == "" {
		return nil, fmt.Errorf("book_id is required")
	}
	if _, err := s.LoadPackage(bookID); err != nil {
		return nil, err
	}
	notebookURL = strings.TrimSpace(notebookURL)
	if notebookURL != "" {
		if err := validateNotebookLMURL(notebookURL); err != nil {
			return nil, err
		}
	}
	bridge, err := s.LoadNotebookLMBridge(bookID)
	if err != nil {
		return nil, err
	}
	bridge.NotebookURL = notebookURL
	bridge.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := s.saveNotebookLMBridge(*bridge); err != nil {
		return nil, err
	}
	return bridge, nil
}

func (s *BookKnowledgeStore) saveNotebookLMBridge(bridge BookKnowledgeNotebookLMBridge) error {
	bridge.BookID = sanitizeBookKnowledgeID(bridge.BookID)
	if bridge.BookID == "" {
		return fmt.Errorf("book_id is required")
	}
	if strings.TrimSpace(bridge.UpdatedAt) == "" {
		bridge.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	if err := os.MkdirAll(s.BookDir(bridge.BookID), os.ModePerm); err != nil {
		return err
	}
	return writeJSONFile(s.NotebookLMBridgePath(bridge.BookID), bridge)
}

func validateNotebookLMURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("notebook url must be a valid http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("notebook url must be a valid http(s) URL")
	}
	return nil
}

func exportNotebookLMBridge(pkg *BookKnowledgePackage, outputDir string) ([]string, error) {
	files := []string{
		filepath.Join(outputDir, "book.md"),
		filepath.Join(outputDir, "claims.md"),
		filepath.Join(outputDir, "notebooklm-prompt.md"),
		filepath.Join(outputDir, "upload-guide.md"),
	}
	contents := []string{
		buildNotebookLMBookMarkdown(pkg),
		buildNotebookLMClaimsMarkdown(pkg),
		buildNotebookLMPromptMarkdown(pkg),
		buildNotebookLMUploadGuideMarkdown(pkg),
	}
	for i, path := range files {
		if err := writeTextFile(path, contents[i]); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func buildNotebookLMBookMarkdown(pkg *BookKnowledgePackage) string {
	var builder strings.Builder
	writeMarkdownTitle(&builder, 1, pkg.Book.Title)
	builder.WriteString("\n")
	writeMarkdownKV(&builder, "book_id", pkg.Book.BookID)
	if pkg.Book.Author != "" {
		writeMarkdownKV(&builder, "author", pkg.Book.Author)
	}
	if pkg.Book.SourceHTML != "" {
		writeMarkdownKV(&builder, "source_html", pkg.Book.SourceHTML)
	}
	builder.WriteString("\n")

	chunksByChapter := map[string][]BookKnowledgeChunk{}
	for _, chunk := range pkg.Chunks {
		chunksByChapter[chunk.ChapterID] = append(chunksByChapter[chunk.ChapterID], chunk)
	}
	for chapterID := range chunksByChapter {
		sort.SliceStable(chunksByChapter[chapterID], func(i, j int) bool {
			return chunksByChapter[chapterID][i].Order < chunksByChapter[chapterID][j].Order
		})
	}

	writeMarkdownTitle(&builder, 2, "章节内容")
	for _, chapter := range pkg.Chapters {
		builder.WriteString("\n")
		writeMarkdownTitle(&builder, 3, strconv.Itoa(chapter.Order)+". "+chapter.Title)
		if chapter.Summary != "" {
			builder.WriteString("\n")
			builder.WriteString(chapter.Summary)
			builder.WriteString("\n")
		}
		for _, chunk := range chunksByChapter[chapter.ChapterID] {
			builder.WriteString("\n")
			builder.WriteString("#### ")
			builder.WriteString(chunk.ChunkID)
			builder.WriteString("\n\n")
			builder.WriteString(chunk.Text)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func buildNotebookLMClaimsMarkdown(pkg *BookKnowledgePackage) string {
	var builder strings.Builder
	writeMarkdownTitle(&builder, 1, pkg.Book.Title+" - Claims")
	builder.WriteString("\n| Claim ID | Title | Summary | Evidence | Status |\n")
	builder.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, claim := range pkg.Claims {
		builder.WriteString("| ")
		builder.WriteString(markdownTableCell(claim.ClaimID))
		builder.WriteString(" | ")
		builder.WriteString(markdownTableCell(claim.Title))
		builder.WriteString(" | ")
		builder.WriteString(markdownTableCell(firstNonEmpty(claim.Summary, claim.Body)))
		builder.WriteString(" | ")
		builder.WriteString(markdownTableCell(strings.Join(claim.Citations, ", ")))
		builder.WriteString(" | ")
		builder.WriteString(markdownTableCell(firstNonEmpty(claim.ReviewStatus, "draft")))
		builder.WriteString(" |\n")
	}
	return builder.String()
}

func buildNotebookLMPromptMarkdown(pkg *BookKnowledgePackage) string {
	var builder strings.Builder
	writeMarkdownTitle(&builder, 1, "NotebookLM Bridge")
	builder.WriteString("\n")
	builder.WriteString("Book: ")
	builder.WriteString(pkg.Book.Title)
	builder.WriteString("\n\n")
	builder.WriteString("上传 `book.md` 和 `claims.md` 后，可以使用这些问题：\n\n")
	builder.WriteString("- 总结本书的核心结论，并按章节给出依据。\n")
	builder.WriteString("- 提取最值得落地的 10 条行动建议，标注来源 claim 或 chunk。\n")
	builder.WriteString("- 找出书中可能冲突、证据不足或需要二次验证的观点。\n")
	builder.WriteString("- 把本书转换成可执行规则卡或项目知识库条目。\n")
	return builder.String()
}

func buildNotebookLMUploadGuideMarkdown(pkg *BookKnowledgePackage) string {
	var builder strings.Builder
	writeMarkdownTitle(&builder, 1, "上传到 NotebookLM")
	builder.WriteString("\n")
	builder.WriteString("Book: ")
	builder.WriteString(pkg.Book.Title)
	builder.WriteString("\n\n")
	builder.WriteString("## 上传文件\n\n")
	builder.WriteString("1. 打开 https://notebooklm.google.com/ 并创建新 notebook。\n")
	builder.WriteString("2. 上传 `book.md` 作为主资料源。\n")
	builder.WriteString("3. 上传 `claims.md` 作为观点与证据索引。\n")
	builder.WriteString("4. 打开 `notebooklm-prompt.md`，复制其中的问题到 NotebookLM 对话框。\n")
	builder.WriteString("5. 回到 dedao-gui 的 NotebookLM tab，保存这个 notebook 的链接。\n")
	builder.WriteString("\n## 推荐上传顺序\n\n")
	builder.WriteString("- `book.md`\n")
	builder.WriteString("- `claims.md`\n")
	builder.WriteString("- `notebooklm-prompt.md` 仅作为提示词参考，不必上传。\n")
	return builder.String()
}

func writeMarkdownTitle(builder *strings.Builder, level int, title string) {
	if level < 1 {
		level = 1
	}
	builder.WriteString(strings.Repeat("#", level))
	builder.WriteString(" ")
	builder.WriteString(strings.TrimSpace(title))
	builder.WriteString("\n")
}

func writeMarkdownKV(builder *strings.Builder, key, value string) {
	builder.WriteString("- ")
	builder.WriteString(key)
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteString("\n")
}

func markdownTableCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
}

func writeTextFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
