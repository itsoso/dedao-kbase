package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const evolutionGenerationMaxAttempts = 3

type EvolutionTriageInput struct {
	RunID                  string
	BaselinePackageVersion string
	BaselineReleaseIDs     []string
	Actor                  string
}

type EvolutionTriageResult struct {
	Run  *EvolutionRun  `json:"run"`
	Work *EvolutionWork `json:"work"`
}

// TriageEvolutionRun resolves the mutable knowledge-store heads before asking
// the control store to freeze them and enqueue the first generation work item.
func TriageEvolutionRun(ctx context.Context, control *EvolutionControlStore, knowledge *BookKnowledgeStore, runID string) (*EvolutionTriageResult, error) {
	if ctx == nil || control == nil || knowledge == nil {
		return nil, fmt.Errorf("evolution triage requires context, control, and knowledge stores")
	}
	run, err := control.LoadRunContext(ctx, runID)
	if err != nil {
		return nil, err
	}
	// A generating run has already frozen its baseline. Replay against that
	// immutable identity before consulting the mutable published package head.
	if run.Status == EvolutionGenerating {
		preparedRun, work, _, err := control.TriageEvolutionRun(EvolutionTriageInput{
			RunID: run.RunID, BaselinePackageVersion: run.BaselinePackageVersion,
			BaselineReleaseIDs: run.BaselineReleaseIDs, Actor: "human-operator",
		})
		if err != nil {
			return nil, err
		}
		return &EvolutionTriageResult{Run: preparedRun, Work: work}, nil
	}
	packageVersion := ""
	releaseIDs := append([]string(nil), run.BaselineReleaseIDs...)
	if run.RunType == EvolutionRunAgentPolicy || run.RunType == EvolutionRunCombined {
		pkg, err := loadPublishedEvolutionBaseline(ctx, knowledge, run.PackageID)
		if err != nil {
			return nil, err
		}
		packageVersion = pkg.Version
		for _, release := range pkg.Releases {
			releaseIDs = append(releaseIDs, release.ReleaseID)
		}
	}
	// Preserve signal-scoped releases first: combined generation uses the first
	// release as the knowledge candidate target, then adds package dependencies.
	releaseIDs = uniqueTrimmedStrings(releaseIDs)
	if run.RunType != EvolutionRunAgentPolicy && len(releaseIDs) == 0 {
		return nil, fmt.Errorf("evolution triage requires a knowledge release baseline")
	}
	for _, releaseID := range releaseIDs {
		if _, err := knowledge.LoadKnowledgeRelease(releaseID); err != nil {
			return nil, fmt.Errorf("load evolution baseline release: %w", err)
		}
	}
	preparedRun, work, _, err := control.TriageEvolutionRun(EvolutionTriageInput{
		RunID: run.RunID, BaselinePackageVersion: packageVersion,
		BaselineReleaseIDs: releaseIDs, Actor: "human-operator",
	})
	if err != nil {
		return nil, err
	}
	return &EvolutionTriageResult{Run: preparedRun, Work: work}, nil
}

func loadPublishedEvolutionBaseline(ctx context.Context, store *BookKnowledgeStore, packageID string) (*AgentPackage, error) {
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		records, err := store.ListAgentPackages(cursor, 200)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if record.PackageID == packageID && record.LifecycleState == AgentPackagePublished {
				return store.LoadAgentPackageContext(ctx, record.PackageID, record.Version)
			}
		}
		if len(records) < 200 {
			return nil, fmt.Errorf("published Agent baseline %q was not found", packageID)
		}
		last := records[len(records)-1]
		next := agentPackageReference(last.PackageID, last.Version)
		if next == cursor {
			return nil, fmt.Errorf("Agent package pagination did not advance")
		}
		cursor = next
	}
}

// TriageEvolutionRun atomically freezes baselines, records both lifecycle
// transitions, and creates the first generation work item.
func (s *EvolutionControlStore) TriageEvolutionRun(input EvolutionTriageInput) (*EvolutionRun, *EvolutionWork, bool, error) {
	input.RunID = strings.TrimSpace(input.RunID)
	input.BaselinePackageVersion = strings.TrimSpace(input.BaselinePackageVersion)
	input.BaselineReleaseIDs = uniqueTrimmedStrings(input.BaselineReleaseIDs)
	input.Actor = strings.TrimSpace(input.Actor)
	if err := validateEvolutionIdentityFields(
		evolutionStringField{name: "run_id", value: input.RunID},
		evolutionStringField{name: "actor", value: input.Actor},
	); err != nil {
		return nil, nil, false, err
	}
	now := s.now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	timestampNS, err := evolutionWorkerUnixNano(now)
	if err != nil {
		return nil, nil, false, err
	}
	tx, err := s.beginTx(context.Background())
	if err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("begin evolution triage", err)
	}
	defer func() { _ = tx.Rollback() }()
	run, err := scanEvolutionRun(tx.QueryRow(evolutionRunSelect+` WHERE run_id = ?`, input.RunID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, ErrEvolutionRunNotFound
	}
	if err != nil {
		return nil, nil, false, err
	}
	prepared := *run
	prepared.BaselinePackageVersion = input.BaselinePackageVersion
	prepared.BaselineReleaseIDs = append([]string(nil), input.BaselineReleaseIDs...)
	prepared.Status = EvolutionGenerating
	prepared.UpdatedAt = timestamp
	if err := prepared.Validate(); err != nil {
		return nil, nil, false, err
	}
	baselineIdentity := evolutionBaselineIdentity(prepared)
	capability := EvolutionCapabilityAgent
	if prepared.RunType == EvolutionRunKnowledgeRelease {
		capability = EvolutionCapabilityKnowledge
	}
	workInput := EvolutionWorkInput{
		IdempotencyKey: "sha256:" + evolutionWorkerPayloadHash("triage:"+prepared.RunID+":"+baselineIdentity),
		RunID:          prepared.RunID, Capability: capability, ArtifactRef: "artifact:" + baselineIdentity,
		AvailableAt: now, MaxAttempts: evolutionGenerationMaxAttempts,
	}
	normalizedWork, inputHash, err := normalizeEvolutionWorkInput(workInput, now)
	if err != nil {
		return nil, nil, false, err
	}
	if run.Status == EvolutionGenerating {
		existing, err := loadEvolutionWorkByIdempotencyKeyTx(tx, normalizedWork.IdempotencyKey)
		if err != nil {
			return nil, nil, false, err
		}
		if existing.inputHash != inputHash || run.BaselinePackageVersion != prepared.BaselinePackageVersion ||
			strings.Join(run.BaselineReleaseIDs, "\x00") != strings.Join(prepared.BaselineReleaseIDs, "\x00") {
			return nil, nil, false, ErrEvolutionIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		return run, existing, false, nil
	}
	if run.Status != EvolutionDetected {
		return nil, nil, false, ErrEvolutionTransitionConflict
	}
	workID, err := newEvolutionStoreID("work")
	if err != nil {
		return nil, nil, false, err
	}
	work := &EvolutionWork{
		WorkID: workID, RunID: prepared.RunID, Capability: capability, ArtifactRef: normalizedWork.ArtifactRef,
		Status: EvolutionWorkPending, MaxAttempts: normalizedWork.MaxAttempts,
		AvailableAt: timestamp, CreatedAt: timestamp, UpdatedAt: timestamp,
		inputHash: inputHash, availableAtUnixNano: timestampNS,
	}
	releasesJSON, err := jsonMarshalEvolutionReferences(prepared.BaselineReleaseIDs)
	if err != nil {
		return nil, nil, false, err
	}
	updateResult, err := tx.Exec(`UPDATE evolution_runs SET baseline_package_version = ?, baseline_release_ids_json = ?,
		status = ?, updated_at = ?, updated_at_unix_nano = ? WHERE run_id = ? AND status = ?`,
		prepared.BaselinePackageVersion, releasesJSON, EvolutionGenerating, timestamp, timestampNS,
		prepared.RunID, EvolutionDetected)
	if err != nil {
		return nil, nil, false, err
	}
	if affected, err := updateResult.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, ErrEvolutionTransitionConflict
	}
	if err := insertEvolutionRunScopesTx(tx, &prepared); err != nil {
		return nil, nil, false, err
	}
	for _, transition := range []struct {
		from, to EvolutionRunStatus
		code     string
	}{
		{EvolutionDetected, EvolutionTriaged, "baseline_frozen"},
		{EvolutionTriaged, EvolutionGenerating, "generation_queued"},
	} {
		eventID, err := newEvolutionStoreID("event")
		if err != nil {
			return nil, nil, false, err
		}
		event := EvolutionEvent{
			EventID: eventID, RunID: prepared.RunID, EventType: "transition", Actor: input.Actor,
			FromStatus: transition.from, ToStatus: transition.to, Code: transition.code,
			Message:      "human triage froze the baseline and queued candidate generation",
			ArtifactRefs: []string{"artifact:" + baselineIdentity}, CreatedAt: timestamp,
		}
		if err := s.insertEventTx(tx, event); err != nil {
			return nil, nil, false, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO evolution_work_items (
		work_id, idempotency_key, input_hash, run_id, capability, artifact_ref, status,
		attempt, max_attempts, available_at, available_at_unix_nano, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		work.WorkID, normalizedWork.IdempotencyKey, inputHash, work.RunID, work.Capability, work.ArtifactRef,
		work.Status, work.MaxAttempts, work.AvailableAt, timestampNS, timestamp, timestamp); err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, wrapEvolutionSQLiteWriteError("commit evolution triage", err)
	}
	return &prepared, work, true, nil
}

func jsonMarshalEvolutionReferences(values []string) (string, error) {
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
