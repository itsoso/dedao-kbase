package app

import (
	"encoding/json"
	"testing"
)

func TestBookKnowledgeMCPListAndSearch(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(BookKnowledgePackage{
		Book: BookKnowledgeBook{BookID: "42", Title: "42_量化分析_作者", Status: "draft"},
		Chapters: []BookKnowledgeChapter{
			{ChapterID: "42-chapter-1", BookID: "42", Order: 1, Title: "趋势过滤"},
		},
		Chunks: []BookKnowledgeChunk{
			{ChunkID: "42-chunk-1", BookID: "42", ChapterID: "42-chapter-1", Text: "MACD 背离需要趋势过滤。"},
		},
	}); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}

	server := NewBookKnowledgeMCPServer(store)
	tools := server.Tools()
	if len(tools) == 0 {
		t.Fatal("expected MCP tools")
	}

	listResp, err := server.Call("book.list_books", nil)
	if err != nil {
		t.Fatalf("book.list_books returned error: %v", err)
	}
	var books []BookKnowledgeBook
	if err := json.Unmarshal(listResp, &books); err != nil {
		t.Fatalf("list response is not books JSON: %v", err)
	}
	if len(books) != 1 || books[0].BookID != "42" {
		t.Fatalf("books = %#v, want saved book", books)
	}

	searchResp, err := server.Call("book.search", json.RawMessage(`{"query":"MACD","limit":5}`))
	if err != nil {
		t.Fatalf("book.search returned error: %v", err)
	}
	var results []BookKnowledgeSearchResult
	if err := json.Unmarshal(searchResp, &results); err != nil {
		t.Fatalf("search response is not results JSON: %v", err)
	}
	if len(results) != 1 || results[0].ChunkID != "42-chunk-1" {
		t.Fatalf("results = %#v, want matching chunk", results)
	}
}

func TestBookKnowledgeMCPRejectsUnknownTool(t *testing.T) {
	server := NewBookKnowledgeMCPServer(NewBookKnowledgeStore(t.TempDir()))

	if _, err := server.Call("unknown.tool", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
}
