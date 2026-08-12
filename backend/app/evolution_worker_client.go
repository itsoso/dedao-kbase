package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	evolutionWorkerClientResponseMaxBytes int64 = 2 << 20
	evolutionWorkerProtocolVersion              = "evolution-worker.v1"
)

type EvolutionWorkerClientConfig struct {
	RemoteURL  string
	Token      string
	WorkerID   string
	HTTPClient *http.Client
}

type EvolutionWorkerClient struct {
	baseURL  *url.URL
	token    string
	workerID string
	client   *http.Client
}

type EvolutionWorkerHTTPError struct {
	Path       string
	StatusCode int
}

func (failure *EvolutionWorkerHTTPError) Error() string {
	return fmt.Sprintf("evolution worker request %s failed with HTTP %d", failure.Path, failure.StatusCode)
}

func (failure *EvolutionWorkerHTTPError) Retryable() bool {
	return failure != nil && (failure.StatusCode == http.StatusRequestTimeout || failure.StatusCode == http.StatusTooManyRequests || failure.StatusCode >= 500)
}

func NewEvolutionWorkerClient(config EvolutionWorkerClientConfig) (*EvolutionWorkerClient, error) {
	config.RemoteURL = strings.TrimRight(strings.TrimSpace(config.RemoteURL), "/")
	config.Token = strings.TrimSpace(config.Token)
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.RemoteURL == "" {
		return nil, fmt.Errorf("KBASE_REMOTE_URL is required")
	}
	if !isSafeSourceAgentToken(config.Token) {
		return nil, fmt.Errorf("KBASE_SOURCE_AGENT_TOKEN must contain printable ASCII characters only")
	}
	if err := validateEvolutionIdentity("worker_id", config.WorkerID); err != nil || strings.ContainsAny(config.WorkerID, "/\\") {
		return nil, fmt.Errorf("KBASE_EVOLUTION_WORKER_ID is invalid")
	}
	baseURL, err := parseSourceAgentBaseURL(config.RemoteURL)
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
	return &EvolutionWorkerClient{baseURL: baseURL, token: config.Token, workerID: config.WorkerID, client: client}, nil
}

func (client *EvolutionWorkerClient) WorkerID() string { return client.workerID }

func (client *EvolutionWorkerClient) Heartbeat(ctx context.Context, capability EvolutionWorkerCapability, version, revision string) (SourceAgent, error) {
	if !isAllowedEvolutionWorkerCapability(capability) {
		return SourceAgent{}, ErrEvolutionCapabilityInvalid
	}
	workerType := strings.ReplaceAll(string(capability), "_", "-") + "-worker"
	health := SourceCapabilityHealth{Healthy: true, Version: strings.TrimSpace(version)}
	if revision = strings.TrimSpace(revision); revision != "" && revision != "development" {
		health.Code = "revision_" + boundedEvolutionWorkerRevision(revision)
	}
	payload := SourceAgentHeartbeat{
		AgentID: client.workerID, WorkerType: workerType, Version: strings.TrimSpace(version),
		ProtocolVersion: evolutionWorkerProtocolVersion, Capabilities: []string{string(capability)},
		CapabilityHealth: map[string]SourceCapabilityHealth{string(capability): health},
	}
	var response struct {
		Agent SourceAgent `json:"agent"`
	}
	if err := client.doJSON(ctx, "/api/source-agent/heartbeat", payload, &response); err != nil {
		return SourceAgent{}, err
	}
	return response.Agent, nil
}

func (client *EvolutionWorkerClient) Lease(ctx context.Context, capabilities []EvolutionWorkerCapability, duration time.Duration) (*EvolutionWork, error) {
	payload := evolutionWorkerLeaseRequest{WorkerID: client.workerID, Capabilities: capabilities, LeaseSeconds: durationSeconds(duration)}
	var response struct {
		Work *EvolutionWork `json:"work"`
	}
	if err := client.doJSON(ctx, "/api/evolution/workers/lease", payload, &response); err != nil {
		return nil, err
	}
	return response.Work, nil
}

func (client *EvolutionWorkerClient) Renew(ctx context.Context, work EvolutionWork, duration time.Duration) (*EvolutionWork, error) {
	payload := evolutionWorkerRenewRequest{
		WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, LeaseSeconds: durationSeconds(duration),
	}
	var response struct {
		Work *EvolutionWork `json:"work"`
	}
	if err := client.doJSON(ctx, "/api/evolution/workers/renew", payload, &response); err != nil {
		return nil, err
	}
	return response.Work, nil
}

func (client *EvolutionWorkerClient) Generate(ctx context.Context, work EvolutionWork) (*EvolutionGenerationResult, error) {
	payload := evolutionWorkerIdentityRequest{WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, Attempt: work.Attempt}
	var response EvolutionGenerationResult
	if err := client.doJSON(ctx, "/api/evolution/workers/generate", payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *EvolutionWorkerClient) Evaluate(ctx context.Context, work EvolutionWork) (*EvolutionEvaluationResult, error) {
	payload := evolutionWorkerIdentityRequest{WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, Attempt: work.Attempt}
	var response EvolutionEvaluationResult
	if err := client.doJSON(ctx, "/api/evolution/workers/evaluate", payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (client *EvolutionWorkerClient) Complete(ctx context.Context, work EvolutionWork, artifactRef string) (*EvolutionWork, error) {
	payload := EvolutionWorkCompletion{
		WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, Attempt: work.Attempt,
		ResultIdempotencyKey: evolutionWorkResultIdempotencyKey(work, artifactRef),
		ResultArtifactRef:    artifactRef,
	}
	var response struct {
		Work *EvolutionWork `json:"work"`
	}
	if err := client.doJSON(ctx, "/api/evolution/workers/complete", payload, &response); err != nil {
		return nil, err
	}
	return response.Work, nil
}

func evolutionWorkResultIdempotencyKey(work EvolutionWork, artifactRef string) string {
	return "sha256:" + evolutionWorkerPayloadHash(strings.Join([]string{"complete", work.WorkID, fmt.Sprint(work.Attempt), artifactRef}, ":"))
}

func (client *EvolutionWorkerClient) Fail(ctx context.Context, work EvolutionWork, code, message string, retryDelay time.Duration) (*EvolutionWork, error) {
	payload := evolutionWorkerFailRequest{
		WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, Attempt: work.Attempt,
		FailureIdempotencyKey: "sha256:" + evolutionWorkerPayloadHash(strings.Join([]string{"fail", work.WorkID, fmt.Sprint(work.Attempt), code}, ":")),
		FailureCode:           code, FailureMessage: message, RetrySeconds: durationSeconds(retryDelay),
	}
	var response struct {
		Work *EvolutionWork `json:"work"`
	}
	if err := client.doJSON(ctx, "/api/evolution/workers/fail", payload, &response); err != nil {
		return nil, err
	}
	return response.Work, nil
}

func (client *EvolutionWorkerClient) Defer(ctx context.Context, work EvolutionWork, code, message string, retryDelay time.Duration) (*EvolutionWork, error) {
	payload := evolutionWorkerFailRequest{
		WorkID: work.WorkID, WorkerID: client.workerID, LeaseID: work.LeaseID, Attempt: work.Attempt,
		FailureIdempotencyKey: "sha256:" + evolutionWorkerPayloadHash(strings.Join([]string{"defer", work.WorkID, fmt.Sprint(work.Attempt), code}, ":")),
		FailureCode:           code, FailureMessage: message, RetrySeconds: durationSeconds(retryDelay),
	}
	var response struct {
		Work *EvolutionWork `json:"work"`
	}
	if err := client.doJSON(ctx, "/api/evolution/workers/defer", payload, &response); err != nil {
		return nil, err
	}
	return response.Work, nil
}

type evolutionWorkerLeaseRequest struct {
	WorkerID     string                      `json:"worker_id"`
	Capabilities []EvolutionWorkerCapability `json:"capabilities"`
	LeaseSeconds int                         `json:"lease_seconds"`
}

type evolutionWorkerRenewRequest struct {
	WorkID       string `json:"work_id"`
	WorkerID     string `json:"worker_id"`
	LeaseID      string `json:"lease_id"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type evolutionWorkerIdentityRequest struct {
	WorkID   string `json:"work_id"`
	WorkerID string `json:"worker_id"`
	LeaseID  string `json:"lease_id"`
	Attempt  int    `json:"attempt"`
}

type evolutionWorkerFailRequest struct {
	WorkID                string `json:"work_id"`
	WorkerID              string `json:"worker_id"`
	LeaseID               string `json:"lease_id"`
	Attempt               int    `json:"attempt"`
	FailureIdempotencyKey string `json:"failure_idempotency_key"`
	FailureCode           string `json:"failure_code"`
	FailureMessage        string `json:"failure_message"`
	RetrySeconds          int    `json:"retry_seconds"`
}

func (client *EvolutionWorkerClient) doJSON(ctx context.Context, requestPath string, payload, target any) error {
	if ctx == nil {
		return fmt.Errorf("evolution worker request context is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := *client.baseURL
	rawPath := path.Join(strings.TrimSuffix(client.baseURL.EscapedPath(), "/"), requestPath)
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return err
	}
	endpoint.Path, endpoint.RawPath = decodedPath, rawPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("evolution worker request %s failed", requestPath)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return &EvolutionWorkerHTTPError{Path: requestPath, StatusCode: response.StatusCode}
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, evolutionWorkerClientResponseMaxBytes+1))
	if err != nil {
		return fmt.Errorf("read evolution worker response %s failed", requestPath)
	}
	if int64(len(responseBody)) > evolutionWorkerClientResponseMaxBytes {
		return fmt.Errorf("evolution worker response %s exceeds %d bytes", requestPath, evolutionWorkerClientResponseMaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode evolution worker response %s failed", requestPath)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("evolution worker response %s must contain one JSON value", requestPath)
	}
	return nil
}

func durationSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int(duration / time.Second)
}

func boundedEvolutionWorkerRevision(revision string) string {
	revision = strings.ToLower(strings.TrimSpace(revision))
	var result strings.Builder
	for _, char := range revision {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			result.WriteRune(char)
		}
		if result.Len() >= 32 {
			break
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}
