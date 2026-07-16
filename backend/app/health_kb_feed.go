package app

import "net/url"

func BuildHealthKnowledgeFeedPage(store *BookKnowledgeStore, values url.Values) (KnowledgeFeedPage, error) {
	query := parseKnowledgeFeedQuery(values)
	query.UsagePolicy = BookUsageEvidenceOnly
	return BuildKnowledgeFeedPage(store, query)
}
