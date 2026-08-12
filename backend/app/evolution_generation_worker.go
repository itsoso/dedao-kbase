package app

import (
	"context"
	"fmt"
	"time"
)

const evolutionGenerationFailureRetryDelay = 5 * time.Second

type EvolutionGenerationWorkerClient interface {
	Heartbeat(context.Context, EvolutionWorkerCapability, string, string) (SourceAgent, error)
	Lease(context.Context, []EvolutionWorkerCapability, time.Duration) (*EvolutionWork, error)
	Renew(context.Context, EvolutionWork, time.Duration) (*EvolutionWork, error)
	Generate(context.Context, EvolutionWork) (*EvolutionGenerationResult, error)
	Complete(context.Context, EvolutionWork, string) (*EvolutionWork, error)
	Fail(context.Context, EvolutionWork, string, string, time.Duration) (*EvolutionWork, error)
	Defer(context.Context, EvolutionWork, string, string, time.Duration) (*EvolutionWork, error)
}

type EvolutionGenerationWorkerConfig struct {
	Client        EvolutionGenerationWorkerClient
	Capability    EvolutionWorkerCapability
	Version       string
	Revision      string
	LeaseDuration time.Duration
	RenewInterval time.Duration
	PollInterval  time.Duration
}

type EvolutionGenerationWorker struct {
	config EvolutionGenerationWorkerConfig
}

func NewEvolutionGenerationWorker(config EvolutionGenerationWorkerConfig) (*EvolutionGenerationWorker, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("evolution worker client is required")
	}
	if config.Capability != EvolutionCapabilityAgent && config.Capability != EvolutionCapabilityKnowledge {
		return nil, fmt.Errorf("generation capability must be agent_evolution or knowledge_evolution")
	}
	if config.LeaseDuration < evolutionMinLeaseDuration || config.LeaseDuration > evolutionMaxLeaseDuration {
		return nil, fmt.Errorf("lease duration must be between %s and %s", evolutionMinLeaseDuration, evolutionMaxLeaseDuration)
	}
	if config.RenewInterval <= 0 || config.RenewInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("renew interval must be shorter than the lease duration")
	}
	if config.PollInterval < time.Second || config.PollInterval > 5*time.Minute {
		return nil, fmt.Errorf("poll interval must be between 1 second and 5 minutes")
	}
	return &EvolutionGenerationWorker{config: config}, nil
}

func (worker *EvolutionGenerationWorker) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("evolution worker context is required")
	}
	for {
		if _, err := worker.RunOnce(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(worker.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (worker *EvolutionGenerationWorker) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("evolution worker context is required")
	}
	if _, err := worker.config.Client.Heartbeat(ctx, worker.config.Capability, worker.config.Version, worker.config.Revision); err != nil {
		return false, fmt.Errorf("evolution worker heartbeat failed")
	}
	work, err := worker.config.Client.Lease(ctx, []EvolutionWorkerCapability{worker.config.Capability}, worker.config.LeaseDuration)
	if err != nil {
		return false, fmt.Errorf("evolution worker lease failed")
	}
	if work == nil {
		return false, nil
	}

	processCtx, cancel := context.WithCancel(ctx)
	renewDone := make(chan error, 1)
	go worker.renewLease(processCtx, *work, renewDone)
	result, generationErr := worker.config.Client.Generate(processCtx, *work)
	cancel()
	renewErr := <-renewDone
	if renewErr != nil {
		return true, renewErr
	}
	if generationErr != nil || result == nil {
		if _, err := worker.config.Client.Fail(ctx, *work, "generation_failed", "candidate generation failed", evolutionGenerationFailureRetryDelay); err != nil {
			return true, fmt.Errorf("evolution worker failure report failed")
		}
		return true, nil
	}
	if result.Deferred {
		if _, err := worker.config.Client.Defer(ctx, *work, result.FailureCode, result.FailureMessage, time.Duration(result.RetrySeconds)*time.Second); err != nil {
			return true, fmt.Errorf("evolution worker deferral failed")
		}
		return true, nil
	}
	if result.Candidate == nil {
		if _, err := worker.config.Client.Fail(ctx, *work, "generation_failed", "candidate generation returned no candidate", evolutionGenerationFailureRetryDelay); err != nil {
			return true, fmt.Errorf("evolution worker failure report failed")
		}
		return true, nil
	}
	if _, err := worker.config.Client.Complete(ctx, *work, result.Candidate.ArtifactRef); err != nil {
		return true, fmt.Errorf("evolution worker completion failed")
	}
	return true, nil
}

func (worker *EvolutionGenerationWorker) renewLease(ctx context.Context, work EvolutionWork, done chan<- error) {
	ticker := time.NewTicker(worker.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			renewed, err := worker.config.Client.Renew(ctx, work, worker.config.LeaseDuration)
			if err != nil || renewed == nil {
				done <- fmt.Errorf("evolution worker lease renewal failed")
				return
			}
			work = *renewed
		}
	}
}
