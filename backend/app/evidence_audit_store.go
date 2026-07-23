package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const evidenceAuditStoreVersion = "2"

const (
	evidenceAuditMaxManifestAudits      = 10000
	evidenceAuditMaxManifestIdempotency = 20000
	evidenceAuditRootLockWait           = 5 * time.Second
	evidenceAuditRootLockStaleAfter     = 2 * time.Minute
)

var (
	ErrEvidenceAuditIdempotencyConflict = errors.New("evidence audit idempotency conflict")
	ErrEvidenceAuditImmutable           = errors.New("completed evidence audit is immutable")
	ErrEvidenceAuditStateConflict       = errors.New("evidence audit state conflict")
)

type EvidenceAuditRecord struct {
	AuditID            string `json:"audit_id"`
	Status             string `json:"status"`
	PackageID          string `json:"package_id"`
	PackageVersion     string `json:"package_version"`
	PackageContentHash string `json:"package_content_hash"`
	InputHash          string `json:"input_hash"`
	OutputHash         string `json:"output_hash,omitempty"`
	TraceID            string `json:"trace_id,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	StartedAt          string `json:"started_at,omitempty"`
	CompletedAt        string `json:"completed_at,omitempty"`
	FailedAt           string `json:"failed_at,omitempty"`
	FailureCode        string `json:"failure_code,omitempty"`
	FailureSummary     string `json:"failure_summary,omitempty"`
}

type EvidenceAuditIdempotencyRecord struct {
	IdempotencyIdentity string `json:"idempotency_identity"`
	AuditID             string `json:"audit_id"`
	InputHash           string `json:"input_hash"`
}

var writeEvidenceAuditManifestFile = writeFileAtomically

type EvidenceAuditManifest struct {
	Version     string                           `json:"version"`
	UpdatedAt   string                           `json:"updated_at"`
	Audits      []EvidenceAuditRecord            `json:"audits"`
	Idempotency []EvidenceAuditIdempotencyRecord `json:"idempotency"`
}

type evidenceAuditPreparedRecord struct {
	Version    string `json:"version"`
	AuditID    string `json:"audit_id"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
}

func (s *BookKnowledgeStore) EvidenceAuditDir() string {
	return filepath.Join(s.root, "agent-audits")
}

func (s *BookKnowledgeStore) EvidenceAuditManifestPath() string {
	return filepath.Join(s.EvidenceAuditDir(), "manifest.json")
}

func (s *BookKnowledgeStore) EvidenceAuditInputPath(inputHash string) string {
	return filepath.Join(s.EvidenceAuditDir(), "inputs", evidenceAuditHashName(inputHash)+".json")
}

func (s *BookKnowledgeStore) EvidenceAuditReportPath(outputHash string) string {
	return filepath.Join(s.EvidenceAuditDir(), "reports", evidenceAuditHashName(outputHash)+".json")
}

func (s *BookKnowledgeStore) EvidenceAuditPreparedPath(auditID string) string {
	return filepath.Join(s.EvidenceAuditDir(), "prepared", sanitizeBookKnowledgeID(auditID)+".json")
}

func CreateEvidenceAudit(store *BookKnowledgeStore, input EvidenceAuditInput, idempotencyKey string, now time.Time) (*EvidenceAudit, bool, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, false, fmt.Errorf("idempotency_key is required")
	}
	idempotencyIdentity := evidenceAuditOpaqueIdentity(idempotencyKey)
	normalized, err := normalizeEvidenceAuditInput(input)
	if err != nil {
		return nil, false, err
	}
	inputHash, err := EvidenceAuditInputHash(normalized)
	if err != nil {
		return nil, false, err
	}
	normalized.InputHash = inputHash
	if now.IsZero() {
		now = time.Now()
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)

	store.mu.Lock()
	defer store.mu.Unlock()
	unlockRoot, err := store.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, false, err
	}
	defer unlockRoot()
	if err := os.MkdirAll(store.EvidenceAuditDir(), os.ModePerm); err != nil {
		return nil, false, err
	}
	manifest, err := store.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, false, err
	}
	for _, record := range manifest.Idempotency {
		if record.IdempotencyIdentity != idempotencyIdentity {
			continue
		}
		if record.InputHash != inputHash {
			return nil, false, fmt.Errorf("%w: opaque identity already references different input", ErrEvidenceAuditIdempotencyConflict)
		}
		audit, err := store.loadEvidenceAuditByIDUnlocked(manifest, record.AuditID)
		return audit, false, err
	}
	for _, record := range manifest.Audits {
		if record.PackageID != normalized.Package.PackageID ||
			record.PackageVersion != normalized.Package.Version ||
			record.InputHash != inputHash {
			continue
		}
		if len(manifest.Idempotency) >= evidenceAuditMaxManifestIdempotency {
			return nil, false, fmt.Errorf(
				"evidence audit manifest idempotency capacity %d reached",
				evidenceAuditMaxManifestIdempotency,
			)
		}
		manifest.Idempotency = append(manifest.Idempotency, EvidenceAuditIdempotencyRecord{
			IdempotencyIdentity: idempotencyIdentity,
			AuditID:             record.AuditID,
			InputHash:           inputHash,
		})
		manifest.UpdatedAt = timestamp
		if err := store.writeEvidenceAuditManifestUnlocked(manifest); err != nil {
			return nil, false, err
		}
		audit, err := store.loadEvidenceAuditRecordUnlocked(record)
		return audit, false, err
	}
	if len(manifest.Audits) >= evidenceAuditMaxManifestAudits {
		return nil, false, fmt.Errorf(
			"evidence audit manifest audit capacity %d reached",
			evidenceAuditMaxManifestAudits,
		)
	}
	if len(manifest.Idempotency) >= evidenceAuditMaxManifestIdempotency {
		return nil, false, fmt.Errorf(
			"evidence audit manifest idempotency capacity %d reached",
			evidenceAuditMaxManifestIdempotency,
		)
	}
	if err := store.writeEvidenceAuditInputUnlocked(normalized); err != nil {
		return nil, false, err
	}
	auditID := "audit-" + strings.TrimPrefix(inputHash, "sha256:")[:24]
	record := EvidenceAuditRecord{
		AuditID:            auditID,
		Status:             EvidenceAuditQueued,
		PackageID:          normalized.Package.PackageID,
		PackageVersion:     normalized.Package.Version,
		PackageContentHash: normalized.Package.ContentHash,
		InputHash:          inputHash,
		CreatedAt:          timestamp,
		UpdatedAt:          timestamp,
	}
	manifest.Version = evidenceAuditStoreVersion
	manifest.UpdatedAt = timestamp
	manifest.Audits = append(manifest.Audits, record)
	manifest.Idempotency = append(manifest.Idempotency, EvidenceAuditIdempotencyRecord{
		IdempotencyIdentity: idempotencyIdentity,
		AuditID:             auditID,
		InputHash:           inputHash,
	})
	if err := store.writeEvidenceAuditManifestUnlocked(manifest); err != nil {
		return nil, false, err
	}
	audit := evidenceAuditFromInput(normalized, record, idempotencyIdentity)
	return &audit, true, nil
}

func StartEvidenceAudit(store *BookKnowledgeStore, auditID, traceID string, now time.Time) (*EvidenceAudit, error) {
	return updateEvidenceAuditRecord(store, auditID, now, func(record *EvidenceAuditRecord) error {
		if record.Status != EvidenceAuditQueued {
			if record.Status == EvidenceAuditCompleted {
				return ErrEvidenceAuditImmutable
			}
			return fmt.Errorf("%w: cannot start audit in %q", ErrEvidenceAuditStateConflict, record.Status)
		}
		traceID = strings.TrimSpace(traceID)
		if traceID == "" {
			return fmt.Errorf("trace_id is required")
		}
		record.Status = EvidenceAuditRunning
		record.TraceID = traceID
		record.StartedAt = evidenceAuditTimestamp(now)
		return nil
	})
}

func FailEvidenceAudit(
	store *BookKnowledgeStore,
	auditID, code, summary string,
	now time.Time,
) (*EvidenceAudit, error) {
	return updateEvidenceAuditRecord(store, auditID, now, func(record *EvidenceAuditRecord) error {
		if record.Status == EvidenceAuditCompleted {
			return ErrEvidenceAuditImmutable
		}
		if record.Status != EvidenceAuditQueued && record.Status != EvidenceAuditRunning {
			return fmt.Errorf("%w: cannot fail audit in %q", ErrEvidenceAuditStateConflict, record.Status)
		}
		code = sanitizeEvidenceAuditFailureCode(code)
		if code == "" {
			return fmt.Errorf("failure code is required")
		}
		if strings.TrimSpace(summary) == "" {
			return fmt.Errorf("failure summary is required")
		}
		record.Status = EvidenceAuditFailed
		record.FailedAt = evidenceAuditTimestamp(now)
		record.FailureCode = code
		record.FailureSummary = sanitizeEvidenceAuditFailureSummary(summary)
		record.OutputHash = ""
		return nil
	})
}

func CompleteEvidenceAudit(store *BookKnowledgeStore, report EvidenceAudit, now time.Time) (*EvidenceAudit, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	unlockRoot, err := store.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, err
	}
	defer unlockRoot()
	manifest, err := store.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	record := findEvidenceAuditRecord(manifest, strings.TrimSpace(report.AuditID))
	if record == nil {
		return nil, fmt.Errorf("%w: evidence audit %q", os.ErrNotExist, report.AuditID)
	}
	if record.Status == EvidenceAuditCompleted {
		return nil, ErrEvidenceAuditImmutable
	}
	if record.Status != EvidenceAuditRunning {
		return nil, fmt.Errorf("%w: cannot complete audit in %q", ErrEvidenceAuditStateConflict, record.Status)
	}
	prepared, err := store.findPreparedEvidenceAuditReportUnlocked(record.AuditID, record.InputHash)
	if err != nil {
		return nil, err
	}
	if prepared != nil {
		if !evidenceAuditReportContentEqual(*prepared, report) {
			return nil, ErrEvidenceAuditImmutable
		}
		record.Status = EvidenceAuditCompleted
		record.OutputHash = prepared.OutputHash
		record.UpdatedAt = prepared.UpdatedAt
		record.CompletedAt = prepared.CompletedAt
		manifest.UpdatedAt = prepared.UpdatedAt
		if err := store.writeEvidenceAuditManifestUnlocked(manifest); err != nil {
			return nil, err
		}
		_ = os.Remove(store.EvidenceAuditPreparedPath(record.AuditID))
		return prepared, nil
	}
	input, err := store.loadEvidenceAuditInputUnlocked(record.InputHash)
	if err != nil {
		return nil, err
	}
	idempotencyKey := evidenceAuditIdempotencyKey(manifest, record.AuditID)
	report.SchemaVersion = EvidenceAuditSchemaVersion
	report.AuditID = record.AuditID
	report.Status = EvidenceAuditCompleted
	report.CreatedAt = record.CreatedAt
	report.UpdatedAt = evidenceAuditTimestamp(now)
	report.StartedAt = record.StartedAt
	report.CompletedAt = evidenceAuditTimestamp(now)
	report.FailedAt = ""
	report.FailureCode = ""
	report.FailureSummary = ""
	report.IdempotencyKey = idempotencyKey
	report.InputHash = record.InputHash
	report.Package = input.Package
	report.EvidencePolicy = input.EvidencePolicy
	report.Model = input.Model
	report.Retrieval = input.Retrieval
	report.Releases = input.Releases
	report.Subject = input.Subject
	report.Scope = input.Scope
	report.SelectedClaims = input.SelectedClaims
	report.TraceID = record.TraceID
	report.OutputHash = ""
	finalized, err := FinalizeEvidenceAuditReport(report)
	if err != nil {
		return nil, err
	}
	if err := store.writeEvidenceAuditReportUnlocked(finalized); err != nil {
		return nil, err
	}
	if err := store.writePreparedEvidenceAuditReportUnlocked(finalized); err != nil {
		return nil, err
	}
	record.Status = EvidenceAuditCompleted
	record.OutputHash = finalized.OutputHash
	record.UpdatedAt = finalized.UpdatedAt
	record.CompletedAt = finalized.CompletedAt
	manifest.UpdatedAt = finalized.UpdatedAt
	if err := store.writeEvidenceAuditManifestUnlocked(manifest); err != nil {
		return nil, err
	}
	_ = os.Remove(store.EvidenceAuditPreparedPath(record.AuditID))
	return &finalized, nil
}

func (s *BookKnowledgeStore) LoadEvidenceAudit(auditID string) (*EvidenceAudit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	unlockRoot, err := s.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, err
	}
	defer unlockRoot()
	manifest, err := s.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	return s.loadEvidenceAuditByIDUnlocked(manifest, strings.TrimSpace(auditID))
}

func (s *BookKnowledgeStore) ListEvidenceAudits(packageID, version string, limit int) ([]EvidenceAuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	unlockRoot, err := s.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, err
	}
	defer unlockRoot()
	manifest, err := s.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	records := make([]EvidenceAuditRecord, 0, len(manifest.Audits))
	for _, record := range manifest.Audits {
		if packageID != "" && record.PackageID != packageID {
			continue
		}
		if version != "" && record.PackageVersion != version {
			continue
		}
		records = append(records, record)
		if len(records) == limit {
			break
		}
	}
	return records, nil
}

func updateEvidenceAuditRecord(store *BookKnowledgeStore, auditID string, now time.Time, update func(*EvidenceAuditRecord) error) (*EvidenceAudit, error) {
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	unlockRoot, err := store.acquireEvidenceAuditRootLock()
	if err != nil {
		return nil, err
	}
	defer unlockRoot()
	manifest, err := store.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	record := findEvidenceAuditRecord(manifest, strings.TrimSpace(auditID))
	if record == nil {
		return nil, fmt.Errorf("%w: evidence audit %q", os.ErrNotExist, auditID)
	}
	if err := update(record); err != nil {
		return nil, err
	}
	record.UpdatedAt = evidenceAuditTimestamp(now)
	manifest.UpdatedAt = record.UpdatedAt
	input, err := store.loadEvidenceAuditInputUnlocked(record.InputHash)
	if err != nil {
		return nil, err
	}
	candidate := evidenceAuditFromInput(
		input,
		*record,
		evidenceAuditIdempotencyKey(manifest, record.AuditID),
	)
	if err := ValidateEvidenceAudit(candidate); err != nil {
		return nil, err
	}
	if err := store.writeEvidenceAuditManifestUnlocked(manifest); err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (s *BookKnowledgeStore) loadEvidenceAuditByIDUnlocked(manifest *EvidenceAuditManifest, auditID string) (*EvidenceAudit, error) {
	record := findEvidenceAuditRecord(manifest, auditID)
	if record == nil {
		return nil, fmt.Errorf("%w: evidence audit %q", os.ErrNotExist, auditID)
	}
	return s.loadEvidenceAuditRecordUnlocked(*record)
}

func (s *BookKnowledgeStore) loadEvidenceAuditRecordUnlocked(record EvidenceAuditRecord) (*EvidenceAudit, error) {
	if record.Status == EvidenceAuditCompleted {
		var report EvidenceAudit
		if err := readJSONFile(s.EvidenceAuditReportPath(record.OutputHash), &report); err != nil {
			return nil, err
		}
		if report.AuditID != record.AuditID || report.InputHash != record.InputHash ||
			report.OutputHash != record.OutputHash || report.Status != record.Status {
			return nil, fmt.Errorf("evidence audit report identity does not match manifest record")
		}
		if err := ValidateEvidenceAudit(report); err != nil {
			return nil, fmt.Errorf("validate persisted evidence audit report: %w", err)
		}
		return &report, nil
	}
	input, err := s.loadEvidenceAuditInputUnlocked(record.InputHash)
	if err != nil {
		return nil, err
	}
	manifest, err := s.loadEvidenceAuditManifestUnlocked()
	if err != nil {
		return nil, err
	}
	audit := evidenceAuditFromInput(input, record, evidenceAuditIdempotencyKey(manifest, record.AuditID))
	if err := ValidateEvidenceAudit(audit); err != nil {
		return nil, fmt.Errorf("validate persisted evidence audit: %w", err)
	}
	return &audit, nil
}

func (s *BookKnowledgeStore) loadEvidenceAuditInputUnlocked(inputHash string) (EvidenceAuditInput, error) {
	var input EvidenceAuditInput
	if err := readJSONFile(s.EvidenceAuditInputPath(inputHash), &input); err != nil {
		return EvidenceAuditInput{}, err
	}
	wantHash, err := EvidenceAuditInputHash(input)
	if err != nil {
		return EvidenceAuditInput{}, err
	}
	if input.InputHash != inputHash || wantHash != inputHash {
		return EvidenceAuditInput{}, fmt.Errorf("evidence audit input content hash does not match artifact")
	}
	return input, nil
}

func (s *BookKnowledgeStore) writeEvidenceAuditInputUnlocked(input EvidenceAuditInput) error {
	path := s.EvidenceAuditInputPath(input.InputHash)
	if _, err := os.Stat(path); err == nil {
		stored, loadErr := s.loadEvidenceAuditInputUnlocked(input.InputHash)
		if loadErr != nil {
			return loadErr
		}
		storedPayload, _ := encodeJSONFile(stored)
		inputPayload, _ := encodeJSONFile(input)
		if string(storedPayload) != string(inputPayload) {
			return fmt.Errorf("immutable evidence audit input artifact conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload, err := encodeJSONFile(input)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	return writeEvidenceAuditImmutableFile(path, payload)
}

func (s *BookKnowledgeStore) writeEvidenceAuditReportUnlocked(report EvidenceAudit) error {
	path := s.EvidenceAuditReportPath(report.OutputHash)
	if _, err := os.Stat(path); err == nil {
		var stored EvidenceAudit
		if readErr := readJSONFile(path, &stored); readErr != nil {
			return readErr
		}
		storedPayload, _ := json.Marshal(stored)
		reportPayload, _ := json.Marshal(report)
		if string(storedPayload) != string(reportPayload) {
			return ErrEvidenceAuditImmutable
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload, err := encodeJSONFile(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	return writeEvidenceAuditImmutableFile(path, payload)
}

func (s *BookKnowledgeStore) loadEvidenceAuditManifestUnlocked() (*EvidenceAuditManifest, error) {
	var manifest EvidenceAuditManifest
	if err := readJSONFile(s.EvidenceAuditManifestPath(), &manifest); errors.Is(err, os.ErrNotExist) {
		return &EvidenceAuditManifest{
			Version:     evidenceAuditStoreVersion,
			Audits:      []EvidenceAuditRecord{},
			Idempotency: []EvidenceAuditIdempotencyRecord{},
		}, nil
	} else if err != nil {
		return nil, err
	}
	if manifest.Version != evidenceAuditStoreVersion {
		return nil, fmt.Errorf(
			"unsupported evidence audit manifest version %q; expected %q",
			manifest.Version,
			evidenceAuditStoreVersion,
		)
	}
	if err := validateEvidenceAuditManifestCapacity(&manifest); err != nil {
		return nil, err
	}
	sort.SliceStable(manifest.Audits, func(i, j int) bool {
		if manifest.Audits[i].CreatedAt != manifest.Audits[j].CreatedAt {
			return manifest.Audits[i].CreatedAt < manifest.Audits[j].CreatedAt
		}
		return manifest.Audits[i].AuditID < manifest.Audits[j].AuditID
	})
	return &manifest, nil
}

func (s *BookKnowledgeStore) writeEvidenceAuditManifestUnlocked(manifest *EvidenceAuditManifest) error {
	manifest.Version = evidenceAuditStoreVersion
	if err := validateEvidenceAuditManifestCapacity(manifest); err != nil {
		return err
	}
	payload, err := encodeJSONFile(manifest)
	if err != nil {
		return err
	}
	return writeEvidenceAuditManifestFile(s.EvidenceAuditManifestPath(), payload)
}

func validateEvidenceAuditManifestCapacity(manifest *EvidenceAuditManifest) error {
	if len(manifest.Audits) > evidenceAuditMaxManifestAudits {
		return fmt.Errorf("evidence audit manifest audit capacity %d exceeded", evidenceAuditMaxManifestAudits)
	}
	if len(manifest.Idempotency) > evidenceAuditMaxManifestIdempotency {
		return fmt.Errorf(
			"evidence audit manifest idempotency capacity %d exceeded",
			evidenceAuditMaxManifestIdempotency,
		)
	}
	return nil
}

func evidenceAuditFromInput(input EvidenceAuditInput, record EvidenceAuditRecord, idempotencyKey string) EvidenceAudit {
	return EvidenceAudit{
		SchemaVersion:  input.SchemaVersion,
		AuditID:        record.AuditID,
		Status:         record.Status,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
		StartedAt:      record.StartedAt,
		CompletedAt:    record.CompletedAt,
		FailedAt:       record.FailedAt,
		IdempotencyKey: idempotencyKey,
		InputHash:      record.InputHash,
		Package:        input.Package,
		EvidencePolicy: input.EvidencePolicy,
		Model:          input.Model,
		Retrieval:      input.Retrieval,
		Releases:       input.Releases,
		Subject:        input.Subject,
		Scope:          input.Scope,
		SelectedClaims: input.SelectedClaims,
		OutputHash:     record.OutputHash,
		TraceID:        record.TraceID,
		FailureCode:    record.FailureCode,
		FailureSummary: record.FailureSummary,
	}
}

func findEvidenceAuditRecord(manifest *EvidenceAuditManifest, auditID string) *EvidenceAuditRecord {
	for index := range manifest.Audits {
		if manifest.Audits[index].AuditID == auditID {
			return &manifest.Audits[index]
		}
	}
	return nil
}

func evidenceAuditIdempotencyKey(manifest *EvidenceAuditManifest, auditID string) string {
	for _, record := range manifest.Idempotency {
		if record.AuditID == auditID {
			return record.IdempotencyIdentity
		}
	}
	return ""
}

func (s *BookKnowledgeStore) findPreparedEvidenceAuditReportUnlocked(
	auditID, inputHash string,
) (*EvidenceAudit, error) {
	var prepared evidenceAuditPreparedRecord
	if err := readJSONFile(s.EvidenceAuditPreparedPath(auditID), &prepared); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if prepared.Version != evidenceAuditStoreVersion ||
		prepared.AuditID != auditID ||
		prepared.InputHash != inputHash ||
		!validEvidenceAuditSHA256(prepared.OutputHash) {
		return nil, fmt.Errorf("prepared evidence audit journal identity is invalid")
	}
	path := s.EvidenceAuditReportPath(prepared.OutputHash)
	if filepath.Base(path) != evidenceAuditHashName(prepared.OutputHash)+".json" {
		return nil, fmt.Errorf("prepared evidence audit report filename does not match output_hash")
	}
	var report EvidenceAudit
	if err := readJSONFile(path, &report); err != nil {
		return nil, err
	}
	if report.AuditID != auditID ||
		report.InputHash != inputHash ||
		report.OutputHash != prepared.OutputHash ||
		filepath.Base(path) != evidenceAuditHashName(report.OutputHash)+".json" {
		return nil, fmt.Errorf("prepared evidence audit report identity is invalid")
	}
	if err := ValidateEvidenceAudit(report); err != nil {
		return nil, fmt.Errorf("validate prepared evidence audit report: %w", err)
	}
	return &report, nil
}

func (s *BookKnowledgeStore) writePreparedEvidenceAuditReportUnlocked(report EvidenceAudit) error {
	prepared := evidenceAuditPreparedRecord{
		Version:    evidenceAuditStoreVersion,
		AuditID:    report.AuditID,
		InputHash:  report.InputHash,
		OutputHash: report.OutputHash,
	}
	payload, err := encodeJSONFile(prepared)
	if err != nil {
		return err
	}
	path := s.EvidenceAuditPreparedPath(report.AuditID)
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	return writeFileAtomically(path, payload)
}

func evidenceAuditReportContentEqual(left, right EvidenceAudit) bool {
	leftPayload, _ := json.Marshal(struct {
		Claims    []EvidenceAuditClaim
		Summary   EvidenceAuditSummary
		Proofroom EvidenceAuditProofroomProjection
	}{left.ClaimAudits, left.Summary, left.Proofroom})
	rightPayload, _ := json.Marshal(struct {
		Claims    []EvidenceAuditClaim
		Summary   EvidenceAuditSummary
		Proofroom EvidenceAuditProofroomProjection
	}{right.ClaimAudits, right.Summary, right.Proofroom})
	return string(leftPayload) == string(rightPayload)
}

func evidenceAuditOpaqueIdentity(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sanitizeEvidenceAuditFailureCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' {
			builder.WriteRune(char)
		}
		if builder.Len() == 48 {
			break
		}
	}
	return builder.String()
}

func sanitizeEvidenceAuditFailureSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if len(summary) > 160 {
		summary = summary[:160]
	}
	for _, sensitive := range []string{"bearer", "token", "prompt=", "patient data", "remote body"} {
		if strings.Contains(strings.ToLower(summary), sensitive) {
			return "Audit failed; sensitive upstream detail was redacted."
		}
	}
	var builder strings.Builder
	for _, char := range summary {
		if char >= 0x20 && char != 0x7f {
			builder.WriteRune(char)
		}
	}
	if strings.TrimSpace(builder.String()) == "" {
		return "Audit failed without a safe diagnostic summary."
	}
	return builder.String()
}

func evidenceAuditTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Format(time.RFC3339Nano)
}

func evidenceAuditHashName(hash string) string {
	return sanitizeBookKnowledgeID(strings.TrimPrefix(strings.TrimSpace(hash), "sha256:"))
}

func writeEvidenceAuditImmutableFile(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		stored, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if string(stored) != string(payload) {
			return ErrEvidenceAuditImmutable
		}
		return nil
	}
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func (s *BookKnowledgeStore) acquireEvidenceAuditRootLock() (func(), error) {
	if err := os.MkdirAll(s.EvidenceAuditDir(), os.ModePerm); err != nil {
		return nil, err
	}
	path := filepath.Join(s.EvidenceAuditDir(), ".store.lock")
	deadline := time.Now().Add(evidenceAuditRootLockWait)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Sync()
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil &&
			time.Since(info.ModTime()) > evidenceAuditRootLockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out acquiring evidence audit root lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
