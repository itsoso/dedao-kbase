package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	researchPreflightRequestSchemaVersion = "research-preflight-request/v1"
	researchPreflightIDMaxRunes           = 128
	researchPreflightOwnerHashMaxRunes    = 64
	researchPreflightJSONMaxBytes         = 64 << 10
	researchPreflightCleanupMax           = 100
	researchPreflightStoreTTL             = 10 * time.Minute
	researchPreflightTimestampLayout      = "2006-01-02T15:04:05.000000000Z"
)

var (
	ErrResearchPreflightNotFound            = errors.New("research preflight not found")
	ErrResearchPreflightExpired             = errors.New("research preflight expired")
	ErrResearchPreflightOwner               = errors.New("research preflight belongs to another owner")
	ErrResearchPreflightIdempotencyConflict = errors.New("research preflight idempotency conflict")
)

type researchPreflightRecord struct {
	preflightID, ownerHash, requestHash, status string
	candidatesJSON, checksJSON, gapsJSON        string
	parentRunID, createdAt, expiresAt           string
}

func (s *ResearchStore) SaveResearchPreflight(
	ownerHash string,
	request ResearchPreflightRequest,
	result ResearchPreflight,
	ttl time.Duration,
) (*ResearchPreflight, error) {
	if err := validateResearchPreflightOwnerHash(ownerHash); err != nil {
		return nil, err
	}
	if ttl != researchPreflightStoreTTL {
		return nil, fmt.Errorf("research preflight ttl must be %s", researchPreflightStoreTTL)
	}
	normalized, err := NormalizeResearchPreflightRequest(request)
	if err != nil {
		return nil, err
	}
	requestHash, err := hashResearchPreflightRequest(normalized)
	if err != nil {
		return nil, err
	}
	result.PreflightID = strings.TrimSpace(result.PreflightID)
	if result.PreflightID == "" {
		result.PreflightID, err = newResearchPreflightID(s.random)
		if err != nil {
			return nil, err
		}
	}
	if err := validateResearchPreflightID(result.PreflightID); err != nil {
		return nil, err
	}
	result.ParentRunID = normalized.ParentRunID
	result.RequestHash = requestHash
	if err := ValidateResearchPreflight(result); err != nil {
		return nil, err
	}
	incoming, err := encodeResearchPreflightPayload(result)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	result.CreatedAt = formatResearchPreflightTimestamp(now)
	result.ExpiresAt = formatResearchPreflightTimestamp(now.Add(ttl))

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	inserted, err := tx.Exec(`INSERT INTO research_preflights (
		preflight_id, owner_hash, request_hash, status, candidates_json, checks_json, gaps_json,
		parent_run_id, created_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(preflight_id) DO NOTHING`,
		result.PreflightID, ownerHash, result.RequestHash, result.Status,
		incoming.candidatesJSON, incoming.checksJSON, incoming.gapsJSON,
		result.ParentRunID, result.CreatedAt, result.ExpiresAt)
	if err != nil {
		return nil, err
	}
	insertedCount, err := inserted.RowsAffected()
	if err != nil {
		return nil, err
	}
	if insertedCount == 1 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if insertedCount != 0 {
		return nil, fmt.Errorf("research preflight insert returned an invalid row count")
	}

	record, err := queryResearchPreflightRecord(tx, result.PreflightID)
	if err != nil {
		return nil, err
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, fmt.Errorf("persisted research preflight owner is invalid")
	}
	if record.ownerHash != ownerHash {
		return nil, ErrResearchPreflightOwner
	}
	existing, err := decodeResearchPreflightRecord(record)
	if err != nil {
		return nil, err
	}
	if researchPreflightExpired(existing.ExpiresAt, s.now()) {
		return nil, ErrResearchPreflightExpired
	}
	if existing.RequestHash != requestHash {
		return nil, ErrResearchPreflightIdempotencyConflict
	}
	if existing.Status != result.Status || existing.ParentRunID != result.ParentRunID ||
		record.candidatesJSON != incoming.candidatesJSON ||
		record.checksJSON != incoming.checksJSON || record.gapsJSON != incoming.gapsJSON {
		return nil, ErrResearchPreflightIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *ResearchStore) LoadResearchPreflightForOwner(preflightID, ownerHash string) (*ResearchPreflight, error) {
	preflightID = strings.TrimSpace(preflightID)
	if err := validateResearchPreflightID(preflightID); err != nil {
		return nil, err
	}
	if err := validateResearchPreflightOwnerHash(ownerHash); err != nil {
		return nil, err
	}
	record, err := queryResearchPreflightRecord(s.db, preflightID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResearchPreflightNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, fmt.Errorf("persisted research preflight owner is invalid")
	}
	if record.ownerHash != ownerHash {
		return nil, ErrResearchPreflightOwner
	}
	preflight, err := decodeResearchPreflightRecord(record)
	if err != nil {
		return nil, err
	}
	if researchPreflightExpired(preflight.ExpiresAt, s.now()) {
		return nil, ErrResearchPreflightExpired
	}
	return preflight, nil
}

func (s *ResearchStore) DeleteExpiredResearchPreflights(limit int) (int, error) {
	if limit <= 0 || limit > researchPreflightCleanupMax {
		return 0, fmt.Errorf("research preflight cleanup limit must be between 1 and %d", researchPreflightCleanupMax)
	}
	now := formatResearchPreflightTimestamp(s.now())
	result, err := s.db.Exec(`DELETE FROM research_preflights WHERE preflight_id IN (
		SELECT preflight_id FROM research_preflights
		WHERE expires_at <= ?
		ORDER BY expires_at, preflight_id LIMIT ?
	)`, now, limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

type researchPreflightPayload struct {
	candidatesJSON, checksJSON, gapsJSON string
}

func encodeResearchPreflightPayload(preflight ResearchPreflight) (researchPreflightPayload, error) {
	var payload researchPreflightPayload
	fields := []struct {
		value  any
		target *string
	}{
		{preflight.Candidates, &payload.candidatesJSON},
		{preflight.Checks, &payload.checksJSON},
		{preflight.Gaps, &payload.gapsJSON},
	}
	for _, field := range fields {
		encoded, err := marshalBoundedResearchPreflightJSON(field.value)
		if err != nil {
			return payload, err
		}
		*field.target = string(encoded)
	}
	return payload, nil
}

func queryResearchPreflightRecord(queryer researchRowQuerier, preflightID string) (researchPreflightRecord, error) {
	var record researchPreflightRecord
	err := queryer.QueryRow(`SELECT preflight_id, owner_hash, request_hash, status,
		candidates_json, checks_json, gaps_json, parent_run_id, created_at, expires_at
		FROM research_preflights WHERE preflight_id = ?`, preflightID).Scan(
		&record.preflightID, &record.ownerHash, &record.requestHash, &record.status,
		&record.candidatesJSON, &record.checksJSON, &record.gapsJSON,
		&record.parentRunID, &record.createdAt, &record.expiresAt)
	return record, err
}

func decodeResearchPreflightRecord(record researchPreflightRecord) (*ResearchPreflight, error) {
	if err := validateResearchPreflightID(record.preflightID); err != nil {
		return nil, fmt.Errorf("persisted research preflight is invalid: %w", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, fmt.Errorf("persisted research preflight is invalid: %w", err)
	}
	if !validResearchPreflightRequestHash(record.requestHash) {
		return nil, fmt.Errorf("persisted research preflight request hash is invalid")
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, record.createdAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, record.expiresAt)
	if createdErr != nil || expiresErr != nil ||
		record.createdAt != formatResearchPreflightTimestamp(createdAt) ||
		record.expiresAt != formatResearchPreflightTimestamp(expiresAt) ||
		expiresAt.Sub(createdAt) != researchPreflightStoreTTL {
		return nil, fmt.Errorf("persisted research preflight timestamps are invalid")
	}
	preflight := &ResearchPreflight{
		PreflightID: record.preflightID,
		RequestHash: record.requestHash,
		Status:      record.status,
		ParentRunID: record.parentRunID,
		CreatedAt:   record.createdAt,
		ExpiresAt:   record.expiresAt,
	}
	if err := decodeBoundedResearchPreflightJSON(record.candidatesJSON, &preflight.Candidates); err != nil {
		return nil, err
	}
	if err := decodeBoundedResearchPreflightJSON(record.checksJSON, &preflight.Checks); err != nil {
		return nil, err
	}
	if err := decodeBoundedResearchPreflightJSON(record.gapsJSON, &preflight.Gaps); err != nil {
		return nil, err
	}
	if err := ValidateResearchPreflight(*preflight); err != nil {
		return nil, fmt.Errorf("persisted research preflight is invalid: %w", err)
	}
	return preflight, nil
}

func marshalBoundedResearchPreflightJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > researchPreflightJSONMaxBytes {
		return nil, fmt.Errorf("research preflight structured data exceeds %d bytes", researchPreflightJSONMaxBytes)
	}
	return encoded, nil
}

func decodeBoundedResearchPreflightJSON(raw string, target any) error {
	if len(raw) == 0 || len(raw) > researchPreflightJSONMaxBytes {
		return fmt.Errorf("persisted research preflight structured data exceeds bounds")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("persisted research preflight structured data is invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("persisted research preflight structured data is invalid")
	}
	canonical, err := marshalBoundedResearchPreflightJSON(target)
	if err != nil || string(canonical) != raw {
		return fmt.Errorf("persisted research preflight structured data is not canonical")
	}
	return nil
}

func hashResearchPreflightRequest(request ResearchPreflightRequest) (string, error) {
	normalized, err := NormalizeResearchPreflightRequest(request)
	if err != nil {
		return "", err
	}
	canonical := struct {
		SchemaVersion     string   `json:"schema_version"`
		Question          string   `json:"question"`
		Mode              string   `json:"mode"`
		RequestedSources  []string `json:"requested_sources"`
		PackageConstraint string   `json:"package_constraint"`
		ParentRunID       string   `json:"parent_run_id"`
	}{
		SchemaVersion: researchPreflightRequestSchemaVersion, Question: normalized.Question,
		Mode: normalized.Mode, RequestedSources: normalized.RequestedSources,
		PackageConstraint: normalized.PackageConstraint, ParentRunID: normalized.ParentRunID,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateResearchPreflightID(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len([]rune(value)) > researchPreflightIDMaxRunes {
		return fmt.Errorf("preflight_id is required and must not exceed %d characters", researchPreflightIDMaxRunes)
	}
	return nil
}

func validateResearchPreflightOwnerHash(value string) error {
	raw, err := hex.DecodeString(value)
	if len(value) != researchPreflightOwnerHashMaxRunes || value != strings.ToLower(value) || err != nil || len(raw) != sha256.Size {
		return fmt.Errorf("owner_hash must be a lowercase SHA-256 digest")
	}
	return nil
}

func validResearchPreflightRequestHash(value string) bool {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return strings.HasPrefix(value, "sha256:") && err == nil && len(raw) == sha256.Size
}

func researchPreflightExpired(value string, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || !expiresAt.After(now.UTC())
}

func formatResearchPreflightTimestamp(value time.Time) string {
	return value.UTC().Format(researchPreflightTimestampLayout)
}

func newResearchPreflightID(reader io.Reader) (string, error) {
	var randomBytes [16]byte
	if _, err := io.ReadFull(reader, randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate research preflight random id: %w", err)
	}
	return "research-preflight-" + hex.EncodeToString(randomBytes[:]), nil
}
