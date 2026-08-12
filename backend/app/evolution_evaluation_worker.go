package app

import (
	"context"
	"fmt"
	"time"
)

type EvolutionEvaluationWorkerClient interface {
	Heartbeat(context.Context, EvolutionWorkerCapability, string, string) (SourceAgent, error)
	Lease(context.Context, []EvolutionWorkerCapability, time.Duration) (*EvolutionWork, error)
	Renew(context.Context, EvolutionWork, time.Duration) (*EvolutionWork, error)
	Evaluate(context.Context, EvolutionWork) (*EvolutionEvaluationResult, error)
	Fail(context.Context, EvolutionWork, string, string, time.Duration) (*EvolutionWork, error)
}

type EvolutionEvaluationWorkerConfig struct {
	Client        EvolutionEvaluationWorkerClient
	Version       string
	Revision      string
	LeaseDuration time.Duration
	RenewInterval time.Duration
	PollInterval  time.Duration
}

type EvolutionEvaluationWorker struct {
	config EvolutionEvaluationWorkerConfig
}

func NewEvolutionEvaluationWorker(config EvolutionEvaluationWorkerConfig) (*EvolutionEvaluationWorker, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("evolution evaluation client is required")
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
	return &EvolutionEvaluationWorker{config: config}, nil
}

func (worker *EvolutionEvaluationWorker) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("evolution evaluation worker context is required")
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

func (worker *EvolutionEvaluationWorker) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("evolution evaluation worker context is required")
	}
	if _, err := worker.config.Client.Heartbeat(ctx, EvolutionCapabilityEvaluation, worker.config.Version, worker.config.Revision); err != nil {
		return false, fmt.Errorf("evolution evaluation worker heartbeat failed")
	}
	work, err := worker.config.Client.Lease(ctx, []EvolutionWorkerCapability{EvolutionCapabilityEvaluation}, worker.config.LeaseDuration)
	if err != nil {
		return false, fmt.Errorf("evolution evaluation worker lease failed")
	}
	if work == nil {
		return false, nil
	}
	processCtx, cancel := context.WithCancel(ctx)
	renewDone := make(chan error, 1)
	go worker.renewLease(processCtx, *work, renewDone)
	result, evaluationErr := worker.config.Client.Evaluate(processCtx, *work)
	cancel()
	renewErr := <-renewDone
	if renewErr != nil {
		return true, renewErr
	}
	if evaluationErr != nil || result == nil || result.Scorecard == nil {
		if _, err := worker.config.Client.Fail(ctx, *work, "evaluation_failed", "deterministic evaluation failed", evolutionGenerationFailureRetryDelay); err != nil {
			return true, fmt.Errorf("evolution evaluation failure report failed")
		}
		return true, nil
	}
	return true, nil
}

func (worker *EvolutionEvaluationWorker) renewLease(ctx context.Context, work EvolutionWork, done chan<- error) {
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
				done <- fmt.Errorf("evolution evaluation lease renewal failed")
				return
			}
			work = *renewed
		}
	}
}
