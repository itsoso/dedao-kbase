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
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	proofroomMaxSafeTextBytes     = 2048
)

const (
	proofroomStateFaultTempSynced      = "proofroom_state_temp_synced"
	proofroomStateFaultBackupPublished = "proofroom_state_backup_published"
	proofroomStateFaultBeforePublish   = "proofroom_state_before_publish"
)

var proofroomStateStorageFault = func(string, string) error { return nil }

var (
	ErrProofroomAuditNotReady          = errors.New("evidence audit is not ready for Proofroom")
	ErrProofroomAuditInvalid           = errors.New("evidence audit is invalid for Proofroom")
	ErrProofroomPrivacyBlocked         = errors.New("Proofroom projection blocked by privacy policy")
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
	Proofroom             ProofroomReviewContract       `json:"proofroom"`
	AdjudicationAuthority string                        `json:"adjudication_authority"`
	KBaseDecisionFinal    bool                          `json:"kbase_decision_final"`
}

type ProofroomAuditIdentity struct {
	AuditID    string `json:"audit_id"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
}

type ProofroomEvidenceAuditClaim struct {
	SourceClaimIdentity string                 `json:"source_claim_identity"`
	NormalizedStatement ProofroomSafeText      `json:"normalized_statement"`
	Verdict             string                 `json:"verdict"`
	ComputedConfidence  float64                `json:"computed_confidence"`
	Evidence            []ProofroomEvidenceRef `json:"evidence"`
	Limitations         []ProofroomSafeText    `json:"limitations"`
	KnowledgeGaps       []ProofroomSafeText    `json:"knowledge_gaps"`
	ReviewActions       []ProofroomSafeText    `json:"review_actions"`
}

type ProofroomEvidenceRef struct {
	ReleaseID         string `json:"release_id"`
	ContentHash       string `json:"content_hash"`
	Role              string `json:"role"`
	SourceType        string `json:"source_type"`
	ClaimID           string `json:"claim_id"`
	ChunkID           string `json:"chunk_id"`
	CitationID        string `json:"citation_id"`
	PublishedAt       string `json:"published_at"`
	FreshnessDecision string `json:"freshness_decision"`
	Conflict          bool   `json:"conflict,omitempty"`
}

type ProofroomEvidenceAuditSummary struct {
	Conclusion    ProofroomSafeText   `json:"conclusion"`
	VerdictCounts map[string]int      `json:"verdict_counts"`
	Limitations   []ProofroomSafeText `json:"limitations"`
}

type ProofroomSafeText struct {
	Text         string `json:"text"`
	OriginalHash string `json:"original_hash"`
	Redacted     bool   `json:"redacted"`
}

type ProofroomReviewContract struct {
	Title       ProofroomSafeText   `json:"title"`
	ReviewItems []ProofroomSafeText `json:"review_items"`
}

type ProofroomEvidenceAuditPreview struct {
	Payload     ProofroomEvidenceAuditProjection `json:"payload"`
	PayloadHash string                           `json:"payload_hash"`
	Summary     string                           `json:"summary"`
}

type ProofroomDeliveryReceipt struct {
	SchemaVersion    string `json:"schema_version"`
	AuditID          string `json:"audit_id"`
	ProjectionHash   string `json:"projection_hash"`
	RemoteReceiptID  string `json:"remote_receipt_id"`
	RemoteStatus     string `json:"remote_status"`
	Status           string `json:"status"`
	RequestedAt      string `json:"requested_at"`
	DeliveredAt      string `json:"delivered_at"`
	ReceiptHash      string `json:"receipt_hash"`
	EndpointIdentity string `json:"endpoint_identity"`
}

type proofroomDeliveryState struct {
	Version             string `json:"version"`
	AuditID             string `json:"audit_id"`
	IdempotencyIdentity string `json:"idempotency_identity"`
	ProjectionHash      string `json:"projection_hash"`
	EndpointIdentity    string `json:"endpoint_identity"`
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
	endpointIdentity          string
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
		statement, err := proofroomMinimizeText(claim.NormalizedStatement)
		if err != nil {
			return ProofroomEvidenceAuditPreview{}, err
		}
		limitations, err := proofroomMinimizeTexts(claim.Limitations)
		if err != nil {
			return ProofroomEvidenceAuditPreview{}, err
		}
		gaps, err := proofroomMinimizeTexts(claim.KnowledgeGaps)
		if err != nil {
			return ProofroomEvidenceAuditPreview{}, err
		}
		actions, err := proofroomMinimizeTexts(claim.ReviewActions)
		if err != nil {
			return ProofroomEvidenceAuditPreview{}, err
		}
		claims = append(claims, ProofroomEvidenceAuditClaim{
			SourceClaimIdentity: proofroomPrivateTextIdentity("source_claim", claim.SourceClaim),
			NormalizedStatement: statement,
			Verdict:             claim.Verdict,
			ComputedConfidence:  claim.ComputedConfidence,
			Evidence:            proofroomEvidenceRefs(claim.Evidence),
			Limitations:         limitations,
			KnowledgeGaps:       gaps,
			ReviewActions:       actions,
		})
	}
	conclusion, err := proofroomMinimizeText(audit.Summary.Conclusion)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
	}
	summaryLimitations, err := proofroomMinimizeTexts(audit.Summary.Limitations)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
	}
	reviewTitle, err := proofroomMinimizeText(audit.Proofroom.Title)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
	}
	reviewItems, err := proofroomMinimizeTexts(audit.Proofroom.ReviewItems)
	if err != nil {
		return ProofroomEvidenceAuditPreview{}, err
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
			Conclusion:    conclusion,
			VerdictCounts: cloneProofroomVerdictCounts(audit.Summary.VerdictCounts),
			Limitations:   summaryLimitations,
		},
		Proofroom: ProofroomReviewContract{
			Title:       reviewTitle,
			ReviewItems: reviewItems,
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
	audit, err := store.LoadEvidenceAuditSnapshot(auditID)
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
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		client = httpClient
	}
	endpointIdentity := proofroomEndpointIdentity(endpoint)
	return &ProofroomDeliveryService{
		endpoint: endpoint, token: token, client: client, now: now,
		allowPrivateTestEndpoints: config.AllowPrivateTestEndpoints,
		resolver:                  resolver, resolveEndpoint: resolveEndpoint,
		endpointIdentity: endpointIdentity,
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
		if state.ProjectionHash != preview.PayloadHash || state.AuditID != auditID ||
			state.EndpointIdentity != s.endpointIdentity {
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
	if unknown, err := store.hasUnknownProofroomDelivery(preview.PayloadHash, s.endpointIdentity); err != nil {
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
		ProjectionHash: preview.PayloadHash, EndpointIdentity: s.endpointIdentity, Status: "in_flight",
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
	if proofroomRemoteOutcomeUnknown(response.StatusCode) {
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(
			statePath, state, fmt.Sprintf("remote_http_%d_outcome_unknown", response.StatusCode), nil,
		)
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
	remote.Status = strings.ToLower(strings.TrimSpace(remote.Status))
	switch remote.Status {
	case "accepted", "delivered", "succeeded":
	case "rejected", "failed":
		state.Status = ProofroomDeliveryRejected
		state.FailureCode = "remote_business_" + remote.Status
		state.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
		if err := writeProofroomDeliveryState(statePath, state); err != nil {
			return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(statePath, state, "rejection_state_failed", err)
		}
		return ProofroomDeliveryReceipt{}, false, ErrProofroomDeliveryRejected
	default:
		return ProofroomDeliveryReceipt{}, false, s.markProofroomOutcomeUnknown(
			statePath, state, "remote_status_unknown", errors.New("unrecognized remote status"),
		)
	}
	deliveredAt := s.now().UTC()
	receipt := ProofroomDeliveryReceipt{
		SchemaVersion: ProofroomDeliveryReceiptVersion,
		AuditID:       auditID, ProjectionHash: preview.PayloadHash,
		RemoteReceiptID:  strings.TrimSpace(remote.ReceiptID),
		RemoteStatus:     strings.TrimSpace(remote.Status),
		Status:           ProofroomDeliveryDelivered,
		EndpointIdentity: s.endpointIdentity,
		RequestedAt:      state.RequestedAt, DeliveredAt: deliveredAt.Format(time.RFC3339Nano),
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

func CoordinateProofroomDeliveryForEndpoint(
	store *BookKnowledgeStore,
	auditID, idempotencyKey, endpointIdentity, resolution string,
	now time.Time,
) error {
	if resolution != ProofroomCoordinationConfirmedNotDelivered {
		return fmt.Errorf("unsupported Proofroom coordination resolution")
	}
	endpointIdentity = strings.TrimSpace(endpointIdentity)
	if !validProofroomEndpointIdentity(endpointIdentity) {
		return fmt.Errorf("valid Proofroom endpoint identity is required")
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
	if state.EndpointIdentity != endpointIdentity {
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

func (s *BookKnowledgeStore) hasUnknownProofroomDelivery(projectionHash, endpointIdentity string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.EvidenceAuditProofroomDir(), "deliveries"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	statePaths := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".bak") {
			name = strings.TrimSuffix(name, ".bak")
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		statePaths[filepath.Join(s.EvidenceAuditProofroomDir(), "deliveries", name)] = struct{}{}
	}
	if len(statePaths) > evidenceAuditMaxManifestIdempotency {
		return false, fmt.Errorf("Proofroom delivery state capacity exceeded")
	}
	for path := range statePaths {
		state, err := loadProofroomDeliveryState(path)
		if err != nil {
			return false, err
		}
		if state.ProjectionHash == projectionHash && state.EndpointIdentity == endpointIdentity &&
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
	state, primaryErr := loadProofroomDeliveryStateFile(path)
	if primaryErr == nil {
		return state, nil
	}
	state, backupErr := loadProofroomDeliveryStateFile(path + ".bak")
	if backupErr != nil {
		return proofroomDeliveryState{}, primaryErr
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return proofroomDeliveryState{}, err
	}
	if err := restoreProofroomDeliveryState(path, payload); err != nil {
		return proofroomDeliveryState{}, err
	}
	return state, nil
}

func loadProofroomDeliveryStateFile(path string) (proofroomDeliveryState, error) {
	var state proofroomDeliveryState
	if err := readJSONFile(path, &state); err != nil {
		return proofroomDeliveryState{}, err
	}
	if state.Version != "1" || state.AuditID == "" || state.IdempotencyIdentity == "" ||
		state.ProjectionHash == "" || state.EndpointIdentity == "" || state.Status == "" {
		return proofroomDeliveryState{}, fmt.Errorf("invalid Proofroom delivery state")
	}
	return state, nil
}

func writeProofroomDeliveryState(path string, state proofroomDeliveryState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeProofroomDeliveryStateCrashSafe(path, payload)
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

func proofroomRemoteOutcomeUnknown(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func proofroomEndpointIdentity(endpoint *url.URL) string {
	scheme := strings.ToLower(endpoint.Scheme)
	host := strings.ToLower(endpoint.Hostname())
	port := endpoint.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	pathHash := proofroomSHA256([]byte(endpoint.EscapedPath()))
	return scheme + "://" + net.JoinHostPort(host, port) + "/path/" + pathHash
}

var proofroomSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)["']?(?:access[_-]?token|refresh[_-]?token|api[_-]?key|client[_-]?secret|password|session|cookie|csrf|token|secret)["']?\s*(?:=|:)\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^&,\s;}\]]+)`),
	regexp.MustCompile(`\b\d{17}[\dXx]\b`),
	regexp.MustCompile(`\b1[3-9]\d{9}\b`),
	regexp.MustCompile(`(?i)(?:\+?\d[\d .()-]{7,}\d)`),
	regexp.MustCompile(`(?i:\bpatient(?:\s+name\s*[:=]?)?\s+)[A-Z][A-Za-z'’-]*(?:\s+[A-Z][A-Za-z'’-]*){0,5}\b`),
	regexp.MustCompile(`(?:患者|病例|姓名)\s*(?:姓名\s*)?[:：=]?\s*[\p{Han}]{2,6}`),
}

var proofroomResidualSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:access[_-]?token|refresh[_-]?token|api[_-]?key|client[_-]?secret|password|session|cookie|csrf|token|secret)\b`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\b`),
	regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
	regexp.MustCompile(`\b\d{17}[\dXx]\b`),
	regexp.MustCompile(`\b1[3-9]\d{9}\b`),
	regexp.MustCompile(`(?i:\bpatient(?:\s+name\s*[:=]?)?\s+)[A-Z][A-Za-z'’-]*(?:\s+[A-Z][A-Za-z'’-]*){0,5}\b`),
	regexp.MustCompile(`(?:患者|病例|姓名)\s*(?:姓名\s*)?[:：=]?\s*[\p{Han}]{2,6}`),
}

func proofroomMinimizeText(value string) (ProofroomSafeText, error) {
	if !utf8.ValidString(value) {
		return ProofroomSafeText{}, ErrProofroomPrivacyBlocked
	}
	original := strings.TrimSpace(value)
	text := original
	redacted := false
	for _, pattern := range proofroomSensitivePatterns {
		if pattern.MatchString(text) {
			redacted = true
			text = pattern.ReplaceAllString(text, "[REDACTED]")
		}
	}
	text = strings.TrimSpace(text)
	if len(text) > proofroomMaxSafeTextBytes {
		text = text[:proofroomMaxSafeTextBytes]
		redacted = true
	}
	if !utf8.ValidString(text) || proofroomContainsResidualSensitiveText(text) {
		return ProofroomSafeText{}, ErrProofroomPrivacyBlocked
	}
	return ProofroomSafeText{
		Text: text, OriginalHash: proofroomPrivateTextIdentity("proofroom_text", original),
		Redacted: redacted,
	}, nil
}

func proofroomMinimizeTexts(values []string) ([]ProofroomSafeText, error) {
	output := make([]ProofroomSafeText, 0, len(values))
	for _, value := range values {
		safeText, err := proofroomMinimizeText(value)
		if err != nil {
			return nil, err
		}
		output = append(output, safeText)
	}
	return output, nil
}

func proofroomContainsResidualSensitiveText(value string) bool {
	for _, pattern := range proofroomResidualSensitivePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func validProofroomEndpointIdentity(identity string) bool {
	parsed, err := url.Parse(identity)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	pathHash := strings.TrimPrefix(parsed.EscapedPath(), "/path/")
	return pathHash != parsed.EscapedPath() && validEvidenceAuditSHA256(pathHash)
}

func proofroomEvidenceRefs(values []EvidenceAuditEvidenceRef) []ProofroomEvidenceRef {
	output := make([]ProofroomEvidenceRef, 0, len(values))
	for _, value := range values {
		output = append(output, ProofroomEvidenceRef{
			ReleaseID: value.ReleaseID, ContentHash: value.ContentHash,
			Role: value.Role, SourceType: value.SourceType,
			ClaimID: value.ClaimID, ChunkID: value.ChunkID, CitationID: value.CitationID,
			PublishedAt: value.PublishedAt, FreshnessDecision: value.FreshnessDecision,
			Conflict: value.Conflict,
		})
	}
	return output
}

func writeProofroomDeliveryStateCrashSafe(path string, payload []byte) error {
	var next proofroomDeliveryState
	if err := json.Unmarshal(payload, &next); err != nil {
		return err
	}
	if next.Version != "1" || next.AuditID == "" || next.IdempotencyIdentity == "" ||
		next.ProjectionHash == "" || next.EndpointIdentity == "" || next.Status == "" {
		return fmt.Errorf("invalid Proofroom delivery state")
	}
	if err := ensureEvidenceAuditPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	tempPath, err := writeEvidenceAuditSyncedTemp(filepath.Dir(path), ".proofroom-state.next-", payload)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := proofroomStateStorageFault(proofroomStateFaultTempSynced, path); err != nil {
		return err
	}
	backupPath := path + ".bak"
	if _, readErr := loadProofroomDeliveryStateFile(path); readErr == nil {
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(path, backupPath); err != nil {
			return err
		}
		if err := syncEvidenceAuditDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := proofroomStateStorageFault(proofroomStateFaultBackupPublished, path); err != nil {
			return err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := proofroomStateStorageFault(proofroomStateFaultBeforePublish, path); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return syncEvidenceAuditDir(filepath.Dir(path))
}

func restoreProofroomDeliveryState(path string, payload []byte) error {
	tempPath, err := writeEvidenceAuditSyncedTemp(filepath.Dir(path), ".proofroom-state.restore-", payload)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	if err := os.Rename(tempPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		if err := os.Rename(tempPath, path); err != nil {
			return err
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return syncEvidenceAuditDir(filepath.Dir(path))
}
