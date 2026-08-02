package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultWCPlusAgentBaseURL = "http://127.0.0.1:5001"
const sourceAgentClientResponseMaxBytes int64 = 2 << 20
const invalidSourceAgentCommandResponse = "invalid source agent command response"

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

func (e *SourceAgentHTTPError) Error() string {
	return fmt.Sprintf("source agent request %s %s failed with HTTP %d", e.Method, e.Path, e.StatusCode)
}

func (e *SourceAgentHTTPError) Retryable() bool {
	return e != nil && (e.StatusCode >= 500 || e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusTooManyRequests)
}

type SourceAgentClient struct {
	baseURL *url.URL
	token   string
	agentID string
	client  *http.Client
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
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &SourceAgentClient{
		baseURL: baseURL,
		token:   normalized.AgentToken,
		agentID: normalized.AgentID,
		client:  client,
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
	if err := json.Unmarshal(trimmed, &command); err != nil {
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
	return command.Type == SourceAgentCommandDiagnose || command.Type == SourceAgentCommandUpgrade
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
