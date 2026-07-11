package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SourceAgentRunnerConfig struct {
	Client        *SourceAgentClient
	Outbox        *SourceAgentOutbox
	Adapter       SourceAdapter
	Version       string
	LeaseDuration time.Duration
}

type SourceAgentRunner struct {
	client        *SourceAgentClient
	outbox        *SourceAgentOutbox
	adapter       SourceAdapter
	version       string
	leaseDuration time.Duration
}

type SourceAgentCycleResult struct {
	OK              bool   `json:"ok"`
	RunID           string `json:"run_id,omitempty"`
	Status          string `json:"status,omitempty"`
	Uploaded        int    `json:"uploaded"`
	OutboxRemaining int    `json:"outbox_remaining"`
}

func NewSourceAgentRunner(config SourceAgentRunnerConfig) (*SourceAgentRunner, error) {
	if config.Client == nil || config.Outbox == nil || config.Adapter == nil {
		return nil, fmt.Errorf("source agent client, outbox, and adapter are required")
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	return &SourceAgentRunner{client: config.Client, outbox: config.Outbox, adapter: config.Adapter, version: strings.TrimSpace(config.Version), leaseDuration: config.LeaseDuration}, nil
}

func (r *SourceAgentRunner) RunOnce(ctx context.Context) (SourceAgentCycleResult, error) {
	result := SourceAgentCycleResult{OK: true}
	health := r.adapter.Status(ctx)
	if _, err := r.client.Heartbeat(ctx, SourceAgentHeartbeat{Version: r.version, Capabilities: r.adapter.Operations(), CapabilityHealth: map[string]SourceCapabilityHealth{r.adapter.Name(): health}}); err != nil {
		return result, fmt.Errorf("send source-agent heartbeat: %w", err)
	}
	if !health.Healthy {
		return result, nil
	}
	run, err := r.client.Lease(ctx, r.adapter.Operations(), r.leaseDuration)
	if err != nil {
		return result, fmt.Errorf("lease source sync run: %w", err)
	}
	if run == nil {
		return result, nil
	}
	result.RunID, result.Status = run.ID, run.Status
	if run.Subscription == nil {
		return result, r.failRun(ctx, run.ID, fmt.Errorf("leased run %s is missing its subscription snapshot", run.ID))
	}
	uploaded, err := r.flush(ctx, run.ID)
	result.Uploaded += uploaded
	if err != nil {
		return result, err
	}
	adapterResult, err := r.adapter.Execute(ctx, *run, r.outbox)
	if err != nil {
		if sourceAgentRequestRetryable(err) {
			return result, err
		}
		return result, r.failRun(ctx, run.ID, err)
	}
	uploaded, err = r.flush(ctx, run.ID)
	result.Uploaded += uploaded
	if err != nil {
		return result, err
	}
	pending, err := r.outbox.CountPendingForRun(run.ID)
	if err != nil {
		return result, err
	}
	result.OutboxRemaining = pending
	if pending != 0 {
		return result, fmt.Errorf("run %s still has %d pending outbox items", run.ID, pending)
	}
	completed, err := r.client.CompleteRun(ctx, run.ID, adapterResult.Cursor)
	if err != nil {
		return result, err
	}
	result.Status = completed.Status
	result.OK = completed.Status == SourceRunSucceeded || completed.Status == SourceRunPartial
	return result, nil
}

func (r *SourceAgentRunner) flush(ctx context.Context, runID string) (int, error) {
	items, err := r.outbox.PeekReadyForRun(runID, 100)
	if err != nil {
		return 0, err
	}
	uploaded := 0
	for _, item := range items {
		if _, err := r.client.UploadArticle(ctx, runID, item.Envelope); err != nil {
			status := 0
			var httpErr *SourceAgentHTTPError
			if errors.As(err, &httpErr) {
				status = httpErr.StatusCode
			}
			updated, recordErr := r.outbox.RecordFailure(item.ID, status, err)
			if recordErr != nil {
				return uploaded, recordErr
			}
			if status == http.StatusBadRequest && updated.State == SourceOutboxDead {
				continue
			}
			return uploaded, fmt.Errorf("upload source article: %w", err)
		}
		if err := r.outbox.Acknowledge(item.ID); err != nil {
			return uploaded, err
		}
		uploaded++
	}
	return uploaded, nil
}

func (r *SourceAgentRunner) failRun(ctx context.Context, runID string, cause error) error {
	if _, err := r.client.FailRun(ctx, runID, cause.Error()); err != nil {
		return fmt.Errorf("%w; report run failure: %v", cause, err)
	}
	return cause
}
