package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeEvolutionEvaluationWorkerClient struct {
	work        *EvolutionWork
	result      *EvolutionEvaluationResult
	evaluateErr error
	heartbeats  int
	failedCodes []string
}

func (fake *fakeEvolutionEvaluationWorkerClient) Heartbeat(context.Context, EvolutionWorkerCapability, string, string) (SourceAgent, error) {
	fake.heartbeats++
	return SourceAgent{}, nil
}
func (fake *fakeEvolutionEvaluationWorkerClient) Lease(context.Context, []EvolutionWorkerCapability, time.Duration) (*EvolutionWork, error) {
	work := fake.work
	fake.work = nil
	return work, nil
}
func (fake *fakeEvolutionEvaluationWorkerClient) Renew(context.Context, EvolutionWork, time.Duration) (*EvolutionWork, error) {
	return fake.work, nil
}
func (fake *fakeEvolutionEvaluationWorkerClient) Evaluate(context.Context, EvolutionWork) (*EvolutionEvaluationResult, error) {
	return fake.result, fake.evaluateErr
}
func (fake *fakeEvolutionEvaluationWorkerClient) Fail(_ context.Context, _ EvolutionWork, code, _ string, _ time.Duration) (*EvolutionWork, error) {
	fake.failedCodes = append(fake.failedCodes, code)
	return &EvolutionWork{}, nil
}

func TestEvolutionEvaluationWorkerFinalizesScorecard(t *testing.T) {
	fake := &fakeEvolutionEvaluationWorkerClient{
		work:   &EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1},
		result: &EvolutionEvaluationResult{Scorecard: &EvolutionScorecard{ScorecardID: "sha256:" + workerHex('a')}, RunStatus: EvolutionAwaitingApproval},
	}
	worker, err := NewEvolutionEvaluationWorker(EvolutionEvaluationWorkerConfig{
		Client: fake, Version: "1.0.0", LeaseDuration: time.Minute,
		RenewInterval: 20 * time.Second, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || fake.heartbeats != 1 || len(fake.failedCodes) != 0 {
		t.Fatalf("processed=%v err=%v heartbeat=%d failures=%v", processed, err, fake.heartbeats, fake.failedCodes)
	}
}

func TestEvolutionEvaluationWorkerReportsBoundedFailure(t *testing.T) {
	fake := &fakeEvolutionEvaluationWorkerClient{
		work:        &EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1},
		evaluateErr: errors.New("private evaluation detail"),
	}
	worker, err := NewEvolutionEvaluationWorker(EvolutionEvaluationWorkerConfig{
		Client: fake, Version: "1.0.0", LeaseDuration: time.Minute,
		RenewInterval: 20 * time.Second, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || len(fake.failedCodes) != 1 || fake.failedCodes[0] != "evaluation_failed" {
		t.Fatalf("processed=%v err=%v failures=%v", processed, err, fake.failedCodes)
	}
}
