package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const researchWorkerClientResponseMaxBytes int64 = 1 << 20

type ResearchWorkerClientConfig struct {
	RemoteURL  string
	Token      string
	AgentID    string
	HTTPClient *http.Client
}

type ResearchWorkerClient struct {
	baseURL *url.URL
	token   string
	agentID string
	client  *http.Client
}

func NewResearchWorkerClient(config ResearchWorkerClientConfig) (*ResearchWorkerClient, error) {
	remoteURL := strings.TrimRight(strings.TrimSpace(config.RemoteURL), "/")
	token := strings.TrimSpace(config.Token)
	if remoteURL == "" {
		return nil, fmt.Errorf("KBASE_REMOTE_URL is required")
	}
	if !isSafeSourceAgentToken(token) {
		return nil, fmt.Errorf("KBASE_SOURCE_AGENT_TOKEN is required and must contain printable ASCII characters only")
	}
	agentID, err := normalizeSourceAgentName("KBASE_SOURCE_AGENT_ID", config.AgentID, sourceAgentIDMaxRunes, false)
	if err != nil {
		return nil, err
	}
	baseURL, err := parseSourceAgentBaseURL(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("KBASE_REMOTE_URL must be an absolute HTTP(S) URL")
	}
	if baseURL.Scheme != "https" && !isLoopbackSourceAgentHost(baseURL.Hostname()) {
		return nil, fmt.Errorf("KBASE_REMOTE_URL must use HTTPS unless it targets loopback")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ResearchWorkerClient{baseURL: baseURL, token: token, agentID: agentID, client: client}, nil
}

func (c *ResearchWorkerClient) Claim(ctx context.Context, lease time.Duration) (*ResearchWorkerJob, error) {
	seconds := int(lease / time.Second)
	payload := struct {
		AgentID      string `json:"agent_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}{c.agentID, seconds}
	var response struct {
		Job *ResearchWorkerJob `json:"job"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/research-worker/jobs/claim", "", payload, &response); err != nil {
		return nil, err
	}
	if response.Job == nil {
		return nil, nil
	}
	if err := c.validateJob(*response.Job, "", ResearchWorkerJobLeased); err != nil {
		return nil, err
	}
	return response.Job, nil
}

func (c *ResearchWorkerClient) Renew(ctx context.Context, job ResearchWorkerJob, lease time.Duration) error {
	if err := c.validateJob(job, job.JobID, ResearchWorkerJobLeased); err != nil {
		return err
	}
	payload := struct {
		AgentID      string `json:"agent_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}{c.agentID, int(lease / time.Second)}
	var response struct {
		Job ResearchWorkerJob `json:"job"`
	}
	requestPath := "/api/research-worker/jobs/" + url.PathEscape(job.JobID) + "/renew"
	idempotency := job.JobID + ":" + job.RequestHash + ":renew"
	if err := c.doJSON(ctx, http.MethodPost, requestPath, idempotency, payload, &response); err != nil {
		return err
	}
	return c.validateJob(response.Job, job.JobID, ResearchWorkerJobLeased)
}

func (c *ResearchWorkerClient) Complete(ctx context.Context, job ResearchWorkerJob, result ResearchWorkerResult) (ResearchWorkerJob, error) {
	if err := c.validateJob(job, job.JobID, ResearchWorkerJobLeased); err != nil {
		return ResearchWorkerJob{}, err
	}
	payload := struct {
		AgentID     string               `json:"agent_id"`
		RequestHash string               `json:"request_hash"`
		Result      ResearchWorkerResult `json:"result"`
	}{c.agentID, job.RequestHash, result}
	var response struct {
		Job ResearchWorkerJob `json:"job"`
	}
	requestPath := "/api/research-worker/jobs/" + url.PathEscape(job.JobID) + "/complete"
	idempotency := job.JobID + ":" + job.RequestHash + ":complete"
	if err := c.doJSON(ctx, http.MethodPost, requestPath, idempotency, payload, &response); err != nil {
		return ResearchWorkerJob{}, err
	}
	if err := c.validateJob(response.Job, job.JobID, ResearchWorkerJobCompleted); err != nil {
		return ResearchWorkerJob{}, err
	}
	return response.Job, nil
}

func (c *ResearchWorkerClient) Fail(ctx context.Context, job ResearchWorkerJob, code string, retryable bool) (ResearchWorkerJob, error) {
	if err := c.validateJob(job, job.JobID, ResearchWorkerJobLeased); err != nil {
		return ResearchWorkerJob{}, err
	}
	payload := struct {
		AgentID     string `json:"agent_id"`
		RequestHash string `json:"request_hash"`
		Code        string `json:"code"`
		Retryable   bool   `json:"retryable"`
	}{c.agentID, job.RequestHash, code, retryable}
	var response struct {
		Job ResearchWorkerJob `json:"job"`
	}
	requestPath := "/api/research-worker/jobs/" + url.PathEscape(job.JobID) + "/fail"
	idempotency := job.JobID + ":" + job.RequestHash + ":fail:" + strings.TrimSpace(code)
	if err := c.doJSON(ctx, http.MethodPost, requestPath, idempotency, payload, &response); err != nil {
		return ResearchWorkerJob{}, err
	}
	if response.Job.State != ResearchWorkerJobQueued && response.Job.State != ResearchWorkerJobFailed {
		return ResearchWorkerJob{}, fmt.Errorf("invalid research worker response")
	}
	if err := c.validateJob(response.Job, job.JobID, response.Job.State); err != nil {
		return ResearchWorkerJob{}, err
	}
	return response.Job, nil
}

func (c *ResearchWorkerClient) doJSON(ctx context.Context, method, requestPath, idempotency string, payload, response any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + requestPath
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	httpResponse, err := c.client.Do(request)
	if err != nil {
		return &SourceAgentTransportError{Method: method, Path: requestPath}
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, 4096))
		return &SourceAgentHTTPError{Method: method, Path: requestPath, StatusCode: httpResponse.StatusCode}
	}
	limited := io.LimitReader(httpResponse.Body, researchWorkerClientResponseMaxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("invalid research worker response")
	}
	if int64(len(body)) > researchWorkerClientResponseMaxBytes {
		return fmt.Errorf("invalid research worker response")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("invalid research worker response")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid research worker response")
	}
	return nil
}

func (c *ResearchWorkerClient) validateJob(job ResearchWorkerJob, expectedJobID, expectedState string) error {
	if strings.TrimSpace(job.JobID) == "" || strings.TrimSpace(job.RunID) == "" ||
		job.TargetAgentID != c.agentID || job.LeaseOwner != c.agentID || job.State != expectedState ||
		job.Attempt <= 0 || strings.TrimSpace(job.RequestHash) == "" {
		return fmt.Errorf("invalid research worker response")
	}
	if expectedJobID != "" && job.JobID != expectedJobID {
		return fmt.Errorf("invalid research worker response")
	}
	if _, err := normalizeResearchWorkerArguments(job.Tool, job.Arguments); err != nil {
		return fmt.Errorf("invalid research worker response")
	}
	return nil
}
