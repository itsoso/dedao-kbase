package app

import (
	"context"
	"fmt"

	jsoniter "github.com/json-iterator/go"
	"github.com/yann0917/dedao-gui/backend/services"
)

type OdobWikiSyncResult struct {
	KnowledgeBookID   string
	Title             string
	Chapters          int
	Chunks            int
	Claims            int
	BookKnowledgeRoot string
}

func SyncOdobToWikiStore(ctx context.Context, store *BookKnowledgeStore, job BookKnowledgeJob) (*OdobWikiSyncResult, error) {
	_ = ctx
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	detail, err := OdobArticleDetail(job.OdobAliasID)
	if err != nil {
		return nil, err
	}
	var content []services.Content
	if err := jsoniter.UnmarshalFromString(detail.Content, &content); err != nil {
		return nil, err
	}
	markdown := ContentsToMarkdown(content)
	title := firstNonEmpty(job.OdobTitle, fmt.Sprintf("听书 %d", job.OdobID))
	bookID := fmt.Sprintf("odob-%d", job.OdobID)
	pkg, err := BuildBookKnowledgeFromMarkdown(BookKnowledgeBook{
		BookID:     bookID,
		DedaoID:    job.OdobID,
		EnID:       job.OdobEnID,
		Title:      title,
		Author:     "得到听书",
		SourceHTML: "dedao-odob:" + job.OdobAliasID,
		Extractor:  "dedao-gui-odob-transcript",
		Status:     "draft",
	}, markdown, store)
	if err != nil {
		return nil, err
	}
	return &OdobWikiSyncResult{
		KnowledgeBookID:   pkg.Book.BookID,
		Title:             pkg.Book.Title,
		Chapters:          len(pkg.Chapters),
		Chunks:            len(pkg.Chunks),
		Claims:            len(pkg.Claims),
		BookKnowledgeRoot: store.Root(),
	}, nil
}
