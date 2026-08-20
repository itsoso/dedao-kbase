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

	"github.com/mattn/go-sqlite3"
)

const (
	researchPreflightRequestSchemaVersion = "research-preflight-request/v2"
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
	ErrResearchPreflightUnavailable         = errors.New("research preflight store unavailable")
	ErrResearchPreflightCorrupt             = errors.New("research preflight persisted data is corrupt")
	ErrResearchPreflightRequired            = errors.New("research preflight is required")
	ErrResearchPreflightRequestChanged      = errors.New("research preflight request changed")
	ErrResearchPreflightCandidate           = errors.New("research preflight candidate is not selected")
	ErrResearchPreflightPackageChanged      = errors.New("research preflight package changed")
	ErrResearchPreflightReadinessChanged    = errors.New("research preflight readiness changed")
	ErrResearchPreflightBlocked             = errors.New("research preflight is blocked")
	ErrResearchPreflightConsumed            = errors.New("research preflight is already consumed")
)

type researchPreflightRecord struct {
	preflightID, ownerHash, requestHash, status   string
	candidatesJSON, checksJSON, gapsJSON          string
	mode, requestedSourcesJSON, subjectIDsJSON    string
	packageConstraint                             string
	parentRunID, boundRunID, createdAt, expiresAt string
}

type ResearchRunConfirmation struct {
	OwnerHash         string
	Input             ResearchRunInput
	SelectedCandidate ResearchPreflightCandidate
}

func (s *ResearchStore) ReplayConfirmedResearchRun(
	ownerHash string,
	input ResearchRunInput,
) (*ResearchRun, bool, error) {
	if err := validateResearchPreflightOwnerHash(ownerHash); err != nil {
		return nil, false, err
	}
	normalizedRequest, err := normalizeResearchRunConfirmationRequest(input.Request)
	if err != nil {
		return nil, false, err
	}
	input.Request = normalizedRequest
	if strings.TrimSpace(input.IdempotencyKey) == "" || len([]rune(input.IdempotencyKey)) > researchIdempotencyKeyMax {
		return nil, false, fmt.Errorf("idempotency_key is required and must not exceed %d characters", researchIdempotencyKeyMax)
	}
	if input.Mode != ResearchModeQuick && input.Mode != ResearchModeDeep {
		return nil, false, fmt.Errorf("resolved mode must be quick or deep")
	}
	if len(input.RouteReasons) == 0 || len(input.RouteReasons) > researchRouteReasonsMax {
		return nil, false, fmt.Errorf("route_reasons must contain 1 to %d items", researchRouteReasonsMax)
	}
	preflightID := input.Request.PreflightID
	record, err := queryResearchPreflightRecord(s.db, preflightID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrResearchPreflightNotFound
	}
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("load research run authority precheck", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, false, corruptResearchPreflightError("validate authority precheck owner", err)
	}
	if record.ownerHash != ownerHash {
		return nil, false, ErrResearchPreflightOwner
	}
	if record.boundRunID == "" {
		preflight, err := decodeResearchPreflightRecord(record)
		if err != nil {
			return nil, false, err
		}
		if researchPreflightExpired(preflight.ExpiresAt, s.now()) {
			return nil, false, ErrResearchPreflightExpired
		}
		if preflight.Status != ResearchPreflightStatusReady {
			return nil, false, ErrResearchPreflightBlocked
		}
		return nil, false, nil
	}
	if !validResearchPreflightResourceID(record.boundRunID) || len([]rune(record.boundRunID)) > researchPackageIDMaxRunes {
		return nil, false, corruptResearchPreflightError("validate authority precheck bound run", nil)
	}
	var existingKey, storedHash string
	if err := s.db.QueryRow(`SELECT idempotency_key, request_hash FROM research_runs WHERE run_id = ?`,
		record.boundRunID).Scan(&existingKey, &storedHash); errors.Is(err, sql.ErrNoRows) {
		return nil, false, corruptResearchPreflightError("authority precheck bound run is missing", err)
	} else if err != nil {
		return nil, false, classifyResearchPreflightStoreError("load authority precheck bound run", err)
	}
	if existingKey != input.IdempotencyKey {
		return nil, false, ErrResearchPreflightConsumed
	}
	expectedHash, err := hashResearchRunInput(input)
	if err != nil {
		return nil, false, err
	}
	if expectedHash != storedHash {
		return nil, false, ErrResearchRunIdempotencyConflict
	}
	run, err := loadResearchRun(s.db, record.boundRunID)
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("decode research run replay", err)
	}
	var ownerCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM research_http_owners WHERE run_id = ? AND owner_hash = ?`,
		run.RunID, ownerHash).Scan(&ownerCount); err != nil {
		return nil, false, classifyResearchPreflightStoreError("validate research run replay owner", err)
	}
	if ownerCount != 1 || run.PreflightID != preflightID {
		return nil, false, corruptResearchPreflightError("validate authority precheck run binding", nil)
	}
	return run, true, nil
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
		return nil, classifyResearchPreflightStoreError("begin preflight save", err)
	}
	defer func() { _ = tx.Rollback() }()
	requestedSourcesJSON, err := marshalBoundedResearchPreflightJSON(normalized.RequestedSources)
	if err != nil {
		return nil, err
	}
	subjectIDsJSON, err := marshalBoundedResearchPreflightJSON(normalized.SubjectIDs)
	if err != nil {
		return nil, err
	}
	inserted, err := tx.Exec(`INSERT INTO research_preflights (
		preflight_id, owner_hash, request_hash, status, candidates_json, checks_json, gaps_json,
		mode, requested_sources_json, subject_ids_json, package_constraint, parent_run_id, bound_run_id, created_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
	ON CONFLICT(preflight_id) DO NOTHING`,
		result.PreflightID, ownerHash, result.RequestHash, result.Status,
		incoming.candidatesJSON, incoming.checksJSON, incoming.gapsJSON,
		normalized.Mode, string(requestedSourcesJSON), string(subjectIDsJSON), normalized.PackageConstraint,
		result.ParentRunID, result.CreatedAt, result.ExpiresAt)
	if err != nil {
		return nil, classifyResearchPreflightStoreError("insert preflight", err)
	}
	insertedCount, err := inserted.RowsAffected()
	if err != nil {
		return nil, classifyResearchPreflightStoreError("read preflight insert result", err)
	}
	if insertedCount == 1 {
		if err := tx.Commit(); err != nil {
			return nil, classifyResearchPreflightStoreError("commit preflight insert", err)
		}
		return &result, nil
	}
	if insertedCount != 0 {
		return nil, fmt.Errorf("research preflight insert returned an invalid row count")
	}

	record, err := queryResearchPreflightRecord(tx, result.PreflightID)
	if err != nil {
		return nil, classifyResearchPreflightStoreError("load preflight replay", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, corruptResearchPreflightError("validate persisted owner", err)
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
		record.mode != normalized.Mode || record.requestedSourcesJSON != string(requestedSourcesJSON) ||
		record.subjectIDsJSON != string(subjectIDsJSON) ||
		record.packageConstraint != normalized.PackageConstraint || record.boundRunID != "" ||
		record.candidatesJSON != incoming.candidatesJSON ||
		record.checksJSON != incoming.checksJSON || record.gapsJSON != incoming.gapsJSON {
		return nil, ErrResearchPreflightIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyResearchPreflightStoreError("commit preflight replay", err)
	}
	return existing, nil
}

func (s *ResearchStore) ConfirmResearchRun(confirmation ResearchRunConfirmation) (*ResearchRun, bool, error) {
	normalizedRequest, err := normalizeResearchRunConfirmationRequest(confirmation.Input.Request)
	if err != nil {
		return nil, false, err
	}
	confirmation.Input.Request = normalizedRequest
	preflightID := strings.TrimSpace(confirmation.Input.Request.PreflightID)
	if preflightID == "" {
		return nil, false, ErrResearchPreflightRequired
	}
	if err := validateResearchPreflightID(preflightID); err != nil {
		return nil, false, err
	}
	if err := validateResearchPreflightOwnerHash(confirmation.OwnerHash); err != nil {
		return nil, false, err
	}
	if err := validateResearchRunInput(confirmation.Input); err != nil {
		return nil, false, err
	}
	selected := confirmation.SelectedCandidate
	if strings.TrimSpace(selected.PackageID) != strings.TrimSpace(confirmation.Input.Request.PackageID) ||
		strings.TrimSpace(selected.PackageVersion) != strings.TrimSpace(confirmation.Input.Request.PackageVersion) {
		return nil, false, ErrResearchPreflightCandidate
	}
	requestHash, err := hashResearchRunInput(confirmation.Input)
	if err != nil {
		return nil, false, err
	}
	return s.confirmResearchRunOnce(confirmation, requestHash)
}

func (s *ResearchStore) confirmResearchRunOnce(
	confirmation ResearchRunConfirmation,
	requestHash string,
) (*ResearchRun, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("begin research run confirmation", err)
	}
	defer func() { _ = tx.Rollback() }()
	preflightID := strings.TrimSpace(confirmation.Input.Request.PreflightID)
	record, err := queryResearchPreflightRecord(tx, preflightID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrResearchPreflightNotFound
	}
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("load confirmation preflight", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, false, corruptResearchPreflightError("validate confirmation owner", err)
	}
	if record.ownerHash != confirmation.OwnerHash {
		return nil, false, ErrResearchPreflightOwner
	}
	if record.boundRunID != "" {
		var existingKey, existingHash string
		err := tx.QueryRow(`SELECT idempotency_key, request_hash FROM research_runs WHERE run_id = ?`, record.boundRunID).Scan(&existingKey, &existingHash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, corruptResearchPreflightError("bound run is missing", err)
		}
		if err != nil {
			return nil, false, classifyResearchPreflightStoreError("load bound run", err)
		}
		if existingKey == confirmation.Input.IdempotencyKey {
			if existingHash != requestHash {
				return nil, false, ErrResearchRunIdempotencyConflict
			}
			var ownerCount int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM research_http_owners WHERE run_id = ? AND owner_hash = ?`,
				record.boundRunID, confirmation.OwnerHash).Scan(&ownerCount); err != nil {
				return nil, false, classifyResearchPreflightStoreError("validate bound run owner", err)
			}
			if ownerCount != 1 {
				return nil, false, corruptResearchPreflightError("bound run owner is missing", nil)
			}
			run, err := loadResearchRun(tx, record.boundRunID)
			return run, false, err
		}
		return nil, false, ErrResearchPreflightConsumed
	}
	preflight, err := decodeResearchPreflightRecord(record)
	if err != nil {
		return nil, false, err
	}
	if researchPreflightExpired(preflight.ExpiresAt, s.now()) {
		return nil, false, ErrResearchPreflightExpired
	}

	normalizedRequest, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
		Mode: confirmation.Input.Request.Mode, Question: confirmation.Input.Request.Question,
		RequestedSources:  confirmation.Input.Request.RequestedSources,
		SubjectIDs:        confirmation.Input.Request.SubjectIDs,
		PackageConstraint: record.packageConstraint, ParentRunID: record.parentRunID,
	})
	if err != nil {
		return nil, false, ErrResearchPreflightRequestChanged
	}
	normalizedSourcesJSON, err := marshalBoundedResearchPreflightJSON(normalizedRequest.RequestedSources)
	if err != nil {
		return nil, false, err
	}
	normalizedSubjectsJSON, err := marshalBoundedResearchPreflightJSON(normalizedRequest.SubjectIDs)
	if err != nil {
		return nil, false, err
	}
	preflightRequestHash, err := hashResearchPreflightRequest(normalizedRequest)
	if err != nil {
		return nil, false, err
	}
	if record.mode != normalizedRequest.Mode || record.requestedSourcesJSON != string(normalizedSourcesJSON) ||
		record.subjectIDsJSON != string(normalizedSubjectsJSON) ||
		preflightRequestHash != record.requestHash {
		return nil, false, ErrResearchPreflightRequestChanged
	}

	if preflight.Status != ResearchPreflightStatusReady {
		return nil, false, ErrResearchPreflightBlocked
	}
	for _, check := range preflight.Checks {
		if check.Status == ResearchPreflightCheckBlocked {
			return nil, false, ErrResearchPreflightBlocked
		}
	}

	var snapshot *ResearchPreflightCandidate
	for index := range preflight.Candidates {
		candidate := &preflight.Candidates[index]
		if candidate.PackageID == strings.TrimSpace(confirmation.Input.Request.PackageID) &&
			candidate.PackageVersion == strings.TrimSpace(confirmation.Input.Request.PackageVersion) {
			snapshot = candidate
			break
		}
	}
	if snapshot == nil {
		return nil, false, ErrResearchPreflightCandidate
	}
	if snapshot.Readiness == ResearchPreflightCheckBlocked {
		return nil, false, ErrResearchPreflightBlocked
	}
	selected := confirmation.SelectedCandidate
	if selected.PackageID != snapshot.PackageID || selected.PackageVersion != snapshot.PackageVersion ||
		selected.ContentHash != snapshot.ContentHash {
		return nil, false, ErrResearchPreflightPackageChanged
	}
	if selected.Readiness != snapshot.Readiness || selected.Budget != snapshot.Budget ||
		selected.Budget.ResolvedMode != confirmation.Input.Mode || selected.Budget.Limits != confirmation.Input.Budget {
		return nil, false, ErrResearchPreflightReadinessChanged
	}
	if record.parentRunID != "" {
		var parentCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM research_http_owners WHERE run_id = ? AND owner_hash = ?`,
			record.parentRunID, confirmation.OwnerHash).Scan(&parentCount); err != nil {
			return nil, false, classifyResearchPreflightStoreError("validate parent run owner", err)
		}
		if parentCount != 1 {
			return nil, false, ErrResearchRunNotFound
		}
	}

	var existingID, existingHash string
	err = tx.QueryRow(`SELECT run_id, request_hash FROM research_runs WHERE idempotency_key = ?`,
		confirmation.Input.IdempotencyKey).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != requestHash || existingID != record.boundRunID {
			return nil, false, ErrResearchRunIdempotencyConflict
		}
		run, err := loadResearchRun(tx, existingID)
		return run, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, classifyResearchPreflightStoreError("load confirmation idempotency", err)
	}

	now := s.now().UTC().Format(time.RFC3339Nano)
	run := &ResearchRun{
		SchemaVersion: ResearchRunSchemaVersion, RunID: newResearchRunID(), ParentRunID: record.parentRunID,
		PreflightID: preflightID, Mode: confirmation.Input.Mode,
		Question: strings.TrimSpace(confirmation.Input.Request.Question), Status: ResearchPlanning,
		PackageID:        strings.TrimSpace(confirmation.Input.Request.PackageID),
		PackageVersion:   strings.TrimSpace(confirmation.Input.Request.PackageVersion),
		SubjectIDs:       append([]string(nil), confirmation.Input.Request.SubjectIDs...),
		RequestedSources: append([]string(nil), confirmation.Input.Request.RequestedSources...),
		RouteReasons:     append([]string(nil), confirmation.Input.RouteReasons...), Budget: confirmation.Input.Budget,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	values, err := marshalResearchRunValues(run)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.Exec(`INSERT INTO research_runs (
		run_id, parent_run_id, preflight_id, idempotency_key, request_hash, schema_version, mode, question, status,
		package_id, package_version, subject_ids_json, requested_sources_json, route_reasons_json,
		actual_scope_json, budget_json, wait_reason, failure_json, version, created_at, updated_at,
		lease_owner, lease_epoch, lease_expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.ParentRunID, run.PreflightID, confirmation.Input.IdempotencyKey, requestHash,
		run.SchemaVersion, run.Mode, run.Question, run.Status, run.PackageID, run.PackageVersion,
		values.subjectIDs, values.requestedSources, values.routeReasons, values.actualScope, values.budget,
		run.WaitReason, values.failure, run.Version, run.CreatedAt, run.UpdatedAt,
		run.LeaseOwner, run.LeaseEpoch, run.LeaseExpiresAt)
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("insert confirmed research run", err)
	}
	if err := insertResearchEvent(tx, run.RunID, "", run.Status, ResearchTransition{Code: "run_created", Actor: "orchestrator"}, now); err != nil {
		return nil, false, classifyResearchPreflightStoreError("insert confirmed research event", err)
	}
	if _, err := tx.Exec(`INSERT INTO research_http_owners(run_id, owner_hash, created_at) VALUES (?, ?, ?)`,
		run.RunID, confirmation.OwnerHash, now); err != nil {
		return nil, false, classifyResearchPreflightStoreError("bind confirmed research owner", err)
	}
	bound, err := tx.Exec(`UPDATE research_preflights SET bound_run_id = ? WHERE preflight_id = ? AND bound_run_id = ''`,
		run.RunID, preflightID)
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("bind confirmed research preflight", err)
	}
	boundCount, err := bound.RowsAffected()
	if err != nil {
		return nil, false, classifyResearchPreflightStoreError("read confirmed preflight binding", err)
	}
	if boundCount != 1 {
		return nil, false, ErrResearchPreflightConsumed
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifyResearchPreflightStoreError("commit research run confirmation", err)
	}
	return run, true, nil
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
		return nil, classifyResearchPreflightStoreError("load preflight", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, corruptResearchPreflightError("validate persisted owner", err)
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
		return 0, classifyResearchPreflightStoreError("delete expired preflights", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, classifyResearchPreflightStoreError("read expired preflight delete result", err)
	}
	return int(deleted), nil
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
		candidates_json, checks_json, gaps_json, mode, requested_sources_json, subject_ids_json, package_constraint,
		parent_run_id, bound_run_id, created_at, expires_at
		FROM research_preflights WHERE preflight_id = ?`, preflightID).Scan(
		&record.preflightID, &record.ownerHash, &record.requestHash, &record.status,
		&record.candidatesJSON, &record.checksJSON, &record.gapsJSON,
		&record.mode, &record.requestedSourcesJSON, &record.subjectIDsJSON, &record.packageConstraint,
		&record.parentRunID, &record.boundRunID, &record.createdAt, &record.expiresAt)
	return record, err
}

func decodeResearchPreflightRecord(record researchPreflightRecord) (*ResearchPreflight, error) {
	if err := validateResearchPreflightID(record.preflightID); err != nil {
		return nil, corruptResearchPreflightError("validate persisted preflight id", err)
	}
	if err := validateResearchPreflightOwnerHash(record.ownerHash); err != nil {
		return nil, corruptResearchPreflightError("validate persisted owner", err)
	}
	if !validResearchPreflightRequestHash(record.requestHash) {
		return nil, corruptResearchPreflightError("validate persisted request hash", nil)
	}
	createdAt, createdErr := time.Parse(time.RFC3339Nano, record.createdAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, record.expiresAt)
	if createdErr != nil || expiresErr != nil ||
		record.createdAt != formatResearchPreflightTimestamp(createdAt) ||
		record.expiresAt != formatResearchPreflightTimestamp(expiresAt) ||
		expiresAt.Sub(createdAt) != researchPreflightStoreTTL {
		return nil, corruptResearchPreflightError("validate persisted timestamps", nil)
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
		return nil, corruptResearchPreflightError("validate persisted payload", err)
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
		return corruptResearchPreflightError("decode persisted structured data", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return corruptResearchPreflightError("decode persisted structured data", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return corruptResearchPreflightError("decode persisted structured data", err)
	}
	canonical, err := marshalBoundedResearchPreflightJSON(target)
	if err != nil || string(canonical) != raw {
		return corruptResearchPreflightError("validate canonical persisted structured data", err)
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
		SubjectIDs        []string `json:"subject_ids"`
		PackageConstraint string   `json:"package_constraint"`
		ParentRunID       string   `json:"parent_run_id"`
	}{
		SchemaVersion: researchPreflightRequestSchemaVersion, Question: normalized.Question,
		Mode: normalized.Mode, RequestedSources: normalized.RequestedSources,
		SubjectIDs:        normalized.SubjectIDs,
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
	if !validResearchPreflightResourceID(value) {
		return fmt.Errorf("preflight_id must be a canonical Research resource ID")
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
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	digest := strings.TrimPrefix(value, "sha256:")
	raw, err := hex.DecodeString(digest)
	return digest == strings.ToLower(digest) && err == nil && len(raw) == sha256.Size
}

func classifyResearchPreflightStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	for _, sentinel := range []error{
		ErrResearchPreflightNotFound,
		ErrResearchPreflightExpired,
		ErrResearchPreflightOwner,
		ErrResearchPreflightIdempotencyConflict,
		ErrResearchPreflightUnavailable,
		ErrResearchPreflightCorrupt,
		ErrResearchPreflightRequired,
		ErrResearchPreflightRequestChanged,
		ErrResearchPreflightCandidate,
		ErrResearchPreflightPackageChanged,
		ErrResearchPreflightReadinessChanged,
		ErrResearchPreflightBlocked,
		ErrResearchPreflightConsumed,
	} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("%s: %w", operation, err)
		}
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked) {
		return fmt.Errorf("%w: %s: %w", ErrResearchPreflightUnavailable, operation, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrResearchPreflightUnavailable, operation, err)
}

func corruptResearchPreflightError(operation string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrResearchPreflightCorrupt, operation)
	}
	return fmt.Errorf("%w: %s: %w", ErrResearchPreflightCorrupt, operation, cause)
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
