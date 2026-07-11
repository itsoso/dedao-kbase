package app

import (
	"context"
	"fmt"
)

type SourceEnvelopeSink interface {
	Enqueue(runID string, envelope SourceArticleEnvelope) (SourceAgentOutboxItem, error)
}

type SourceAdapterResult struct {
	Cursor string
}

type SourceAdapterExecutionError struct {
	Cursor string
	Err    error
}

func (e *SourceAdapterExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return "source adapter execution failed"
	}
	return e.Err.Error()
}

func (e *SourceAdapterExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newSourceAdapterExecutionError(cursor string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("source adapter execution failed")
	}
	return &SourceAdapterExecutionError{Cursor: cursor, Err: cause}
}

type SourceAdapter interface {
	Name() string
	Operations() []string
	Status(context.Context) SourceCapabilityHealth
	Execute(context.Context, SourceSyncRun, SourceEnvelopeSink) (SourceAdapterResult, error)
}
