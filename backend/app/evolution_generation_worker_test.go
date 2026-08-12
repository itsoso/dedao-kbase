package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeEvolutionGenerationWorkerClient struct {
	work          *EvolutionWork
	result        *EvolutionGenerationResult
	generateErr   error
	heartbeats    int
	completedRefs []string
	failedCodes   []string
	deferredCodes []string
}

func (fake *fakeEvolutionGenerationWorkerClient) Heartbeat(context.Context, EvolutionWorkerCapability, string, string) (SourceAgent, error) {
	fake.heartbeats++
	return SourceAgent{AgentID: "worker-a"}, nil
}
func (fake *fakeEvolutionGenerationWorkerClient) Lease(context.Context, []EvolutionWorkerCapability, time.Duration) (*EvolutionWork, error) {
	work := fake.work
	fake.work = nil
	return work, nil
}
func (fake *fakeEvolutionGenerationWorkerClient) Renew(context.Context, EvolutionWork, time.Duration) (*EvolutionWork, error) {
	return fake.work, nil
}
func (fake *fakeEvolutionGenerationWorkerClient) Generate(context.Context, EvolutionWork) (*EvolutionGenerationResult, error) {
	return fake.result, fake.generateErr
}
func (fake *fakeEvolutionGenerationWorkerClient) Complete(_ context.Context, _ EvolutionWork, artifactRef string) (*EvolutionWork, error) {
	fake.completedRefs = append(fake.completedRefs, artifactRef)
	return &EvolutionWork{Status: EvolutionWorkCompleted}, nil
}
func (fake *fakeEvolutionGenerationWorkerClient) Fail(_ context.Context, _ EvolutionWork, code, _ string, _ time.Duration) (*EvolutionWork, error) {
	fake.failedCodes = append(fake.failedCodes, code)
	return &EvolutionWork{Status: EvolutionWorkPending}, nil
}
func (fake *fakeEvolutionGenerationWorkerClient) Defer(_ context.Context, _ EvolutionWork, code, _ string, _ time.Duration) (*EvolutionWork, error) {
	fake.deferredCodes = append(fake.deferredCodes, code)
	return &EvolutionWork{Status: EvolutionWorkPending}, nil
}

func TestEvolutionGenerationWorkerCompletesGeneratedCandidate(t *testing.T) {
	fake := &fakeEvolutionGenerationWorkerClient{
		work:   &EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1},
		result: &EvolutionGenerationResult{Candidate: &EvolutionCandidate{ArtifactRef: "candidate:sha256:" + workerHex('a')}},
	}
	worker, err := NewEvolutionGenerationWorker(EvolutionGenerationWorkerConfig{
		Client: fake, Capability: EvolutionCapabilityAgent, Version: "1.0.0", Revision: "revision-a",
		LeaseDuration: time.Minute, RenewInterval: 20 * time.Second, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || fake.heartbeats != 1 || len(fake.completedRefs) != 1 || len(fake.failedCodes) != 0 {
		t.Fatalf("processed=%v err=%v heartbeat=%d complete=%v fail=%v", processed, err, fake.heartbeats, fake.completedRefs, fake.failedCodes)
	}
}

func TestEvolutionGenerationWorkerDefersDependencyWaitWithoutFailure(t *testing.T) {
	fake := &fakeEvolutionGenerationWorkerClient{
		work: &EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1},
		result: &EvolutionGenerationResult{
			Deferred: true, FailureCode: "knowledge_candidate_waiting",
			FailureMessage: "candidate generation is waiting for reverification", RetrySeconds: 300,
		},
	}
	worker, err := NewEvolutionGenerationWorker(EvolutionGenerationWorkerConfig{
		Client: fake, Capability: EvolutionCapabilityKnowledge, Version: "1.0.0",
		LeaseDuration: time.Minute, RenewInterval: 20 * time.Second, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || len(fake.deferredCodes) != 1 || fake.deferredCodes[0] != "knowledge_candidate_waiting" || len(fake.failedCodes) != 0 {
		t.Fatalf("processed=%v err=%v defer=%v fail=%v", processed, err, fake.deferredCodes, fake.failedCodes)
	}
}

func TestEvolutionGenerationWorkerReportsBoundedFailure(t *testing.T) {
	fake := &fakeEvolutionGenerationWorkerClient{
		work:        &EvolutionWork{WorkID: "work-a", Status: EvolutionWorkLeased, Attempt: 1},
		generateErr: errors.New("private path /Users/private and token secret"),
	}
	worker, err := NewEvolutionGenerationWorker(EvolutionGenerationWorkerConfig{
		Client: fake, Capability: EvolutionCapabilityKnowledge, Version: "1.0.0",
		LeaseDuration: time.Minute, RenewInterval: 20 * time.Second, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed || len(fake.failedCodes) != 1 || fake.failedCodes[0] != "generation_failed" || len(fake.completedRefs) != 0 {
		t.Fatalf("processed=%v err=%v complete=%v fail=%v", processed, err, fake.completedRefs, fake.failedCodes)
	}
}
