package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const defaultWCPlusAgentBaseURL = "http://127.0.0.1:5001"
const sourceAgentClientResponseMaxBytes int64 = 2 << 20
const invalidSourceAgentCommandResponse = "invalid source agent command response"
const invalidSourceAgentArtifactResponse = "invalid source agent artifact response"

const (
	sourceAgentHeaderCommandID          = "X-Source-Agent-Command-ID"
	sourceAgentHeaderArtifactID         = "X-Source-Agent-Artifact-ID"
	sourceAgentHeaderArtifactVersion    = "X-Source-Agent-Artifact-Version"
	sourceAgentHeaderArtifactWorkerType = "X-Source-Agent-Artifact-Worker-Type"
	sourceAgentHeaderArtifactPlatform   = "X-Source-Agent-Artifact-Platform"
	sourceAgentHeaderArtifactArch       = "X-Source-Agent-Artifact-Architecture"
	sourceAgentHeaderArtifactProtocol   = "X-Source-Agent-Artifact-Protocol-Version"
	sourceAgentHeaderArtifactRevision   = "X-Source-Agent-Artifact-Revision"
	sourceAgentHeaderArtifactChannel    = "X-Source-Agent-Artifact-Channel"
	sourceAgentHeaderArtifactSize       = "X-Source-Agent-Artifact-Size"
	sourceAgentHeaderArtifactSHA256     = "X-Source-Agent-Artifact-SHA256"
)

type SourceAgentConfig struct {
	RemoteURL     string
	AgentToken    string
	AgentID       string
	StateDir      string
	WCPlusBaseURL string
	HTTPClient    *http.Client
}

func (c SourceAgentConfig) Validate() error {
	_, err := c.Normalized()
	return err
}

func (c SourceAgentConfig) Normalized() (SourceAgentConfig, error) {
	c.RemoteURL = strings.TrimRight(strings.TrimSpace(c.RemoteURL), "/")
	c.AgentToken = strings.TrimSpace(c.AgentToken)
	c.AgentID = strings.TrimSpace(c.AgentID)
	c.StateDir = strings.TrimSpace(c.StateDir)
	c.WCPlusBaseURL = strings.TrimRight(strings.TrimSpace(c.WCPlusBaseURL), "/")
	if c.RemoteURL == "" {
		return c, fmt.Errorf("KBASE_REMOTE_URL is required")
	}
	if c.AgentToken == "" {
		return c, fmt.Errorf("KBASE_SOURCE_AGENT_TOKEN is required")
	}
	if !isSafeSourceAgentToken(c.AgentToken) {
		return c, fmt.Errorf("KBASE_SOURCE_AGENT_TOKEN must contain printable ASCII characters only")
	}
	if c.AgentID == "" {
		return c, fmt.Errorf("KBASE_SOURCE_AGENT_ID is required")
	}
	normalizedAgentID, err := normalizeSourceAgentName("KBASE_SOURCE_AGENT_ID", c.AgentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return c, err
	}
	c.AgentID = normalizedAgentID
	if c.StateDir == "" {
		return c, fmt.Errorf("SOURCE_AGENT_STATE_DIR is required")
	}
	remoteCandidate, parseErr := url.Parse(c.RemoteURL)
	if parseErr == nil && remoteCandidate.User != nil {
		return c, fmt.Errorf("KBASE_REMOTE_URL must not contain credentials")
	}
	remote, err := parseSourceAgentBaseURL(c.RemoteURL)
	if err != nil {
		return c, fmt.Errorf("KBASE_REMOTE_URL must be an absolute HTTP(S) URL")
	}
	if remote.Scheme != "https" && !isLoopbackSourceAgentHost(remote.Hostname()) {
		return c, fmt.Errorf("KBASE_REMOTE_URL must use HTTPS unless it targets loopback")
	}
	if c.WCPlusBaseURL == "" {
		c.WCPlusBaseURL = defaultWCPlusAgentBaseURL
	}
	wcplusURL, err := parseSourceAgentBaseURL(c.WCPlusBaseURL)
	if err != nil {
		return c, fmt.Errorf("WCPLUS_BASE_URL must be an absolute HTTP(S) URL")
	}
	if !isExactLoopbackSourceAgentHost(wcplusURL.Hostname()) {
		return c, fmt.Errorf("WCPLUS_BASE_URL must target loopback")
	}
	return c, nil
}

func parseSourceAgentBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid base URL")
	}
	return parsed, nil
}

func isSafeSourceAgentToken(token string) bool {
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		if token[index] < 0x21 || token[index] > 0x7e {
			return false
		}
	}
	return true
}

func isLoopbackSourceAgentHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isExactLoopbackSourceAgentHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

type SourceAgentHTTPError struct {
	Method     string
	Path       string
	StatusCode int
}

type SourceAgentTransportError struct {
	Method string
	Path   string
}

func (e *SourceAgentTransportError) Error() string {
	return fmt.Sprintf("source agent request %s %s failed", e.Method, e.Path)
}

func (e *SourceAgentTransportError) Retryable() bool { return e != nil }

func (e *SourceAgentHTTPError) Error() string {
	return fmt.Sprintf("source agent request %s %s failed with HTTP %d", e.Method, e.Path, e.StatusCode)
}

func (e *SourceAgentHTTPError) Retryable() bool {
	return e != nil && (e.StatusCode >= 500 || e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusTooManyRequests)
}

type SourceAgentClient struct {
	baseURL        *url.URL
	token          string
	agentID        string
	client         *http.Client
	artifactClient *http.Client
}

func NewSourceAgentClient(config SourceAgentConfig) (*SourceAgentClient, error) {
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(normalized.RemoteURL)
	if err != nil {
		return nil, err
	}
	client := normalized.HTTPClient
	artifactClient := client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
		artifactClient = &http.Client{Timeout: 5 * time.Minute, Transport: client.Transport}
	}
	return &SourceAgentClient{
		baseURL:        baseURL,
		token:          normalized.AgentToken,
		agentID:        normalized.AgentID,
		client:         client,
		artifactClient: artifactClient,
	}, nil
}

func (c *SourceAgentClient) Heartbeat(ctx context.Context, heartbeat SourceAgentHeartbeat) (SourceAgent, error) {
	heartbeat.AgentID = c.agentID
	heartbeat.Capabilities = normalizeSourceCapabilities(heartbeat.Capabilities)
	var response struct {
		Agent SourceAgent `json:"agent"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/source-agent/heartbeat", heartbeat, &response); err != nil {
		return SourceAgent{}, err
	}
	return response.Agent, nil
}

func (c *SourceAgentClient) Lease(ctx context.Context, capabilities []string, duration time.Duration) (*SourceSyncRun, error) {
	capabilities = normalizeSourceCapabilities(capabilities)
	leaseSeconds := int(duration / time.Second)
	if leaseSeconds < 0 {
		leaseSeconds = 0
	}
	payload := struct {
		AgentID      string   `json:"agent_id"`
		Capabilities []string `json:"capabilities"`
		LeaseSeconds int      `json:"lease_seconds"`
	}{AgentID: c.agentID, Capabilities: capabilities, LeaseSeconds: leaseSeconds}
	var response struct {
		Run *SourceSyncRun `json:"run"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/source-agent/lease", payload, &response); err != nil {
		return nil, err
	}
	return response.Run, nil
}

func (c *SourceAgentClient) CheckAuth(ctx context.Context) error {
	_, err := c.Lease(ctx, []string{}, 0)
	return err
}

func (c *SourceAgentClient) ClaimCommand(ctx context.Context) (*SourceAgentCommand, error) {
	payload := struct {
		AgentID string `json:"agent_id"`
	}{AgentID: c.agentID}
	var response struct {
		Command json.RawMessage `json:"command"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/source-agent/commands/claim", payload, &response); err != nil {
		return nil, err
	}
	command, err := decodeSourceAgentCommandResponse(response.Command, true)
	if err != nil {
		return nil, err
	}
	if command == nil {
		return nil, nil
	}
	if !validSourceAgentCommandResponseDomain(*command, c.agentID) || command.State != SourceAgentCommandClaimed {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", command.ID, true)
	if err != nil || commandID != command.ID {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return command, nil
}

func (c *SourceAgentClient) ResumeUpgradeCommand(ctx context.Context, commandID string) (*SourceAgentCommand, error) {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", commandID, true)
	if err != nil {
		return nil, err
	}
	command, err := c.recoverUpgradeCommand(ctx, commandID)
	if err != nil {
		return nil, err
	}
	if command == nil || command.ID != commandID || !validSourceAgentUpgradeRecoveryResponse(*command, c.agentID, true) {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return command, nil
}

func (c *SourceAgentClient) RecoverOwnedUpgrade(ctx context.Context) (*SourceAgentCommand, error) {
	command, err := c.recoverUpgradeCommand(ctx, "")
	if err != nil || command == nil {
		return command, err
	}
	if !validSourceAgentUpgradeRecoveryResponse(*command, c.agentID, false) {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return command, nil
}

func (c *SourceAgentClient) recoverUpgradeCommand(ctx context.Context, commandID string) (*SourceAgentCommand, error) {
	payload := struct {
		AgentID   string `json:"agent_id"`
		CommandID string `json:"command_id,omitempty"`
	}{AgentID: c.agentID, CommandID: commandID}
	var responsePayload json.RawMessage
	if err := c.doJSON(ctx, http.MethodPost, "/api/source-agent/commands/recover", payload, &responsePayload); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	var response struct {
		Command json.RawMessage `json:"command"`
	}
	if decodeStrictSourceAgentUpdateJSON(responsePayload, &response) != nil {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	trimmed := bytes.TrimSpace(response.Command)
	if bytes.Equal(trimmed, []byte("null")) {
		if commandID == "" {
			return nil, nil
		}
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	var command SourceAgentCommand
	if len(trimmed) == 0 || decodeStrictSourceAgentUpdateJSON(trimmed, &command) != nil {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return &command, nil
}

func validSourceAgentUpgradeRecoveryResponse(command SourceAgentCommand, agentID string, allowTerminal bool) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", command.ID, true)
	owner, ownerErr := normalizeSourceAgentCommandIdentifier("claim_owner", command.ClaimOwner, true)
	if err != nil || commandID != command.ID || ownerErr != nil || owner != command.ClaimOwner || owner != agentID ||
		!validSourceAgentCommandResponseDomain(command, agentID) || command.Type != SourceAgentCommandUpgrade ||
		!validSourceAgentUpgradeHandoff(command) {
		return false
	}
	idempotency, err := normalizeSourceAgentCommandIdentifier("idempotency_key", command.IdempotencyKey, true)
	if err != nil || idempotency != command.IdempotencyKey {
		return false
	}
	for _, value := range []string{command.CreatedAt, command.UpdatedAt, command.ClaimedAt, command.ExpiresAt} {
		parsed, err := time.Parse(sourceAgentCommandTimestampLayout, value)
		if err != nil || formatSourceAgentCommandTime(parsed) != value {
			return false
		}
	}
	if command.CompletedAt != "" {
		parsed, err := time.Parse(sourceAgentCommandTimestampLayout, command.CompletedAt)
		if err != nil || formatSourceAgentCommandTime(parsed) != command.CompletedAt {
			return false
		}
	}
	message, err := normalizeSourceAgentCommandMessage(command.Message)
	if err != nil || message != command.Message {
		return false
	}
	if command.ActualVersion != "" {
		actualVersion, err := normalizeSourceAgentVersion("actual_version", command.ActualVersion)
		if err != nil || actualVersion != command.ActualVersion {
			return false
		}
	}
	switch command.State {
	case SourceAgentCommandClaimed, SourceAgentCommandDownloading, SourceAgentCommandVerified,
		SourceAgentCommandInstalling, SourceAgentCommandRestarting, SourceAgentCommandVerifying,
		SourceAgentCommandRollback:
		return command.ResultCode == "" && command.ActualVersion == "" && command.CompletedAt == ""
	case SourceAgentCommandSucceeded, SourceAgentCommandFailed, SourceAgentCommandCanceled,
		SourceAgentCommandExpired, SourceAgentCommandRolledBack:
		if !allowTerminal || command.CompletedAt == "" {
			return false
		}
		if command.State == SourceAgentCommandCanceled {
			return command.ResultCode == SourceAgentCommandCodeCanceled && command.ActualVersion == ""
		}
		if command.State == SourceAgentCommandExpired {
			return command.ResultCode == SourceAgentCommandCodeExpired && command.ActualVersion == ""
		}
		return validateSourceAgentCommandTerminalResult(command.Type, SourceAgentCommandTransition{
			State: command.State, ResultCode: command.ResultCode, Message: command.Message,
			ActualVersion: command.ActualVersion,
		}) == nil
	default:
		return false
	}
}

func (c *SourceAgentClient) ReportCommand(
	ctx context.Context,
	commandID, state, code, message, actualVersion string,
) (SourceAgentCommand, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return SourceAgentCommand{}, fmt.Errorf("command_id is required")
	}
	if commandID == "." || commandID == ".." {
		return SourceAgentCommand{}, fmt.Errorf("command_id contains an invalid path segment")
	}
	state = strings.ToLower(strings.TrimSpace(state))
	action, ok := sourceAgentCommandWorkerReportAction(state)
	if !ok {
		return SourceAgentCommand{}, fmt.Errorf("unsupported worker command state")
	}
	payload := struct {
		AgentID       string `json:"agent_id"`
		State         string `json:"state"`
		Code          string `json:"code,omitempty"`
		Message       string `json:"message,omitempty"`
		ActualVersion string `json:"actual_version,omitempty"`
	}{
		AgentID:       c.agentID,
		State:         state,
		Code:          code,
		Message:       message,
		ActualVersion: actualVersion,
	}
	var response struct {
		Command json.RawMessage `json:"command"`
	}
	requestPath := "/api/source-agent/commands/" + url.PathEscape(commandID) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, requestPath, payload, &response); err != nil {
		return SourceAgentCommand{}, err
	}
	command, err := decodeSourceAgentCommandResponse(response.Command, false)
	if err != nil {
		return SourceAgentCommand{}, err
	}
	if !validSourceAgentCommandResponseDomain(*command, c.agentID) || command.ID != commandID || command.State != state {
		return SourceAgentCommand{}, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return *command, nil
}

func (c *SourceAgentClient) DownloadArtifact(
	ctx context.Context,
	command SourceAgentCommand,
	target SourceAgentArtifactTarget,
	protocolVersion string,
) (SourceAgentArtifactPublic, io.ReadCloser, error) {
	if command.Type != SourceAgentCommandUpgrade || command.UpgradeSpec == nil || command.TargetAgentID != c.agentID ||
		(command.State != SourceAgentCommandClaimed && command.State != SourceAgentCommandDownloading) ||
		!validSourceAgentUpgradeHandoff(command) {
		return SourceAgentArtifactPublic{}, nil, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", command.ID, true)
	if err != nil || commandID != command.ID {
		return SourceAgentArtifactPublic{}, nil, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	artifactID, err := normalizeSourceAgentCommandIdentifier("artifact_id", command.UpgradeSpec.ArtifactID, true)
	if err != nil || artifactID != command.UpgradeSpec.ArtifactID ||
		target.CurrentVersion != command.UpgradeSpec.ExpectedCurrentVersion ||
		target.CurrentVersion != command.ExpectedCurrentVersion ||
		!isExactSourceAgentArtifactName("worker_type", target.WorkerType) ||
		!isExactSourceAgentArtifactName("platform", target.Platform) ||
		!isExactSourceAgentArtifactName("architecture", target.Architecture) ||
		!isSourceAgentArtifactVersion(target.CurrentVersion) ||
		!isExactSourceAgentProtocolVersion(protocolVersion) {
		return SourceAgentArtifactPublic{}, nil, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}

	requestPath := "/api/source-agent/artifacts/" + url.PathEscape(artifactID) + "/download"
	endpoint, err := c.endpointForRequestPath(requestPath)
	if err != nil {
		return SourceAgentArtifactPublic{}, nil, err
	}
	query := endpoint.Query()
	query.Set("agent_id", c.agentID)
	query.Set("command_id", commandID)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return SourceAgentArtifactPublic{}, nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Authorization", "Bearer "+c.token)
	requestClient := *c.artifactClient
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := requestClient.Do(req)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return SourceAgentArtifactPublic{}, nil, contextErr
		}
		return SourceAgentArtifactPublic{}, nil, &SourceAgentTransportError{Method: http.MethodGet, Path: requestPath}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return SourceAgentArtifactPublic{}, nil, &SourceAgentHTTPError{Method: http.MethodGet, Path: requestPath, StatusCode: resp.StatusCode}
	}
	metadata, err := parseSourceAgentArtifactResponse(resp, commandID, artifactID, target, protocolVersion)
	if err != nil {
		_ = resp.Body.Close()
		return SourceAgentArtifactPublic{}, nil, err
	}
	return metadata, &sourceAgentBoundedArtifactBody{
		Reader: io.LimitReader(resp.Body, metadata.Size+1),
		Closer: resp.Body,
	}, nil
}

type sourceAgentBoundedArtifactBody struct {
	io.Reader
	io.Closer
}

func parseSourceAgentArtifactResponse(
	response *http.Response,
	commandID, artifactID string,
	target SourceAgentArtifactTarget,
	protocolVersion string,
) (SourceAgentArtifactPublic, error) {
	if response == nil || response.Body == nil || response.Uncompressed || response.ContentLength <= 0 ||
		response.Header.Get("Content-Encoding") != "" {
		return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	contentType, ok := exactSourceAgentResponseHeader(response.Header, "Content-Type")
	if !ok || contentType != "application/octet-stream" {
		return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	for _, forbidden := range []string{
		"X-Source-Agent-Artifact-URL", "X-Source-Agent-Path", "X-Source-Agent-Updater-Path",
		"X-Source-Agent-Executable-Path", "X-Source-Agent-State-Path", "X-Source-Agent-Command-Line",
		"X-Source-Agent-Shell", "X-Source-Agent-Script", "X-Source-Agent-Environment",
		"X-Source-Agent-LaunchAgent-Label", "X-Source-Agent-Token",
	} {
		if len(response.Header.Values(forbidden)) != 0 {
			return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
		}
	}
	values := make(map[string]string, 11)
	for _, name := range []string{
		sourceAgentHeaderCommandID, sourceAgentHeaderArtifactID, sourceAgentHeaderArtifactVersion,
		sourceAgentHeaderArtifactWorkerType, sourceAgentHeaderArtifactPlatform, sourceAgentHeaderArtifactArch,
		sourceAgentHeaderArtifactProtocol, sourceAgentHeaderArtifactRevision, sourceAgentHeaderArtifactChannel,
		sourceAgentHeaderArtifactSize, sourceAgentHeaderArtifactSHA256,
	} {
		value, exact := exactSourceAgentResponseHeader(response.Header, name)
		if !exact {
			return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
		}
		values[name] = value
	}
	size, err := strconv.ParseInt(values[sourceAgentHeaderArtifactSize], 10, 64)
	if err != nil || strconv.FormatInt(size, 10) != values[sourceAgentHeaderArtifactSize] ||
		size <= 0 || size > sourceAgentArtifactMaxBytes || response.ContentLength != size {
		return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	metadata := SourceAgentArtifactPublic{
		ID: values[sourceAgentHeaderArtifactID], WorkerType: values[sourceAgentHeaderArtifactWorkerType],
		Platform: values[sourceAgentHeaderArtifactPlatform], Architecture: values[sourceAgentHeaderArtifactArch],
		Revision: values[sourceAgentHeaderArtifactRevision], Version: values[sourceAgentHeaderArtifactVersion],
		ProtocolVersion: values[sourceAgentHeaderArtifactProtocol], Channel: values[sourceAgentHeaderArtifactChannel],
		Size: size, SHA256: values[sourceAgentHeaderArtifactSHA256],
	}
	if values[sourceAgentHeaderCommandID] != commandID || metadata.ID != artifactID ||
		!isExactSourceAgentArtifactName("artifact_id", metadata.ID) || metadata.ID == "." || metadata.ID == ".." ||
		metadata.WorkerType != target.WorkerType || metadata.Platform != target.Platform || metadata.Architecture != target.Architecture ||
		metadata.ProtocolVersion != protocolVersion || !isSourceAgentArtifactVersion(metadata.Version) ||
		compareSourceAgentArtifactVersions(target.CurrentVersion, metadata.Version) >= 0 ||
		(!isExactLowerHex(metadata.Revision, 40) && !isExactLowerHex(metadata.Revision, 64)) ||
		(metadata.Channel != "staging" && metadata.Channel != "production") ||
		!isExactLowerHex(metadata.SHA256, sha256.Size*2) {
		return SourceAgentArtifactPublic{}, fmt.Errorf(invalidSourceAgentArtifactResponse)
	}
	return metadata, nil
}

func exactSourceAgentResponseHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) != 1 || values[0] == "" || values[0] != strings.TrimSpace(values[0]) {
		return "", false
	}
	return values[0], true
}

func (c *SourceAgentClient) Check(ctx context.Context, check SourceAgentUpdateGuardCheck) error {
	if !validSourceAgentUpdateGuardCheck(check) {
		return fmt.Errorf("invalid source agent update guard request")
	}
	payload := struct {
		AgentID         string `json:"agent_id"`
		ArtifactID      string `json:"artifact_id"`
		CurrentVersion  string `json:"current_version"`
		TargetVersion   string `json:"target_version"`
		Revision        string `json:"revision"`
		Channel         string `json:"channel"`
		Size            int64  `json:"size"`
		SHA256          string `json:"sha256"`
		WorkerType      string `json:"worker_type"`
		Platform        string `json:"platform"`
		Architecture    string `json:"architecture"`
		ProtocolVersion string `json:"protocol_version"`
	}{
		AgentID: c.agentID, ArtifactID: check.ArtifactID,
		CurrentVersion: check.CurrentVersion, TargetVersion: check.Version,
		Revision: check.Revision, Channel: check.Channel, Size: check.Size, SHA256: check.SHA256,
		WorkerType: check.WorkerType, Platform: check.Platform, Architecture: check.Architecture,
		ProtocolVersion: check.ProtocolVersion,
	}
	requestPath := "/api/source-agent/commands/" + url.PathEscape(check.CommandID) + "/guard"
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint, err := c.endpointForRequestPath(requestPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	requestClient := *c.client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := requestClient.Do(req)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return &SourceAgentTransportError{Method: http.MethodPost, Path: requestPath}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return &SourceAgentHTTPError{Method: http.MethodPost, Path: requestPath, StatusCode: resp.StatusCode}
	}
	if resp.ContentLength != 0 || len(resp.TransferEncoding) != 0 || resp.Uncompressed ||
		len(resp.Header.Values("Transfer-Encoding")) != 0 || len(resp.Header.Values("Content-Encoding")) != 0 {
		return fmt.Errorf("invalid source agent update guard response")
	}
	var probe [1]byte
	read, readErr := io.ReadFull(resp.Body, probe[:])
	if read != 0 || readErr != io.EOF {
		return fmt.Errorf("invalid source agent update guard response")
	}
	return nil
}

func validSourceAgentUpdateGuardCheck(check SourceAgentUpdateGuardCheck) bool {
	commandID, err := normalizeSourceAgentCommandIdentifier("command_id", check.CommandID, true)
	if err != nil || commandID != check.CommandID {
		return false
	}
	artifactID, err := normalizeSourceAgentCommandIdentifier("artifact_id", check.ArtifactID, true)
	return err == nil && artifactID == check.ArtifactID &&
		isAllowedSourceAgentUpdateWorkerType(check.WorkerType) &&
		isSourceAgentArtifactVersion(check.CurrentVersion) && isSourceAgentArtifactVersion(check.Version) &&
		compareSourceAgentArtifactVersions(check.CurrentVersion, check.Version) < 0 &&
		(isExactLowerHex(check.Revision, 40) || isExactLowerHex(check.Revision, 64)) &&
		(check.Channel == "staging" || check.Channel == "production") &&
		check.Size > 0 && check.Size <= sourceAgentArtifactMaxBytes &&
		isExactLowerHex(check.SHA256, sha256.Size*2) &&
		isExactSourceAgentArtifactName("platform", check.Platform) &&
		isExactSourceAgentArtifactName("architecture", check.Architecture) &&
		isExactSourceAgentProtocolVersion(check.ProtocolVersion)
}

func decodeSourceAgentCommandResponse(raw json.RawMessage, allowNull bool) (*SourceAgentCommand, error) {
	if raw == nil {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		if allowNull {
			return nil, nil
		}
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	var command SourceAgentCommand
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(invalidSourceAgentCommandResponse)
	}
	return &command, nil
}

func validSourceAgentCommandResponseDomain(command SourceAgentCommand, expectedAgentID string) bool {
	targetAgentID, err := normalizeSourceAgentCommandIdentifier("target_agent_id", command.TargetAgentID, true)
	if err != nil || targetAgentID != command.TargetAgentID || targetAgentID != expectedAgentID {
		return false
	}
	if !isSourceAgentCommandState(command.State) {
		return false
	}
	return command.Type == SourceAgentCommandDiagnose || command.Type == SourceAgentCommandUpgrade || command.Type == SourceAgentCommandRestart
}

func (c *SourceAgentClient) UploadArticle(ctx context.Context, runID string, envelope SourceArticleEnvelope) (SourceIngestReceipt, error) {
	payload := struct {
		AgentID string `json:"agent_id"`
		SourceArticleEnvelope
	}{AgentID: c.agentID, SourceArticleEnvelope: envelope}
	var response struct {
		Receipt SourceIngestReceipt `json:"receipt"`
	}
	requestPath := "/api/source-agent/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/items"
	if err := c.doJSON(ctx, http.MethodPost, requestPath, payload, &response); err != nil {
		return SourceIngestReceipt{}, err
	}
	return response.Receipt, nil
}

func (c *SourceAgentClient) UploadAsset(ctx context.Context, runID string, envelope SourceAssetEnvelope) (SourceAssetReference, error) {
	endpoint := *c.baseURL
	endpoint.Path = path.Join(strings.TrimSuffix(c.baseURL.Path, "/"), "/api/source-agent/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/assets")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(envelope.Data))
	if err != nil {
		return SourceAssetReference{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", envelope.ContentType)
	req.Header.Set("X-Source-Agent-ID", c.agentID)
	req.Header.Set("X-Source-Item-Key", envelope.SourceItemKey)
	req.Header.Set("X-Source-URL", envelope.SourceURL)
	req.Header.Set("X-Content-SHA256", envelope.SHA256)
	resp, err := c.client.Do(req)
	if err != nil {
		return SourceAssetReference{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SourceAssetReference{}, &SourceAgentHTTPError{Method: http.MethodPost, Path: req.URL.Path, StatusCode: resp.StatusCode}
	}
	var payload struct {
		Asset SourceAssetReference `json:"asset"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) != nil {
		return SourceAssetReference{}, fmt.Errorf("decode source asset response failed")
	}
	return payload.Asset, nil
}

func (c *SourceAgentClient) ReportItemFailure(ctx context.Context, runID, sourceItemKey, idempotencyKey, message string) (SourceSyncItem, error) {
	payload := struct {
		AgentID        string `json:"agent_id"`
		SourceItemKey  string `json:"source_item_key"`
		IdempotencyKey string `json:"idempotency_key"`
		Error          string `json:"error"`
	}{
		AgentID:        c.agentID,
		SourceItemKey:  strings.TrimSpace(sourceItemKey),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Error:          strings.TrimSpace(message),
	}
	var response struct {
		Item SourceSyncItem `json:"item"`
	}
	requestPath := "/api/source-agent/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/items"
	if err := c.doJSON(ctx, http.MethodPost, requestPath, payload, &response); err != nil {
		return SourceSyncItem{}, err
	}
	return response.Item, nil
}

func (c *SourceAgentClient) CompleteRun(ctx context.Context, runID string, cursor ...string) (SourceSyncRun, error) {
	cursorValue := ""
	if len(cursor) > 0 {
		cursorValue = strings.TrimSpace(cursor[0])
	}
	return c.finishRun(ctx, runID, "complete", "", cursorValue)
}

func (c *SourceAgentClient) FailRun(ctx context.Context, runID, message string, cursor ...string) (SourceSyncRun, error) {
	cursorValue := ""
	if len(cursor) > 0 {
		cursorValue = strings.TrimSpace(cursor[0])
	}
	return c.finishRun(ctx, runID, "fail", strings.TrimSpace(message), cursorValue)
}

func (c *SourceAgentClient) finishRun(ctx context.Context, runID, action, message, cursor string) (SourceSyncRun, error) {
	payload := struct {
		AgentID string `json:"agent_id"`
		Error   string `json:"error,omitempty"`
		Cursor  string `json:"cursor,omitempty"`
	}{AgentID: c.agentID, Error: message, Cursor: cursor}
	var response struct {
		Run SourceSyncRun `json:"run"`
	}
	requestPath := "/api/source-agent/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, requestPath, payload, &response); err != nil {
		return SourceSyncRun{}, err
	}
	return response.Run, nil
}

func (c *SourceAgentClient) doJSON(ctx context.Context, method, requestPath string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint, err := c.endpointForRequestPath(requestPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SourceAgentHTTPError{Method: method, Path: requestPath, StatusCode: resp.StatusCode}
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, sourceAgentClientResponseMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read source agent response for %s %s failed", method, requestPath)
	}
	if int64(len(responseBody)) > sourceAgentClientResponseMaxBytes {
		return fmt.Errorf(
			"source agent response for %s %s exceeds %d bytes",
			method, requestPath, sourceAgentClientResponseMaxBytes,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode source agent response for %s %s failed", method, requestPath)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("source agent response for %s %s must contain one JSON value", method, requestPath)
	}
	return nil
}

func (c *SourceAgentClient) endpointForRequestPath(requestPath string) (url.URL, error) {
	endpoint := *c.baseURL
	rawPath := path.Join(strings.TrimSuffix(c.baseURL.EscapedPath(), "/"), requestPath)
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return url.URL{}, err
	}
	endpoint.Path = decodedPath
	endpoint.RawPath = rawPath
	return endpoint, nil
}
