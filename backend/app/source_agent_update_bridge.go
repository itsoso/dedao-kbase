package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"path/filepath"
	"sync"
)

const (
	sourceAgentUpdateUpdaterBasename      = "source-agent-updater"
	sourceAgentUpdateStagingDirectoryName = ".source-agent-staging"
	sourceAgentUpdateHandoffDirectoryName = ".source-agent-handoff"
	sourceAgentUpdateStagedBasename       = "worker.staged"
	sourceAgentUpdateHandoffFileName      = "handoff.json"
	sourceAgentUpdateStagedPendingName    = ".worker.staged.pending"
	sourceAgentUpdateHandoffPendingName   = ".handoff.json.pending"
)

var (
	errSourceAgentUpdateBridgeUnavailable = errors.New("source agent update bridge is unavailable")
	errSourceAgentUpdateHandoffConflict   = errors.New("source agent update handoff conflicts")
)

type SourceAgentArtifactDownloader interface {
	DownloadArtifact(
		context.Context,
		SourceAgentCommand,
		SourceAgentArtifactTarget,
		string,
	) (SourceAgentArtifactPublic, io.ReadCloser, error)
}

type SourceAgentUpdateBridgeConfig struct {
	Downloader        SourceAgentArtifactDownloader
	UpdaterExecutable string
	WorkerType        string
	CurrentVersion    string
	Platform          string
	Architecture      string
	ProtocolVersion   string
}

type sourceAgentUpdateBridgeStorage interface {
	Lock() error
	Unlock() error
	LoadHandoff() ([]byte, bool, error)
	Stage(context.Context, io.Reader, int64, string) error
	VerifyStaged(int64, string) error
	PublishHandoff([]byte) error
	StagedPath() string
	Close() error
}

type SourceAgentUpdateBridge struct {
	mu      sync.Mutex
	config  SourceAgentUpdateBridgeConfig
	storage sourceAgentUpdateBridgeStorage
	closed  bool
	faultAt string
}

type sourceAgentUpdateHandoff struct {
	CommandID          string `json:"command_id"`
	ArtifactID         string `json:"artifact_id"`
	WorkerType         string `json:"worker_type"`
	CurrentVersion     string `json:"current_version"`
	TargetVersion      string `json:"target_version"`
	SHA256             string `json:"sha256"`
	Size               int64  `json:"size"`
	StagedBasename     string `json:"staged_basename"`
	Platform           string `json:"platform"`
	Architecture       string `json:"architecture"`
	ProtocolVersion    string `json:"protocol_version"`
	Revision           string `json:"revision"`
	Channel            string `json:"channel"`
	RequestFingerprint string `json:"request_fingerprint"`
}

func NewSourceAgentUpdateBridge(config SourceAgentUpdateBridgeConfig) (*SourceAgentUpdateBridge, error) {
	if config.Downloader == nil || !isCleanAbsoluteSourceAgentUpdatePath(config.UpdaterExecutable) ||
		filepath.Base(config.UpdaterExecutable) != sourceAgentUpdateUpdaterBasename ||
		!isAllowedSourceAgentUpdateWorkerType(config.WorkerType) ||
		!isSourceAgentArtifactVersion(config.CurrentVersion) ||
		!isExactSourceAgentArtifactName("platform", config.Platform) || config.Platform != "darwin" ||
		!isExactSourceAgentArtifactName("architecture", config.Architecture) ||
		!isExactSourceAgentProtocolVersion(config.ProtocolVersion) {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	storage, err := newSourceAgentUpdateBridgeStorage(config.UpdaterExecutable, config.WorkerType)
	if err != nil {
		return nil, errSourceAgentUpdateBridgeUnavailable
	}
	return &SourceAgentUpdateBridge{config: config, storage: storage}, nil
}

func (b *SourceAgentUpdateBridge) Prepare(
	ctx context.Context,
	command SourceAgentCommand,
) (request SourceAgentUpdateRequest, returnErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || ctx.Err() != nil {
		if ctx.Err() != nil {
			return SourceAgentUpdateRequest{}, ctx.Err()
		}
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	if !b.validCommand(command) {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	if err := b.storage.Lock(); err != nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	defer func() {
		if err := b.storage.Unlock(); err != nil {
			request = SourceAgentUpdateRequest{}
			returnErr = errSourceAgentUpdateBridgeUnavailable
		}
	}()
	if payload, found, err := b.storage.LoadHandoff(); err != nil {
		return SourceAgentUpdateRequest{}, err
	} else if found {
		request, err := b.requestFromHandoff(payload)
		if err != nil || !b.requestMatchesCommand(request, command) ||
			b.storage.VerifyStaged(request.ExpectedSize, request.ExpectedSHA256) != nil {
			return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
		}
		return request, nil
	}

	target := SourceAgentArtifactTarget{
		WorkerType: b.config.WorkerType, Platform: b.config.Platform,
		Architecture: b.config.Architecture, CurrentVersion: b.config.CurrentVersion,
	}
	metadata, body, err := b.config.Downloader.DownloadArtifact(ctx, command, target, b.config.ProtocolVersion)
	if err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if body == nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	defer body.Close()
	request, err = b.requestFromMetadata(command, metadata)
	if err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if err := b.storage.Stage(ctx, body, request.ExpectedSize, request.ExpectedSHA256); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if b.faultAt == "after_stage" {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	handoff := sourceAgentUpdateHandoffFromRequest(request)
	payload, err := marshalSourceAgentUpdateJSON(handoff)
	if err != nil {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	if err := b.storage.PublishHandoff(payload); err != nil {
		return SourceAgentUpdateRequest{}, err
	}
	if b.faultAt == "after_handoff" {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateBridgeUnavailable
	}
	return request, nil
}

func (b *SourceAgentUpdateBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return b.storage.Close()
}

func (b *SourceAgentUpdateBridge) validCommand(command SourceAgentCommand) bool {
	return command.State == SourceAgentCommandDownloading && command.Type == SourceAgentCommandUpgrade &&
		validSourceAgentUpgradeHandoff(command) && command.UpgradeSpec.ExpectedCurrentVersion == b.config.CurrentVersion &&
		(command.ExpectedCurrentVersion == "" || command.ExpectedCurrentVersion == b.config.CurrentVersion)
}

func (b *SourceAgentUpdateBridge) requestFromMetadata(
	command SourceAgentCommand,
	metadata SourceAgentArtifactPublic,
) (SourceAgentUpdateRequest, error) {
	check := SourceAgentUpdateGuardCheck{
		CommandID: command.ID, ArtifactID: metadata.ID, WorkerType: metadata.WorkerType,
		CurrentVersion: b.config.CurrentVersion, Version: metadata.Version,
		Revision: metadata.Revision, Channel: metadata.Channel, Size: metadata.Size, SHA256: metadata.SHA256,
		Platform: metadata.Platform, Architecture: metadata.Architecture, ProtocolVersion: metadata.ProtocolVersion,
	}
	if !validSourceAgentUpdateGuardCheck(check) || metadata.ID != command.UpgradeSpec.ArtifactID ||
		metadata.WorkerType != b.config.WorkerType || metadata.Platform != b.config.Platform ||
		metadata.Architecture != b.config.Architecture || metadata.ProtocolVersion != b.config.ProtocolVersion {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	return SourceAgentUpdateRequest{
		CommandID: command.ID, ArtifactID: metadata.ID,
		WorkerType: b.config.WorkerType, CurrentVersion: b.config.CurrentVersion, TargetVersion: metadata.Version,
		ExpectedSHA256: metadata.SHA256, ExpectedSize: metadata.Size, StagedBinary: b.storage.StagedPath(),
		Platform: b.config.Platform, Architecture: b.config.Architecture, ProtocolVersion: b.config.ProtocolVersion,
		Revision: metadata.Revision, Channel: metadata.Channel,
	}, nil
}

func sourceAgentUpdateHandoffFromRequest(request SourceAgentUpdateRequest) sourceAgentUpdateHandoff {
	return sourceAgentUpdateHandoff{
		CommandID: request.CommandID, ArtifactID: request.ArtifactID,
		WorkerType: request.WorkerType, CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		SHA256: request.ExpectedSHA256, Size: request.ExpectedSize, StagedBasename: sourceAgentUpdateStagedBasename,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
		Revision: request.Revision, Channel: request.Channel,
		RequestFingerprint: sourceAgentUpdateRequestFingerprint(request),
	}
}

func (b *SourceAgentUpdateBridge) requestFromHandoff(payload []byte) (SourceAgentUpdateRequest, error) {
	var handoff sourceAgentUpdateHandoff
	if len(payload) == 0 || len(payload) > sourceAgentUpdateReceiptMaxBytes ||
		decodeStrictSourceAgentUpdateJSON(payload, &handoff) != nil || handoff.StagedBasename != sourceAgentUpdateStagedBasename {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	request := SourceAgentUpdateRequest{
		CommandID: handoff.CommandID, ArtifactID: handoff.ArtifactID,
		WorkerType: handoff.WorkerType, CurrentVersion: handoff.CurrentVersion, TargetVersion: handoff.TargetVersion,
		ExpectedSHA256: handoff.SHA256, ExpectedSize: handoff.Size, StagedBinary: b.storage.StagedPath(),
		Platform: handoff.Platform, Architecture: handoff.Architecture, ProtocolVersion: handoff.ProtocolVersion,
		Revision: handoff.Revision, Channel: handoff.Channel,
	}
	check := SourceAgentUpdateGuardCheck{
		CommandID: request.CommandID, ArtifactID: request.ArtifactID, WorkerType: request.WorkerType,
		CurrentVersion: request.CurrentVersion, Version: request.TargetVersion,
		Revision: request.Revision, Channel: request.Channel, Size: request.ExpectedSize, SHA256: request.ExpectedSHA256,
		Platform: request.Platform, Architecture: request.Architecture, ProtocolVersion: request.ProtocolVersion,
	}
	if !validSourceAgentUpdateGuardCheck(check) || !isExactLowerHex(handoff.RequestFingerprint, sha256.Size*2) ||
		handoff.RequestFingerprint != sourceAgentUpdateRequestFingerprint(request) {
		return SourceAgentUpdateRequest{}, errSourceAgentUpdateHandoffConflict
	}
	return request, nil
}

func (b *SourceAgentUpdateBridge) requestMatchesCommand(request SourceAgentUpdateRequest, command SourceAgentCommand) bool {
	return request.CommandID == command.ID && request.ArtifactID == command.UpgradeSpec.ArtifactID &&
		request.WorkerType == b.config.WorkerType && request.CurrentVersion == b.config.CurrentVersion &&
		request.Platform == b.config.Platform && request.Architecture == b.config.Architecture &&
		request.ProtocolVersion == b.config.ProtocolVersion && request.StagedBinary == b.storage.StagedPath()
}

func sourceAgentUpdateWorkerBasename(workerType string) (string, bool) {
	switch workerType {
	case "wechat-worker":
		return "source-agent", true
	case "wcplus-worker":
		return "wcplus-agent", true
	default:
		return "", false
	}
}
