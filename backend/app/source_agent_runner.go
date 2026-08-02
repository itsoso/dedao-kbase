package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

const defaultSourceAgentLeaseDuration = 90 * time.Minute
const defaultSourceAgentProtocolVersion = "2026-08-01"

var errInvalidSourceAgentCapabilityHealth = errors.New("invalid source agent capability health")

type SourceAgentRunnerConfig struct {
	Client          *SourceAgentClient
	CommandClient   SourceAgentCommandClient
	Outbox          *SourceAgentOutbox
	Adapter         SourceAdapter
	Diagnoser       SourceAgentDiagnoser
	Updater         SourceAgentUpdater
	WorkerType      string
	Platform        string
	Architecture    string
	Version         string
	ProtocolVersion string
	LeaseDuration   time.Duration
}

type SourceAgentRunner struct {
	client          *SourceAgentClient
	commandClient   SourceAgentCommandClient
	outbox          *SourceAgentOutbox
	adapter         SourceAdapter
	diagnoser       SourceAgentDiagnoser
	updater         SourceAgentUpdater
	workerType      string
	platform        string
	architecture    string
	version         string
	protocolVersion string
	leaseDuration   time.Duration

	controlGate chan struct{}
	stateMu     sync.Mutex
	state       sourceAgentRunnerState
}

type sourceAgentRunnerState struct {
	sourceRunActive bool
	currentRunID    string
	currentCommand  *SourceAgentCommand
	upgradeActive   bool
	pendingReports  []sourceAgentPendingCommandReport
	lastSuccessAt   string
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
	if config.CommandClient == nil {
		config.CommandClient = config.Client
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = defaultSourceAgentLeaseDuration
	}
	if strings.TrimSpace(config.WorkerType) == "" {
		config.WorkerType = config.Adapter.Name()
	}
	if strings.TrimSpace(config.Platform) == "" {
		config.Platform = runtime.GOOS
	}
	if strings.TrimSpace(config.Architecture) == "" {
		config.Architecture = runtime.GOARCH
	}
	if strings.TrimSpace(config.ProtocolVersion) == "" {
		config.ProtocolVersion = defaultSourceAgentProtocolVersion
	}
	workerType, err := normalizeSourceAgentName("worker_type", config.WorkerType, sourceAgentRuntimeNameMaxRunes, true)
	if err != nil || workerType == "" {
		return nil, fmt.Errorf("invalid source agent worker_type")
	}
	platform, err := normalizeSourceAgentName("platform", config.Platform, sourceAgentRuntimeNameMaxRunes, true)
	if err != nil || platform == "" {
		return nil, fmt.Errorf("invalid source agent platform")
	}
	architecture, err := normalizeSourceAgentName("architecture", config.Architecture, sourceAgentRuntimeNameMaxRunes, true)
	if err != nil || architecture == "" {
		return nil, fmt.Errorf("invalid source agent architecture")
	}
	version, err := normalizeSourceAgentVersion("version", config.Version)
	if err != nil {
		return nil, err
	}
	protocolVersion, err := normalizeSourceAgentVersion("protocol_version", config.ProtocolVersion)
	if err != nil || protocolVersion == "" {
		return nil, fmt.Errorf("invalid source agent protocol_version")
	}
	runner := &SourceAgentRunner{
		client: config.Client, commandClient: config.CommandClient, outbox: config.Outbox,
		adapter: config.Adapter, diagnoser: config.Diagnoser, updater: config.Updater,
		workerType: workerType, platform: platform, architecture: architecture,
		version: version, protocolVersion: protocolVersion, leaseDuration: config.LeaseDuration,
		controlGate: make(chan struct{}, 1),
	}
	runner.controlGate <- struct{}{}
	return runner, nil
}

func (r *SourceAgentRunner) RunOnce(ctx context.Context) (SourceAgentCycleResult, error) {
	result := SourceAgentCycleResult{OK: true}
	if err := r.acquireControl(ctx); err != nil {
		return result, err
	}
	run, err := func() (*SourceSyncRun, error) {
		defer r.releaseControl()
		return r.beginCycle(ctx)
	}()
	if err != nil {
		return result, err
	}
	if run == nil {
		return result, nil
	}
	defer r.finishSourceRun()
	return r.executeLeasedRun(ctx, *run, result)
}

func (r *SourceAgentRunner) acquireControl(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.controlGate:
		return nil
	}
}

func (r *SourceAgentRunner) releaseControl() {
	r.controlGate <- struct{}{}
}

func (r *SourceAgentRunner) beginCycle(ctx context.Context) (*SourceSyncRun, error) {
	health, heartbeat, err := r.collectHeartbeat(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := r.client.Heartbeat(ctx, heartbeat)
	if err != nil {
		return nil, fmt.Errorf("send source-agent heartbeat: %w", err)
	}

	if r.hasCurrentCommand() {
		if r.hasPendingCommandReports() {
			return nil, r.reportPendingCommand(ctx)
		}
		command, sourceRunActive := r.currentCommandSnapshot()
		if command.Type == SourceAgentCommandUpgrade && sourceRunActive {
			return nil, nil
		}
		return nil, r.executeCurrentCommand(ctx, command)
	}

	command, err := r.commandClient.ClaimCommand(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim source-agent command: %w", err)
	}
	if command != nil {
		r.setCurrentCommand(*command)
		if command.Type == SourceAgentCommandUpgrade && r.isSourceRunActive() {
			return nil, nil
		}
		return nil, r.executeCurrentCommand(ctx, *command)
	}

	desiredState := strings.TrimSpace(agent.DesiredState)
	if desiredState == "" {
		desiredState = SourceAgentDesiredActive
	}
	if !health.Healthy || desiredState != SourceAgentDesiredActive || r.isSourceRunActive() || r.isUpgradeActive() {
		return nil, nil
	}
	run, err := r.client.Lease(ctx, r.adapter.Operations(), r.leaseDuration)
	if err != nil {
		return nil, fmt.Errorf("lease source sync run: %w", err)
	}
	if run != nil {
		r.startSourceRun(run.ID)
	}
	return run, nil
}

func (r *SourceAgentRunner) executeLeasedRun(ctx context.Context, run SourceSyncRun, result SourceAgentCycleResult) (SourceAgentCycleResult, error) {
	result.RunID, result.Status = run.ID, run.Status
	if run.Subscription == nil {
		return result, r.failRun(ctx, run.ID, fmt.Errorf("leased run %s is missing its subscription snapshot", run.ID))
	}
	uploaded, err := r.flush(ctx, run.ID)
	result.Uploaded += uploaded
	if err != nil {
		return result, err
	}
	adapterResult, err := r.adapter.Execute(ctx, run, r.outbox)
	if err != nil {
		var executionErr *SourceAdapterExecutionError
		if errors.As(err, &executionErr) && strings.TrimSpace(executionErr.Cursor) != "" {
			uploaded, flushErr := r.flush(ctx, run.ID)
			result.Uploaded += uploaded
			if flushErr != nil {
				return result, flushErr
			}
			pending, countErr := r.outbox.CountPendingForRun(run.ID)
			if countErr != nil {
				return result, countErr
			}
			result.OutboxRemaining = pending
			if pending != 0 {
				return result, fmt.Errorf("%w; run %s still has %d pending outbox items", err, run.ID, pending)
			}
			return result, r.failRun(ctx, run.ID, err, executionErr.Cursor)
		}
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
	for _, failure := range adapterResult.Failures {
		if _, err := r.client.ReportItemFailure(ctx, run.ID, failure.SourceItemKey, failure.IdempotencyKey, failure.Error); err != nil {
			return result, fmt.Errorf("report source item failure: %w", err)
		}
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
	if result.OK {
		r.recordLastSuccess(time.Now().UTC())
	}
	return result, nil
}

func (r *SourceAgentRunner) collectHeartbeat(ctx context.Context) (SourceCapabilityHealth, SourceAgentHeartbeat, error) {
	if err := ctx.Err(); err != nil {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, err
	}
	capabilities := normalizeSourceCapabilities(r.adapter.Operations())
	if len(capabilities) > sourceAgentMaxCapabilities {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, fmt.Errorf("source agent capabilities exceed %d entries", sourceAgentMaxCapabilities)
	}
	capabilityName, err := normalizeSourceAgentName(
		"capability_health key", r.adapter.Name(), sourceAgentRuntimeNameMaxRunes, true,
	)
	if err != nil || capabilityName == "" {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, fmt.Errorf("invalid source agent capability name")
	}
	health := r.adapter.Status(ctx)
	if err := ctx.Err(); err != nil {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, err
	}
	health.Code = strings.ToLower(strings.TrimSpace(health.Code))
	if _, allowed := allowedSourceCapabilityCodes[health.Code]; !allowed || !validSourceAgentCapabilityVersion(health.Version) {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, errInvalidSourceAgentCapabilityHealth
	}
	if strings.TrimSpace(health.LastError) != "" {
		health.LastError = "Capability check failed."
	}
	if strings.TrimSpace(health.RequiresAction) != "" {
		health.RequiresAction = "Operator action is required."
	}
	normalizedHealth, err := normalizeSourceCapabilityHealth(map[string]SourceCapabilityHealth{capabilityName: health})
	if err != nil {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, errInvalidSourceAgentCapabilityHealth
	}
	health = normalizedHealth[capabilityName]
	pending, err := r.outbox.CountPending()
	if err != nil {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, fmt.Errorf("count pending source-agent outbox: %w", err)
	}
	deadLetters, err := r.outbox.CountDeadLetters()
	if err != nil {
		return SourceCapabilityHealth{}, SourceAgentHeartbeat{}, fmt.Errorf("count source-agent dead letters: %w", err)
	}
	currentRunID, currentCommandID, lastSuccessAt := r.heartbeatStateSnapshot()
	return health, SourceAgentHeartbeat{
		WorkerType: r.workerType, Platform: r.platform, Architecture: r.architecture,
		Version: r.version, ProtocolVersion: r.protocolVersion,
		Capabilities: capabilities, CapabilityHealth: normalizedHealth,
		CurrentRunID: currentRunID, CurrentCommandID: currentCommandID,
		OutboxPending: pending, DeadLetterCount: deadLetters, LastSuccessAt: lastSuccessAt,
	}, nil
}

func validSourceAgentCapabilityVersion(value string) bool {
	if value == "" {
		return true
	}
	if strings.TrimSpace(value) != value || len(value) > sourceAgentVersionMaxRunes || value == "." || value == ".." {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' ||
			character == '+' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func (r *SourceAgentRunner) heartbeatStateSnapshot() (string, string, string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	commandID := ""
	if r.state.currentCommand != nil {
		commandID = r.state.currentCommand.ID
	}
	return r.state.currentRunID, commandID, r.state.lastSuccessAt
}

func (r *SourceAgentRunner) hasCurrentCommand() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.state.currentCommand != nil
}

func (r *SourceAgentRunner) hasPendingCommandReports() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return len(r.state.pendingReports) > 0
}

func (r *SourceAgentRunner) currentCommandSnapshot() (SourceAgentCommand, bool) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	command := SourceAgentCommand{}
	if r.state.currentCommand != nil {
		command = *r.state.currentCommand
	}
	return command, r.state.sourceRunActive
}

func (r *SourceAgentRunner) setCurrentCommand(command SourceAgentCommand) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state.currentCommand = &command
	r.state.upgradeActive = command.Type == SourceAgentCommandUpgrade
}

func (r *SourceAgentRunner) setPendingCommandReports(reports []sourceAgentPendingCommandReport) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state.pendingReports = append([]sourceAgentPendingCommandReport(nil), reports...)
}

func (r *SourceAgentRunner) nextPendingCommandReport() (SourceAgentCommand, sourceAgentPendingCommandReport, bool) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.state.currentCommand == nil || len(r.state.pendingReports) == 0 {
		return SourceAgentCommand{}, sourceAgentPendingCommandReport{}, false
	}
	return *r.state.currentCommand, r.state.pendingReports[0], true
}

func (r *SourceAgentRunner) commandReportSucceeded(command SourceAgentCommand) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.state.currentCommand == nil || r.state.currentCommand.ID != command.ID || len(r.state.pendingReports) == 0 {
		return
	}
	r.state.currentCommand = &command
	r.state.pendingReports = r.state.pendingReports[1:]
	if len(r.state.pendingReports) == 0 && isTerminalSourceAgentCommandState(command.State) {
		r.state.currentCommand = nil
		r.state.upgradeActive = false
	}
}

func (r *SourceAgentRunner) isSourceRunActive() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.state.sourceRunActive
}

func (r *SourceAgentRunner) isUpgradeActive() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.state.upgradeActive
}

func (r *SourceAgentRunner) startSourceRun(runID string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state.sourceRunActive = true
	r.state.currentRunID = strings.TrimSpace(runID)
}

func (r *SourceAgentRunner) finishSourceRun() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state.sourceRunActive = false
	r.state.currentRunID = ""
}

func (r *SourceAgentRunner) recordLastSuccess(at time.Time) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state.lastSuccessAt = at.UTC().Format(time.RFC3339Nano)
}

func (r *SourceAgentRunner) flush(ctx context.Context, runID string) (int, error) {
	uploaded := 0
	for {
		items, err := r.outbox.PeekReadyForRun(runID, 100)
		if err != nil {
			return uploaded, err
		}
		if len(items) == 0 {
			return uploaded, nil
		}
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
	}
}

func (r *SourceAgentRunner) failRun(ctx context.Context, runID string, cause error, cursor ...string) error {
	if _, err := r.client.FailRun(ctx, runID, cause.Error(), cursor...); err != nil {
		return fmt.Errorf("%w; report run failure: %v", cause, err)
	}
	return cause
}
