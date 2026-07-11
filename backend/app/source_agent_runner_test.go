package app

import (
	"context"
	"testing"
	"time"
)

type fakeSourceAdapter struct {
	status SourceCapabilityHealth
	run    SourceSyncRun
	called bool
}

func (a *fakeSourceAdapter) Name() string                                  { return "fake" }
func (a *fakeSourceAdapter) Operations() []string                          { return []string{"sync_fake"} }
func (a *fakeSourceAdapter) Status(context.Context) SourceCapabilityHealth { return a.status }
func (a *fakeSourceAdapter) Execute(_ context.Context, run SourceSyncRun, _ SourceEnvelopeSink) (SourceAdapterResult, error) {
	a.called = true
	a.run = run
	return SourceAdapterResult{Cursor: "cursor-2"}, nil
}

func TestSourceAgentRunnerRejectsMissingDependencies(t *testing.T) {
	_, err := NewSourceAgentRunner(SourceAgentRunnerConfig{Adapter: &fakeSourceAdapter{}, LeaseDuration: time.Minute})
	if err == nil {
		t.Fatal("NewSourceAgentRunner succeeded without client and outbox")
	}
}

func TestSourceAgentRunnerUsesAdapterContract(t *testing.T) {
	var _ SourceAdapter = (*fakeSourceAdapter)(nil)
	var _ SourceEnvelopeSink = (*SourceAgentOutbox)(nil)
}
