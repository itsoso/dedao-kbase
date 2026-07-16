package app

import "testing"

func TestKnowledgeSearchIndexRebuildsAndFiltersPackages(t *testing.T) {
	root := t.TempDir()
	bookStore := NewBookKnowledgeStore(root)
	packages := []BookKnowledgePackage{
		{
			Book: BookKnowledgeBook{
				BookID:      "source-a",
				Title:       "疫苗证据综述",
				SourceType:  "wechat_mp_article",
				SourceKey:   "article-a",
				ContentHash: "hash-a",
				PublishedAt: "2026-07-15T10:00:00Z",
				UpdatedAt:   "2026-07-15T11:00:00Z",
				Status:      "ready",
			},
			Chapters: []BookKnowledgeChapter{{ChapterID: "source-a-chapter-1", BookID: "source-a", Order: 1, Title: "证据"}},
			Chunks: []BookKnowledgeChunk{
				{ChunkID: "source-a-chunk-1", BookID: "source-a", ChapterID: "source-a-chapter-1", Order: 1, Text: "疫苗证据需要追踪试验设计、终点和安全性。"},
			},
			Claims: []BookKnowledgeClaim{
				{ClaimID: "source-a-claim-1", BookID: "source-a", ChapterID: "source-a-chapter-1", Title: "证据审计", Summary: "疫苗结论必须绑定 citation。"},
			},
		},
		{
			Book: BookKnowledgeBook{
				BookID:      "source-b",
				Title:       "课程学习方法",
				SourceType:  "dedao_course",
				SourceKey:   "course-b",
				ContentHash: "hash-b",
				PublishedAt: "2026-07-01T10:00:00Z",
				UpdatedAt:   "2026-07-01T11:00:00Z",
				Status:      "ready",
			},
			Chapters: []BookKnowledgeChapter{{ChapterID: "source-b-chapter-1", BookID: "source-b", Order: 1, Title: "学习"}},
			Chunks: []BookKnowledgeChunk{
				{ChunkID: "source-b-chunk-1", BookID: "source-b", ChapterID: "source-b-chapter-1", Order: 1, Text: "课程证据来自讲次、案例和练习反馈。"},
			},
		},
	}
	for _, pkg := range packages {
		if err := bookStore.SavePackage(pkg); err != nil {
			t.Fatalf("save package %s: %v", pkg.Book.BookID, err)
		}
	}
	index, err := NewKnowledgeSearchIndex(root)
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	defer index.Close()

	rebuilt, err := index.RebuildFromBookStore(bookStore)
	if err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	if rebuilt != 3 {
		t.Fatalf("rebuilt %d records, want 3", rebuilt)
	}
	results, err := index.Search(KnowledgeSearchIndexQuery{Query: "疫苗 证据", SourceType: "wechat_mp_article", Limit: 10})
	if err != nil {
		t.Fatalf("search index: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v, want chunk and claim", results)
	}
	if results[0].BookID != "source-a" || results[0].SourceType != "wechat_mp_article" || results[0].ContentHash != "hash-a" {
		t.Fatalf("unexpected first result: %#v", results[0])
	}
	fresh, err := index.Search(KnowledgeSearchIndexQuery{Query: "证据", UpdatedAfter: "2026-07-10T00:00:00Z", Limit: 10})
	if err != nil {
		t.Fatalf("fresh search: %v", err)
	}
	if len(fresh) != 2 || fresh[0].BookID != "source-a" {
		t.Fatalf("fresh results = %#v", fresh)
	}
}
