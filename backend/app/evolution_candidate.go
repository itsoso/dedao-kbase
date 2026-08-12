package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	EvolutionCandidateAgentCompilation = "agent_compilation"
	EvolutionCandidateKnowledgeRelease = "knowledge_release"
	EvolutionCandidateCombined         = "combined"

	evolutionCandidateArtifactSchema   = "evolution-candidate.v1"
	evolutionCandidateArtifactMaxBytes = 2 << 20
)

var (
	ErrEvolutionCandidateNotFound         = errors.New("evolution candidate not found")
	ErrEvolutionCandidateArtifactConflict = errors.New("evolution candidate artifact conflicts with its content hash")
)

type EvolutionCandidateInput struct {
	IdempotencyKey   string `json:"idempotency_key"`
	RunID            string `json:"run_id"`
	CandidateType    string `json:"candidate_type"`
	BaselineIdentity string `json:"baseline_identity"`
	ChangeSummary    string `json:"change_summary"`
	GeneratorVersion string `json:"generator_version"`
	Artifact         any    `json:"artifact"`
}

type evolutionCandidateArtifact struct {
	SchemaVersion    string          `json:"schema_version"`
	RunID            string          `json:"run_id"`
	CandidateType    string          `json:"candidate_type"`
	BaselineIdentity string          `json:"baseline_identity"`
	ChangeSummary    string          `json:"change_summary"`
	GeneratorVersion string          `json:"generator_version"`
	Artifact         json.RawMessage `json:"artifact"`
}

func (s *EvolutionControlStore) SaveEvolutionCandidate(input EvolutionCandidateInput) (*EvolutionCandidate, bool, error) {
	normalized, payload, contentHash, err := normalizeEvolutionCandidateInput(input)
	if err != nil {
		return nil, false, err
	}
	artifactRef := "candidate:" + contentHash
	candidateID := "candidate-" + strings.TrimPrefix(contentHash, "sha256:")
	createdAtTime := s.now().UTC()
	createdAt := createdAtTime.Format(time.RFC3339Nano)
	candidate := &EvolutionCandidate{
		CandidateID: candidateID, RunID: normalized.RunID, CandidateType: normalized.CandidateType,
		ContentHash: contentHash, ArtifactRef: artifactRef, BaselineIdentity: normalized.BaselineIdentity,
		ChangeSummary: normalized.ChangeSummary, GeneratorVersion: normalized.GeneratorVersion, CreatedAt: createdAt,
	}
	if err := candidate.Validate(); err != nil {
		return nil, false, err
	}

	path, err := s.evolutionCandidateArtifactPath(contentHash)
	if err != nil {
		return nil, false, err
	}
	if err := writeImmutableEvolutionCandidateArtifact(path, payload); err != nil {
		return nil, false, err
	}

	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("begin save evolution candidate", err)
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := loadEvolutionCandidateByIdempotencyKeyTx(tx, normalized.IdempotencyKey)
	if err == nil {
		if existing.ContentHash != contentHash || existing.RunID != normalized.RunID {
			return nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, wrapEvolutionSQLiteWriteError("commit evolution candidate replay", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("find evolution candidate replay: %w", err)
	}
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, normalized.RunID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrEvolutionRunNotFound
		}
		return nil, false, fmt.Errorf("load evolution run for candidate: %w", err)
	}
	if run.Status != EvolutionGenerating {
		return nil, false, fmt.Errorf("%w: candidate requires generating run", ErrEvolutionTransitionConflict)
	}
	if _, err := tx.Exec(`
		INSERT INTO evolution_candidates (
			candidate_id, idempotency_key, run_id, candidate_type, content_hash, artifact_ref,
			baseline_identity, change_summary, generator_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, candidate.CandidateID, normalized.IdempotencyKey, candidate.RunID, candidate.CandidateType,
		candidate.ContentHash, candidate.ArtifactRef, candidate.BaselineIdentity, candidate.ChangeSummary,
		candidate.GeneratorVersion, candidate.CreatedAt); err != nil {
		return nil, false, fmt.Errorf("insert evolution candidate: %w", err)
	}
	if _, err := tx.Exec(`UPDATE evolution_runs SET current_candidate_id = ?, updated_at = ?, updated_at_unix_nano = ? WHERE run_id = ?`, candidate.CandidateID, createdAt, createdAtTime.UnixNano(), candidate.RunID); err != nil {
		return nil, false, fmt.Errorf("attach evolution candidate to run: %w", err)
	}
	eventID, err := newEvolutionStoreID("event")
	if err != nil {
		return nil, false, err
	}
	event := EvolutionEvent{
		EventID: eventID, RunID: candidate.RunID, EventType: "candidate_ready", Actor: candidate.GeneratorVersion,
		FromStatus: EvolutionGenerating, ToStatus: EvolutionGenerating, Code: "candidate_ready",
		Message: "immutable evolution candidate is ready", ArtifactRefs: []string{candidate.ArtifactRef}, CreatedAt: createdAt,
	}
	if err := s.insertEventTx(tx, event); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, wrapEvolutionSQLiteWriteError("commit evolution candidate", err)
	}
	return candidate, true, nil
}

func (s *EvolutionControlStore) LoadEvolutionCandidate(candidateID string) (*EvolutionCandidate, json.RawMessage, error) {
	if err := validateEvolutionIdentity("candidate_id", candidateID); err != nil {
		return nil, nil, err
	}
	candidate, err := loadEvolutionCandidate(s.db.QueryRow(`
		SELECT candidate_id, run_id, candidate_type, content_hash, artifact_ref,
			baseline_identity, change_summary, generator_version, created_at
		FROM evolution_candidates WHERE candidate_id = ?
	`, candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrEvolutionCandidateNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load evolution candidate: %w", err)
	}
	path, err := s.evolutionCandidateArtifactPath(candidate.ContentHash)
	if err != nil {
		return nil, nil, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read evolution candidate artifact: %w", err)
	}
	digest := sha256.Sum256(payload)
	if "sha256:"+hex.EncodeToString(digest[:]) != candidate.ContentHash {
		return nil, nil, ErrEvolutionCandidateArtifactConflict
	}
	return candidate, json.RawMessage(payload), nil
}

func normalizeEvolutionCandidateInput(input EvolutionCandidateInput) (EvolutionCandidateInput, []byte, string, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.RunID = strings.TrimSpace(input.RunID)
	input.CandidateType = strings.TrimSpace(input.CandidateType)
	input.BaselineIdentity = strings.TrimSpace(input.BaselineIdentity)
	input.ChangeSummary = strings.TrimSpace(input.ChangeSummary)
	input.GeneratorVersion = strings.TrimSpace(input.GeneratorVersion)
	if !isEvolutionOpaqueID(input.IdempotencyKey) {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("idempotency_key must be a canonical UUID or sha256 identity")
	}
	switch input.CandidateType {
	case EvolutionCandidateAgentCompilation, EvolutionCandidateKnowledgeRelease, EvolutionCandidateCombined:
	default:
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("candidate_type is invalid")
	}
	artifactPayload, err := json.Marshal(input.Artifact)
	if err != nil {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("encode evolution candidate artifact: %w", err)
	}
	if len(artifactPayload) == 0 || len(artifactPayload) > evolutionCandidateArtifactMaxBytes || bytes.Equal(artifactPayload, []byte("null")) {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("artifact must contain at most %d bytes", evolutionCandidateArtifactMaxBytes)
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(artifactPayload))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("decode evolution candidate artifact: %w", err)
	}
	canonicalPayload, err := json.Marshal(canonical)
	if err != nil {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("canonicalize evolution candidate artifact: %w", err)
	}
	envelope := evolutionCandidateArtifact{
		SchemaVersion: evolutionCandidateArtifactSchema, RunID: input.RunID, CandidateType: input.CandidateType,
		BaselineIdentity: input.BaselineIdentity, ChangeSummary: input.ChangeSummary,
		GeneratorVersion: input.GeneratorVersion, Artifact: canonicalPayload,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return EvolutionCandidateInput{}, nil, "", fmt.Errorf("encode evolution candidate envelope: %w", err)
	}
	digest := sha256.Sum256(payload)
	contentHash := "sha256:" + hex.EncodeToString(digest[:])
	probe := EvolutionCandidate{
		CandidateID: "candidate-" + hex.EncodeToString(digest[:]), RunID: input.RunID,
		CandidateType: input.CandidateType, ContentHash: contentHash,
		ArtifactRef: "candidate:" + contentHash, BaselineIdentity: input.BaselineIdentity,
		ChangeSummary: input.ChangeSummary, GeneratorVersion: input.GeneratorVersion,
		CreatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339Nano),
	}
	if err := probe.Validate(); err != nil {
		return EvolutionCandidateInput{}, nil, "", err
	}
	return input, payload, contentHash, nil
}

func (s *EvolutionControlStore) evolutionCandidateArtifactPath(contentHash string) (string, error) {
	hexDigest := strings.TrimPrefix(contentHash, "sha256:")
	if len(hexDigest) != 64 || "sha256:"+hexDigest != contentHash {
		return "", fmt.Errorf("candidate content_hash is invalid")
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return "", fmt.Errorf("candidate content_hash is invalid")
	}
	return filepath.Join(filepath.Dir(s.dbPath), "evolution_artifacts", "sha256", hexDigest[:2], hexDigest+".json"), nil
}

func writeImmutableEvolutionCandidateArtifact(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create evolution candidate artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read existing evolution candidate artifact: %w", readErr)
		}
		if !bytes.Equal(existing, payload) {
			return ErrEvolutionCandidateArtifactConflict
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create evolution candidate artifact: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write evolution candidate artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync evolution candidate artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close evolution candidate artifact: %w", err)
	}
	remove = false
	return nil
}

type evolutionCandidateScanner interface{ Scan(...any) error }

func loadEvolutionCandidate(scanner evolutionCandidateScanner) (*EvolutionCandidate, error) {
	var candidate EvolutionCandidate
	if err := scanner.Scan(&candidate.CandidateID, &candidate.RunID, &candidate.CandidateType,
		&candidate.ContentHash, &candidate.ArtifactRef, &candidate.BaselineIdentity,
		&candidate.ChangeSummary, &candidate.GeneratorVersion, &candidate.CreatedAt); err != nil {
		return nil, err
	}
	if err := candidate.Validate(); err != nil {
		return nil, fmt.Errorf("stored evolution candidate is invalid: %w", err)
	}
	return &candidate, nil
}

func loadEvolutionCandidateByIdempotencyKeyTx(tx *sql.Tx, key string) (*EvolutionCandidate, error) {
	return loadEvolutionCandidate(tx.QueryRow(`
		SELECT candidate_id, run_id, candidate_type, content_hash, artifact_ref,
			baseline_identity, change_summary, generator_version, created_at
		FROM evolution_candidates WHERE idempotency_key = ?
	`, key))
}
