package app

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type KnowledgeFeedQuery struct {
	After        string
	Limit        int
	Source       string
	UsagePolicy  string
	ChangedSince string
	BookID       string
}

func BuildKnowledgeFeedPage(store *BookKnowledgeStore, query KnowledgeFeedQuery) (KnowledgeFeedPage, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 50
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return KnowledgeFeedPage{}, err
	}
	filtered := make([]KnowledgeReleaseRecord, 0, len(manifest.Releases))
	for _, record := range manifest.Releases {
		if query.BookID != "" && record.BookID != query.BookID {
			continue
		}
		if query.UsagePolicy != "" && record.UsagePolicy != query.UsagePolicy {
			continue
		}
		if query.ChangedSince != "" && record.CreatedAt <= query.ChangedSince {
			continue
		}
		if query.Source != "" {
			release, err := store.LoadKnowledgeRelease(record.ReleaseID)
			if err != nil {
				return KnowledgeFeedPage{}, err
			}
			if release.Book.SourceType != query.Source {
				continue
			}
		}
		filtered = append(filtered, record)
	}
	start := 0
	if query.After != "" {
		start = -1
		for index, record := range filtered {
			if record.ReleaseID == query.After {
				start = index + 1
				break
			}
		}
		if start < 0 {
			return KnowledgeFeedPage{}, fmt.Errorf("invalid cursor")
		}
	}
	end := start + query.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	items := make([]KnowledgeFeedItem, 0, end-start)
	for _, record := range filtered[start:end] {
		items = append(items, KnowledgeFeedItem{
			ReleaseID:   record.ReleaseID,
			BookID:      record.BookID,
			ContentHash: record.ContentHash,
			Supersedes:  record.Supersedes,
			UsagePolicy: record.UsagePolicy,
			CreatedAt:   record.CreatedAt,
			URL:         "/api/knowledge/releases/" + url.PathEscape(record.ReleaseID),
		})
	}
	nextCursor := ""
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ReleaseID
	}
	return KnowledgeFeedPage{
		SchemaVersion: KnowledgeFeedSchemaVersion,
		Items:         items,
		NextCursor:    nextCursor,
		HasMore:       end < len(filtered),
	}, nil
}

func parseKnowledgeFeedQuery(values url.Values) KnowledgeFeedQuery {
	limit, _ := strconv.Atoi(values.Get("limit"))
	return KnowledgeFeedQuery{
		After:        strings.TrimSpace(values.Get("after")),
		Limit:        limit,
		Source:       strings.TrimSpace(values.Get("source")),
		UsagePolicy:  strings.TrimSpace(values.Get("policy")),
		ChangedSince: strings.TrimSpace(values.Get("changed_since")),
		BookID:       strings.TrimSpace(values.Get("book_id")),
	}
}
