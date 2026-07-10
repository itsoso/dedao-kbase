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
	if c.StateDir == "" {
		return c, fmt.Errorf("WCPLUS_AGENT_STATE_DIR is required")
	}
	remote, err := url.Parse(c.RemoteURL)
	if err != nil || remote.Hostname() == "" || (remote.Scheme != "http" && remote.Scheme != "https") {
		return c, fmt.Errorf("KBASE_REMOTE_URL must be an absolute HTTP(S) URL")
	}
	if remote.User != nil {
		return c, fmt.Errorf("KBASE_REMOTE_URL must not contain credentials")
	}
	if remote.Scheme != "https" && !isLoopbackSourceAgentHost(remote.Hostname()) {
		return c, fmt.Errorf("KBASE_REMOTE_URL must use HTTPS unless it targets loopback")
	}
	if c.WCPlusBaseURL == "" {
		c.WCPlusBaseURL = defaultWCPlusAgentBaseURL
	}
	wcplusURL, err := url.Parse(c.WCPlusBaseURL)
	if err != nil || wcplusURL.Hostname() == "" || (wcplusURL.Scheme != "http" && wcplusURL.Scheme != "https") {
		return c, fmt.Errorf("WCPLUS_BASE_URL must be an absolute HTTP(S) URL")
	}
	if !isLoopbackSourceAgentHost(wcplusURL.Hostname()) {
		return c, fmt.Errorf("WCPLUS_BASE_URL must target loopback")
	}
	return c, nil
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

func (c *SourceAgentClient) FailRun(ctx context.Context, runID, message string) (SourceSyncRun, error) {
	return c.finishRun(ctx, runID, "fail", strings.TrimSpace(message), "")
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
	endpoint := *c.baseURL
	endpoint.Path = path.Join(strings.TrimSuffix(c.baseURL.Path, "/"), requestPath)
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
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return &SourceAgentHTTPError{Method: method, Path: requestPath, StatusCode: resp.StatusCode}
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode source agent response for %s %s: %w", method, requestPath, err)
	}
	return nil
}
