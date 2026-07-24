package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	ProofroomEvidenceAuditSchemaVersion = "proofroom-evidence-audit.v1"
	ProofroomDeliveryReceiptVersion     = "proofroom-delivery-receipt.v1"

	ProofroomDeliveryDelivered      = "delivered"
	ProofroomDeliveryRejected       = "rejected"
	ProofroomDeliveryOutcomeUnknown = "outcome_unknown"

	ProofroomCoordinationConfirmedNotDelivered = "confirmed_not_delivered"

	proofroomMaxPayloadBytes      = 256 << 10
	proofroomMaxResponseBytes     = 64 << 10
	proofroomMaxSummaryBytes      = 1024
	proofroomMaxRemoteIDBytes     = 256
	proofroomDeliveryLockWait     = 130 * time.Second
	proofroomDefaultClientTimeout = 20 * time.Second
)

var (
	ErrProofroomAuditNotReady          = errors.New("evidence audit is not ready for Proofroom")
	ErrProofroomAuditInvalid           = errors.New("evidence audit is invalid for Proofroom")
	ErrProofroomDeliveryUnconfigured   = errors.New("Proofroom delivery is not configured")
	ErrProofroomDeliveryConflict       = errors.New("Proofroom delivery idempotency conflict")
	ErrProofroomDeliveryRejected       = errors.New("Proofroom rejected delivery")
	ErrProofroomDeliveryOutcomeUnknown = errors.New("Proofroom delivery outcome is unknown")
)

type ProofroomRemoteError struct {
	StatusCode int
}

func (e *ProofroomRemoteError) Error() string {
	return fmt.Sprintf("Proofroom remote status %d", e.StatusCode)
}

func (e *ProofroomRemoteError) Unwrap() error {
	return ErrProofroomDeliveryRejected
}

type ProofroomEvidenceAuditProjection struct {
	SchemaVersion         string                        `json:"schema_version"`
	Audit                 ProofroomAuditIdentity        `json:"audit"`
	Package               EvidenceAuditPackageRef       `json:"package"`
	TraceID               string                        `json:"trace_id"`
	SubjectIdentity       string                        `json:"subject_identity"`
	ScopeIdentity         string                        `json:"scope_identity"`
	Claims                []ProofroomEvidenceAuditClaim `json:"claims"`
	Summary               ProofroomEvidenceAuditSummary `json:"summary"`
	AdjudicationAuthority string                        `json:"adjudication_authority"`
	KBaseDecisionFinal    bool                          `json:"kbase_decision_final"`
}

type ProofroomAuditIdentity struct {
	AuditID    string `json:"audit_id"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
}

type ProofroomEvidenceAuditClaim struct {
	SourceClaim         string                     `json:"source_claim"`
	NormalizedStatement string                     `json:"normalized_statement"`
	Verdict             string                     `json:"verdict"`
	ComputedConfidence  float64                    `json:"computed_confidence"`
	Evidence            []EvidenceAuditEvidenceRef `json:"evidence"`
	Limitations         []string                   `json:"limitations"`
	KnowledgeGaps       []string                   `json:"knowledge_gaps"`
	ReviewActions       []string                   `json:"review_actions"`
}

type ProofroomEvidenceAuditSummary struct {
	Conclusion    string         `json:"conclusion"`
	VerdictCounts map[string]int `json:"verdict_counts"`
	Limitations   []string       `json:"limitations"`
}

type ProofroomEvidenceAuditPreview struct {
	Payload     ProofroomEvidenceAuditProjection `json:"payload"`
	PayloadHash string                           `json:"payload_hash"`
	Summary     string                           `json:"summary"`
}

type ProofroomDeliveryReceipt struct {
	SchemaVersion   string `json:"schema_version"`
	AuditID         string `json:"audit_id"`
	ProjectionHash  string `json:"projection_hash"`
	RemoteReceiptID string `json:"remote_receipt_id"`
	RemoteStatus    string `json:"remote_status"`
	Status          string `json:"status"`
	RequestedAt     string `json:"requested_at"`
	DeliveredAt     string `json:"delivered_at"`
	ReceiptHash     string `json:"receipt_hash"`
}

type proofroomDeliveryState struct {
	Version             string `json:"version"`
	AuditID             string `json:"audit_id"`
	IdempotencyIdentity string `json:"idempotency_identity"`
	ProjectionHash      string `json:"projection_hash"`
	Status              string `json:"status"`
	RequestedAt         string `json:"requested_at"`
	UpdatedAt           string `json:"updated_at"`
	ReceiptHash         string `json:"receipt_hash,omitempty"`
	FailureCode         string `json:"failure_code,omitempty"`
	RemoteStatusCode    int    `json:"remote_status_code,omitempty"`
	CoordinatedAt       string `json:"coordinated_at,omitempty"`
}

type ProofroomHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ProofroomDNSResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type ProofroomDeliveryConfig struct {
	Endpoint                  string
	Token                     string
	Client                    ProofroomHTTPClient
	Timeout                   time.Duration
	Now                       func() time.Time
	AllowPrivateTestEndpoints bool
	Resolver                  ProofroomDNSResolver
}

type ProofroomDeliveryService struct {
	endpoint                  *url.URL
	token                     string
	client                    ProofroomHTTPClient
	now                       func() time.Time
	allowPrivateTestEndpoints bool
	resolver                  ProofroomDNSResolver
	resolveEndpoint           bool
}

func BuildProofroomEvidenceAuditProjection(audit EvidenceAudit) (ProofroomEvidenceAuditPreview, error) {
	if audit.Status != EvidenceAuditCompleted || strings.TrimSpace(audit.OutputHash) == "" {
		return ProofroomEvidenceAuditPreview{}, ErrProofroomAuditNotReady
	}
	if err := ValidateEvidenceAudit(audit); err != nil {
		return ProofroomEvidenceAuditPreview{}, fmt.Errorf("%w: %v", ErrProofroomAuditInvalid, err)
	}
	calculated, err := EvidenceAuditOutputHash(audit)
	if err != nil || calculated != audit.OutputHash {
		return ProofroomEvidenceAuditPreview{}, fmt.Errorf("%w: output hash mismatch", ErrProofroomAuditInvalid)
	}
	claims := make([]ProofroomEvidenceAuditClaim, 0, len(audit.ClaimAudits))
	for _, claim := range audit.ClaimAudits {
		claims = append(claims, ProofroomEvidenceAuditClaim{
			SourceClaim:         claim.SourceClaim,
			NormalizedStatement: claim.NormalizedStatement,
			Verdict:             claim.Verdict,
			ComputedConfidence:  claim.ComputedConfidence,
			Evidence:            append([]EvidenceAuditEvidenceRef(nil), claim.Evidence...),
			Limitations:         append([]string(nil), claim.Limitations...),
			KnowledgeGaps:       append([]string(nil), claim.KnowledgeGaps...),
			ReviewActions:       append([]string(nil), claim.ReviewActions...),
		})
	}
	projection := ProofroomEvidenceAuditProjection{
		SchemaVersion: ProofroomEvidenceAuditSchemaVersion,
		Audit: ProofroomAuditIdentity{
			AuditID: audit.AuditID, InputHash: audit.InputHash, OutputHash: audit.OutputHash,
		},
		Package:         audit.Package,
		TraceID:         audit.TraceID,
		SubjectIdentity: proofroomPrivateTextIdentity("subject", audit.Subject),
		ScopeIdentity:   proofroomPrivateTextIdentity("scope", audit.Scope),
		Claims:          claims,
		Summary: ProofroomEvidenceAuditSummary{
			Conclusion:    audit.Summary.Conclusion,
			VerdictCounts: cloneProofroomVerdictCounts(audit.Summary.VerdictCounts),
			Limitations:   append([]string(nil), audit.Summary.Limitations...),
		},
		AdjudicationAuthority: "proofroom",
		KBaseDecisionFinal:    false,
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
	}
	if len(payload) > proofroomMaxPayloadBytes {
		return ProofroomEvidenceAuditPreview{}, fmt.Errorf("%w: projection exceeds %d bytes", ErrProofroomAuditInvalid, proofroomMaxPayloadBytes)
	}
	hash := proofroomSHA256(payload)
	summary := fmt.Sprintf(
		"%d claim(s) prepared for Proofroom adjudication; KBase verdicts are evidence-audit findings, not final decisions.",
		len(claims),
	)
	if len(summary) > proofroomMaxSummaryBytes {
		summary = summary[:proofroomMaxSummaryBytes]
	}
	return ProofroomEvidenceAuditPreview{
		Payload: projection, PayloadHash: hash, Summary: summary,
	}, nil
}

func PreviewEvidenceAuditProofroom(store *BookKnowledgeStore, auditID string) (ProofroomEvidenceAuditPreview, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	audit, err := store.LoadEvidenceAudit(auditID)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
	}
	return BuildProofroomEvidenceAuditProjection(*audit)
}

func NewProofroomDeliveryService(config ProofroomDeliveryConfig) (*ProofroomDeliveryService, error) {
	endpoint, err := validateProofroomEndpoint(config.Endpoint, config.AllowPrivateTestEndpoints)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(config.Token)
	if token == "" || len(token) > 8192 || strings.ContainsAny(token, "\r\n\x00") {
		return nil, ErrProofroomDeliveryUnconfigured
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	client := config.Client
	resolveEndpoint := config.Resolver != nil || client == nil
	resolver := config.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = proofroomDefaultClientTimeout
		}
		transport := &http.Transport{
			Proxy:                 nil,
			DialContext:           proofroomSafeDialContext(resolver, config.AllowPrivateTestEndpoints),
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			IdleConnTimeout:       30 * time.Second,
		}
		httpClient := &http.Client{Timeout: timeout, Transport: transport}
		httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many Proofroom redirects")
			}
			redirect, err := validateProofroomEndpoint(
				request.URL.String(), config.AllowPrivateTestEndpoints,
			)
			if err != nil {
				return err
			}
			if !strings.EqualFold(redirect.Hostname(), endpoint.Hostname()) {
				return errors.New("cross-host Proofroom redirect is not allowed")
			}
			return validateProofroomResolvedEndpoint(
				request.Context(), redirect, config.AllowPrivateTestEndpoints, resolver,
			)
		}
		client = httpClient
	}
	return &ProofroomDeliveryService{
		endpoint: endpoint, token: token, client: client, now: now,
		allowPrivateTestEndpoints: config.AllowPrivateTestEndpoints,
		resolver:                  resolver, resolveEndpoint: resolveEndpoint,
	}, nil
}

func (s *ProofroomDeliveryService) Deliver(
	ctx context.Context,
	store *BookKnowledgeStore,
	auditID, idempotencyKey string,
) (ProofroomDeliveryReceipt, bool, error) {
	if s == nil || s.endpoint == nil || strings.TrimSpace(s.token) == "" {
		return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryUnconfigured
	}
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > evidenceAuditMaxIdentifierBytes {
		return ProofroomDeliveryReceipt{}, false, fmt.Errorf("Idempotency-Key is required and bounded")
	}
	preview, err := PreviewEvidenceAuditProofroom(store, auditID)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, err
	}
	unlock, err := store.acquireProofroomDeliveryLock(ctx, auditID)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, err
	}
	defer unlock()
	identity := evidenceAuditOpaqueIdentity(idempotencyKey)
	statePath := store.EvidenceAuditProofroomStatePath(auditID, identity)
	state, stateErr := loadProofroomDeliveryState(statePath)
	if stateErr == nil {
		if state.ProjectionHash != preview.PayloadHash || state.AuditID != auditID {
			return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryConflict
		}
		switch state.Status {
		case ProofroomDeliveryDelivered:
			receipt, err := store.loadProofroomDeliveryReceipt(state.ReceiptHash)
			return receipt, false, err
		case ProofroomDeliveryOutcomeUnknown, "in_flight":
			return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryOutcomeUnknown
		case ProofroomDeliveryRejected:
			if state.RemoteStatusCode != 0 {
				return ProofroomDeliveryReceipt{}, false, &ProofroomRemoteError{
					StatusCode: state.RemoteStatusCode,
				}
			}
			return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryRejected
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		return ProofroomDeliveryReceipt{}, false, stateErr
	}
	if unknown, err := store.hasUnknownProofroomDelivery(preview.PayloadHash); err != nil {
		return ProofroomDeliveryReceipt{}, false, err
	} else if unknown {
		return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryOutcomeUnknown
	}
	if s.resolveEndpoint {
		if err := validateProofroomResolvedEndpoint(
			ctx, s.endpoint, s.allowPrivateTestEndpoints, s.resolver,
		); err != nil {
			return ProofroomDeliveryReceipt{}, false, err
		}
	}
	now := s.now().UTC()
	state = proofroomDeliveryState{
		Version: "1", AuditID: auditID, IdempotencyIdentity: identity,
		ProjectionHash: preview.PayloadHash, Status: "in_flight",
		RequestedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if err := writeProofroomDeliveryState(statePath, state); err != nil {
		return ProofroomDeliveryReceipt{}, false, err
	}
	payload, err := json.Marshal(preview.Payload)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "payload_encoding_failed", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "request_creation_failed", err)
	}
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", identity)
	request.Header.Set("X-KBase-Projection-Hash", preview.PayloadHash)
	response, err := s.client.Do(request)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "transport_outcome_unknown", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		state.Status = ProofroomDeliveryRejected
		state.FailureCode = "unsafe_redirect"
		state.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
		_ = writeProofroomDeliveryState(statePath, state)
		return ProofroomDeliveryReceipt{}, false, fmt.Errorf("%w: redirect is not accepted", ErrProofroomDeliveryRejected)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, proofroomMaxResponseBytes+1))
	if err != nil || len(body) > proofroomMaxResponseBytes {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "response_outcome_unknown", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		state.Status = ProofroomDeliveryRejected
		state.FailureCode = fmt.Sprintf("remote_http_%d", response.StatusCode)
		state.RemoteStatusCode = response.StatusCode
		state.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
		if writeErr := writeProofroomDeliveryState(statePath, state); writeErr != nil {
			return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "rejection_state_failed", writeErr)
		}
		return ProofroomDeliveryReceipt{}, false, &ProofroomRemoteError{StatusCode: response.StatusCode}
	}
	var remote struct {
		ReceiptID string `json:"receipt_id"`
		Status    string `json:"status"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&remote); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		strings.TrimSpace(remote.ReceiptID) == "" || len(remote.ReceiptID) > proofroomMaxRemoteIDBytes ||
		strings.TrimSpace(remote.Status) == "" || len(remote.Status) > proofroomMaxRemoteIDBytes {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(
			statePath, state, "remote_response_invalid", errors.New("invalid remote response"),
		)
	}
	deliveredAt := s.now().UTC()
	receipt := ProofroomDeliveryReceipt{
		SchemaVersion: ProofroomDeliveryReceiptVersion,
		AuditID:       auditID, ProjectionHash: preview.PayloadHash,
		RemoteReceiptID: strings.TrimSpace(remote.ReceiptID),
		RemoteStatus:    strings.TrimSpace(remote.Status),
		Status:          ProofroomDeliveryDelivered,
		RequestedAt:     state.RequestedAt, DeliveredAt: deliveredAt.Format(time.RFC3339Nano),
	}
	receiptHash, payload, err := proofroomReceiptHash(receipt)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "receipt_encoding_failed", err)
	}
	receipt.ReceiptHash = receiptHash
	payload, err = json.Marshal(receipt)
	if err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "receipt_encoding_failed", err)
	}
	if err := writeEvidenceAuditImmutableFile(store.EvidenceAuditProofroomReceiptPath(receiptHash), payload); err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "receipt_persistence_unknown", err)
	}
	state.Status = ProofroomDeliveryDelivered
	state.ReceiptHash = receiptHash
	state.UpdatedAt = deliveredAt.Format(time.RFC3339Nano)
	if err := writeProofroomDeliveryState(statePath, state); err != nil {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "delivery_state_unknown", err)
	}
	return receipt, true, nil
}

func CoordinateProofroomDelivery(
	store *BookKnowledgeStore,
	auditID, idempotencyKey, resolution string,
	now time.Time,
) error {
	if resolution != ProofroomCoordinationConfirmedNotDelivered {
		return fmt.Errorf("unsupported Proofroom coordination resolution")
	}
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	unlock, err := store.acquireProofroomDeliveryLock(context.Background(), auditID)
	if err != nil {
		return err
	}
	defer unlock()
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > evidenceAuditMaxIdentifierBytes {
		return fmt.Errorf("Idempotency-Key is required and bounded")
	}
	path := store.EvidenceAuditProofroomStatePath(auditID, evidenceAuditOpaqueIdentity(idempotencyKey))
	state, err := loadProofroomDeliveryState(path)
	if err != nil {
		return err
	}
	if state.AuditID != auditID {
		return ErrProofroomDeliveryConflict
	}
	if state.Status != ProofroomDeliveryOutcomeUnknown && state.Status != "in_flight" {
		return fmt.Errorf("Proofroom delivery is not awaiting coordination")
	}
	if now.IsZero() {
		now = time.Now()
	}
	state.Status = "coordinated_not_delivered"
	state.CoordinatedAt = now.UTC().Format(time.RFC3339Nano)
	state.UpdatedAt = state.CoordinatedAt
	return writeProofroomDeliveryState(path, state)
}

func (s *BookKnowledgeStore) EvidenceAuditProofroomDir() string {
	return filepath.Join(s.EvidenceAuditDir(), "proofroom")
}

func (s *BookKnowledgeStore) EvidenceAuditProofroomReceiptPath(receiptHash string) string {
	return filepath.Join(s.EvidenceAuditProofroomDir(), "receipts", evidenceAuditHashName(receiptHash)+".json")
}

func (s *BookKnowledgeStore) EvidenceAuditProofroomStatePath(auditID, identity string) string {
	return filepath.Join(s.EvidenceAuditProofroomDir(), "deliveries", evidenceAuditHashName(identity)+".json")
}

func (s *BookKnowledgeStore) acquireProofroomDeliveryLock(ctx context.Context, auditID string) (func(), error) {
	if err := ensureEvidenceAuditPrivateDir(filepath.Join(s.EvidenceAuditProofroomDir(), "locks")); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(
		s.EvidenceAuditProofroomDir(), "locks", ".delivery.lock",
	))
	if ctx == nil {
		ctx = context.Background()
	}
	lockCtx, cancel := context.WithTimeout(ctx, proofroomDeliveryLockWait)
	locked, err := lock.TryLockContext(lockCtx, 10*time.Millisecond)
	cancel()
	if err != nil || !locked {
		_ = lock.Close()
		if err == nil {
			err = fmt.Errorf("timed out acquiring Proofroom delivery lock")
		}
		return nil, err
	}
	return func() { _ = lock.Close() }, nil
}

func (s *BookKnowledgeStore) hasUnknownProofroomDelivery(projectionHash string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.EvidenceAuditProofroomDir(), "deliveries"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) > evidenceAuditMaxManifestIdempotency {
		return false, fmt.Errorf("Proofroom delivery state capacity exceeded")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		state, err := loadProofroomDeliveryState(filepath.Join(
			s.EvidenceAuditProofroomDir(), "deliveries", entry.Name(),
		))
		if err != nil {
			return false, err
		}
		if state.ProjectionHash == projectionHash &&
			(state.Status == ProofroomDeliveryOutcomeUnknown || state.Status == "in_flight") {
			return true, nil
		}
	}
	return false, nil
}

func (s *BookKnowledgeStore) loadProofroomDeliveryReceipt(hash string) (ProofroomDeliveryReceipt, error) {
	var receipt ProofroomDeliveryReceipt
	if err := readJSONFile(s.EvidenceAuditProofroomReceiptPath(hash), &receipt); err != nil {
		return ProofroomDeliveryReceipt{}, err
	}
	calculated, _, err := proofroomReceiptHash(receipt)
	if err != nil || calculated != receipt.ReceiptHash || calculated != hash {
		return ProofroomDeliveryReceipt{}, ErrEvidenceAuditImmutable
	}
	return receipt, nil
}

func (s *ProofroomDeliveryService) markProofroomOutcomeUnknown(
	path string,
	state proofroomDeliveryState,
	code string,
	cause error,
) error {
	state.Status = ProofroomDeliveryOutcomeUnknown
	state.FailureCode = code
	state.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	if err := writeProofroomDeliveryState(path, state); err != nil {
		return fmt.Errorf("%w: persist unknown outcome: %v", ErrProofroomDeliveryOutcomeUnknown, err)
	}
	if cause == nil {
		return ErrProofroomDeliveryOutcomeUnknown
	}
	return fmt.Errorf("%w: %v", ErrProofroomDeliveryOutcomeUnknown, cause)
}

func validateProofroomEndpoint(raw string, allowPrivate bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Hostname() == "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("invalid Proofroom endpoint")
	}
	if endpoint.User != nil || endpoint.Fragment != "" {
		return nil, fmt.Errorf("Proofroom endpoint cannot contain userinfo or fragment")
	}
	if endpoint.Scheme != "https" && !(allowPrivate && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("Proofroom endpoint must use https")
	}
	if proofroomHostIsPrivate(endpoint.Hostname()) && !allowPrivate {
		return nil, fmt.Errorf("Proofroom endpoint cannot use loopback or private address")
	}
	return endpoint, nil
}

func proofroomHostIsPrivate(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	for _, raw := range []string{"100.64.0.0/10", "198.18.0.0/15"} {
		_, network, _ := net.ParseCIDR(raw)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func validateProofroomResolvedEndpoint(
	ctx context.Context,
	endpoint *url.URL,
	allowPrivate bool,
	resolver ProofroomDNSResolver,
) error {
	if allowPrivate {
		return nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, endpoint.Hostname())
	if err != nil {
		return fmt.Errorf("resolve Proofroom endpoint: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("Proofroom endpoint did not resolve")
	}
	for _, address := range addresses {
		if proofroomHostIsPrivate(address.IP.String()) {
			return fmt.Errorf("Proofroom endpoint resolved to loopback or private address")
		}
	}
	return nil
}

func proofroomSafeDialContext(
	resolver ProofroomDNSResolver,
	allowPrivate bool,
) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if allowPrivate {
			return dialer.DialContext(ctx, network, address)
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, candidate := range addresses {
			if proofroomHostIsPrivate(candidate.IP.String()) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		}
		return nil, fmt.Errorf("Proofroom endpoint has no public address")
	}
}

func loadProofroomDeliveryState(path string) (proofroomDeliveryState, error) {
	var state proofroomDeliveryState
	if err := readJSONFile(path, &state); err != nil {
		return proofroomDeliveryState{}, err
	}
	if state.Version != "1" || state.AuditID == "" || state.IdempotencyIdentity == "" ||
		state.ProjectionHash == "" || state.Status == "" {
		return proofroomDeliveryState{}, fmt.Errorf("invalid Proofroom delivery state")
	}
	return state, nil
}

func writeProofroomDeliveryState(path string, state proofroomDeliveryState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeEvidenceAuditPrivateFile(path, payload)
}

func proofroomReceiptHash(receipt ProofroomDeliveryReceipt) (string, []byte, error) {
	receipt.ReceiptHash = ""
	payload, err := json.Marshal(receipt)
	if err != nil {
		return "", nil, err
	}
	return proofroomSHA256(payload), payload, nil
}

func proofroomPrivateTextIdentity(label, value string) string {
	return proofroomSHA256([]byte(label + "\x00" + value))
}

func proofroomSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneProofroomVerdictCounts(input map[string]int) map[string]int {
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
