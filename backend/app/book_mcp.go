package app

import (
	"encoding/json"
	"fmt"
)

type BookKnowledgeMCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type BookKnowledgeMCPServer struct {
	store *BookKnowledgeStore
}

func NewBookKnowledgeMCPServer(store *BookKnowledgeStore) *BookKnowledgeMCPServer {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	return &BookKnowledgeMCPServer{store: store}
}

func (s *BookKnowledgeMCPServer) Tools() []BookKnowledgeMCPTool {
	return []BookKnowledgeMCPTool{
		{
			Name:        "book.list_books",
			Description: "列出 dedao-gui 已提取的本地书籍知识包。",
			InputSchema: objectSchema(map[string]any{}),
		},
		{
			Name:        "book.search",
			Description: "在本地书籍知识包的 chunks 和 claims 中检索。",
			InputSchema: objectSchema(map[string]any{
				"query":   map[string]any{"type": "string"},
				"book_id": map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer"},
			}),
		},
		{
			Name:        "book.get_chapter",
			Description: "按 book_id 和 chapter_id 读取章节、chunks、claims。",
			InputSchema: objectSchema(map[string]any{
				"book_id":    map[string]any{"type": "string"},
				"chapter_id": map[string]any{"type": "string"},
			}),
		},
		{
			Name:        "book.get_context",
			Description: "按 book_id 读取整本书的知识包上下文。",
			InputSchema: objectSchema(map[string]any{
				"book_id": map[string]any{"type": "string"},
			}),
		},
	}
}

func (s *BookKnowledgeMCPServer) Call(name string, arguments json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "book.list_books":
		books, err := s.store.ListBooks()
		if err != nil {
			return nil, err
		}
		return marshalMCPResult(books)
	case "book.search":
		var query BookKnowledgeSearchQuery
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &query); err != nil {
				return nil, err
			}
		}
		results, err := s.store.Search(query)
		if err != nil {
			return nil, err
		}
		return marshalMCPResult(results)
	case "book.get_chapter":
		var input struct {
			BookID    string `json:"book_id"`
			ChapterID string `json:"chapter_id"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return nil, err
		}
		result, err := s.getChapter(input.BookID, input.ChapterID)
		if err != nil {
			return nil, err
		}
		return marshalMCPResult(result)
	case "book.get_context":
		var input struct {
			BookID string `json:"book_id"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return nil, err
		}
		pkg, err := s.store.LoadPackage(input.BookID)
		if err != nil {
			return nil, err
		}
		return marshalMCPResult(pkg)
	default:
		return nil, fmt.Errorf("unknown book knowledge MCP tool: %s", name)
	}
}

func (s *BookKnowledgeMCPServer) getChapter(bookID, chapterID string) (map[string]any, error) {
	pkg, err := s.store.LoadPackage(bookID)
	if err != nil {
		return nil, err
	}
	var chapter *BookKnowledgeChapter
	for i := range pkg.Chapters {
		if pkg.Chapters[i].ChapterID == chapterID {
			chapter = &pkg.Chapters[i]
			break
		}
	}
	if chapter == nil {
		return nil, fmt.Errorf("chapter not found: %s", chapterID)
	}
	var chunks []BookKnowledgeChunk
	for _, chunk := range pkg.Chunks {
		if chunk.ChapterID == chapterID {
			chunks = append(chunks, chunk)
		}
	}
	var claims []BookKnowledgeClaim
	for _, claim := range pkg.Claims {
		if claim.ChapterID == chapterID {
			claims = append(claims, claim)
		}
	}
	return map[string]any{
		"book":    pkg.Book,
		"chapter": chapter,
		"chunks":  chunks,
		"claims":  claims,
	}, nil
}

func marshalMCPResult(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": true,
	}
}
