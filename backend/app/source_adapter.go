package app

import "context"

type SourceEnvelopeSink interface {
	Enqueue(runID string, envelope SourceArticleEnvelope) (SourceAgentOutboxItem, error)
}

type SourceAdapterResult struct {
	Cursor string
}

type SourceAdapter interface {
	Name() string
	Operations() []string
	Status(context.Context) SourceCapabilityHealth
	Execute(context.Context, SourceSyncRun, SourceEnvelopeSink) (SourceAdapterResult, error)
}
