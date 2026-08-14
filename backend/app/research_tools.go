package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ResearchToolSearchKnowledge        = "search_knowledge"
	ResearchToolFetchKnowledgeEvidence = "fetch_knowledge_evidence"
	ResearchToolSearchPriorRuns        = "search_prior_runs"

	ResearchToolPolicyAllow = "allow"

	ResearchToolOutcomeCompleted = "completed"
	ResearchToolOutcomeFailed    = "failed"

	researchToolDefaultLimit = 8
	researchToolMaxLimit     = 50
)

type ResearchTool interface {
	Name() string
	Execute(context.Context, ResearchToolRequest) (ResearchToolResult, error)
}

type ResearchToolRequest struct {
	RunID          string         `json:"run_id"`
	PackageID      string         `json:"package_id"`
	PackageVersion string         `json:"package_version"`
	Arguments      map[string]any `json:"arguments"`
}

type ResearchToolPackageScope struct {
	PackageID      string            `json:"package_id"`
	PackageVersion string            `json:"package_version"`
	PackageHash    string            `json:"package_hash"`
	ReleaseHashes  map[string]string `json:"release_hashes"`
}

type ResearchPriorConclusion struct {
	RunID        string   `json:"run_id"`
	ConclusionID string   `json:"conclusion_id"`
	Text         string   `json:"text"`
	EvidenceIDs  []string `json:"evidence_ids"`
	Confidence   float64  `json:"confidence"`
}

type ResearchToolResult struct {
	ToolName         string                    `json:"tool_name"`
	Outcome          string                    `json:"outcome"`
	Package          ResearchToolPackageScope  `json:"package"`
	Knowledge        []AgentPackageEvidence    `json:"knowledge,omitempty"`
	Citations        []AgentScopedCitation     `json:"citations,omitempty"`
	PriorConclusions []ResearchPriorConclusion `json:"prior_conclusions,omitempty"`
	PromotedEvidence []ResearchEvidence        `json:"promoted_evidence,omitempty"`
}

type ResearchToolAuditRecord struct {
	AuditID             string   `json:"audit_id"`
	RunID               string   `json:"run_id"`
	ToolName            string   `json:"tool_name"`
	ArgumentFingerprint string   `json:"argument_fingerprint"`
	PackageID           string   `json:"package_id"`
	PackageVersion      string   `json:"package_version"`
	PackageHash         string   `json:"package_hash,omitempty"`
	PolicyDecision      string   `json:"policy_decision"`
	Outcome             string   `json:"outcome"`
	ResultFingerprint   string   `json:"result_fingerprint"`
	DurationMS          int64    `json:"duration_ms"`
	PromotedEvidenceIDs []string `json:"promoted_evidence_ids,omitempty"`
	CreatedAt           string   `json:"created_at"`
}

type ResearchToolRegistry struct {
	knowledge *BookKnowledgeStore
	research  *ResearchStore
	tools     map[string]ResearchTool
}

type researchToolFunc struct {
	name    string
	execute func(context.Context, ResearchToolRequest) (ResearchToolResult, error)
}

func (t researchToolFunc) Name() string { return t.name }

func (t researchToolFunc) Execute(ctx context.Context, request ResearchToolRequest) (ResearchToolResult, error) {
	return t.execute(ctx, request)
}

func NewResearchToolRegistry(knowledge *BookKnowledgeStore, research *ResearchStore) (*ResearchToolRegistry, error) {
	if knowledge == nil || research == nil || research.db == nil {
		return nil, fmt.Errorf("knowledge and research stores are required")
	}
	if err := migrateResearchToolAudits(research.db); err != nil {
		return nil, err
	}
	registry := &ResearchToolRegistry{knowledge: knowledge, research: research}
	registry.tools = map[string]ResearchTool{
		ResearchToolSearchKnowledge:        researchToolFunc{name: ResearchToolSearchKnowledge, execute: registry.searchKnowledge},
		ResearchToolFetchKnowledgeEvidence: researchToolFunc{name: ResearchToolFetchKnowledgeEvidence, execute: registry.fetchKnowledgeEvidence},
		ResearchToolSearchPriorRuns:        researchToolFunc{name: ResearchToolSearchPriorRuns, execute: registry.searchPriorRuns},
	}
	return registry, nil
}

func (r *ResearchToolRegistry) Execute(ctx context.Context, name string, request ResearchToolRequest) (ResearchToolResult, error) {
	started := time.Now().UTC()
	name = strings.TrimSpace(name)
	request.RunID = strings.TrimSpace(request.RunID)
	request.PackageID = strings.TrimSpace(request.PackageID)
	request.PackageVersion = strings.TrimSpace(request.PackageVersion)
	argumentFingerprint := researchToolFingerprint(request.Arguments)
	run, err := r.research.LoadRun(request.RunID)
	if err != nil {
		return ResearchToolResult{}, err
	}
	tool, ok := r.tools[name]
	if !ok {
		err = fmt.Errorf("unsupported research tool %q", name)
		if auditErr := r.saveAudit(started, request, ResearchToolAuditRecord{
			ToolName: name, ArgumentFingerprint: argumentFingerprint,
			PolicyDecision: "block", Outcome: ResearchToolOutcomeFailed,
			ResultFingerprint: researchToolFingerprint(map[string]any{"outcome": ResearchToolOutcomeFailed}),
		}); auditErr != nil {
			return ResearchToolResult{}, fmt.Errorf("%v; persist research tool audit: %w", err, auditErr)
		}
		return ResearchToolResult{}, err
	}
	pkg, err := loadRunnableResearchAgentPackage(ctx, r.knowledge, *run, request.PackageID, request.PackageVersion)
	if err != nil {
		if auditErr := r.saveAudit(started, request, ResearchToolAuditRecord{
			ToolName: name, ArgumentFingerprint: argumentFingerprint,
			PolicyDecision: "block", Outcome: ResearchToolOutcomeFailed,
			ResultFingerprint: researchToolFingerprint(map[string]any{"outcome": ResearchToolOutcomeFailed}),
		}); auditErr != nil {
			return ResearchToolResult{}, fmt.Errorf("%v; persist research tool audit: %w", err, auditErr)
		}
		return ResearchToolResult{}, err
	}
	result, executeErr := tool.Execute(ctx, request)
	result.ToolName = name
	result.Package = researchToolPackageScope(*pkg)
	result.Outcome = ResearchToolOutcomeCompleted
	audit := ResearchToolAuditRecord{
		ToolName: name, ArgumentFingerprint: argumentFingerprint,
		PackageHash: pkg.ContentHash, PolicyDecision: ResearchToolPolicyAllow,
		Outcome: ResearchToolOutcomeCompleted,
	}
	if executeErr != nil {
		result = ResearchToolResult{}
		audit.Outcome = ResearchToolOutcomeFailed
		audit.ResultFingerprint = researchToolFingerprint(map[string]any{"outcome": ResearchToolOutcomeFailed})
	} else {
		audit.ResultFingerprint = researchToolFingerprint(result)
		for _, evidence := range result.PromotedEvidence {
			audit.PromotedEvidenceIDs = append(audit.PromotedEvidenceIDs, evidence.EvidenceID)
		}
	}
	if auditErr := r.saveAudit(started, request, audit); auditErr != nil {
		if executeErr != nil {
			return ResearchToolResult{}, fmt.Errorf("%v; persist research tool audit: %w", executeErr, auditErr)
		}
		return ResearchToolResult{}, auditErr
	}
	return result, executeErr
}

func (r *ResearchToolRegistry) searchKnowledge(ctx context.Context, request ResearchToolRequest) (ResearchToolResult, error) {
	pkg, err := loadRunnableAgentPackageContext(ctx, r.knowledge, request.PackageID, request.PackageVersion, "search")
	if err != nil {
		return ResearchToolResult{}, err
	}
	query, err := requiredResearchToolString(request.Arguments, "query")
	if err != nil {
		return ResearchToolResult{}, err
	}
	limit, err := researchToolLimit(request.Arguments)
	if err != nil {
		return ResearchToolResult{}, err
	}
	search, err := searchAgentPackageNaturalLanguageEvidence(r.knowledge, *pkg, query, limit)
	if err != nil {
		return ResearchToolResult{}, err
	}
	return ResearchToolResult{Knowledge: search.Results}, nil
}

func (r *ResearchToolRegistry) fetchKnowledgeEvidence(ctx context.Context, request ResearchToolRequest) (ResearchToolResult, error) {
	pkg, err := loadRunnableAgentPackageContext(ctx, r.knowledge, request.PackageID, request.PackageVersion, "search")
	if err != nil {
		return ResearchToolResult{}, err
	}
	releaseID, err := requiredResearchToolString(request.Arguments, "release_id")
	if err != nil {
		return ResearchToolResult{}, err
	}
	claimID, err := requiredResearchToolString(request.Arguments, "claim_id")
	if err != nil {
		return ResearchToolResult{}, err
	}
	citationID, err := requiredResearchToolString(request.Arguments, "citation_id")
	if err != nil {
		return ResearchToolResult{}, err
	}
	ref, err := agentPackagePinnedReleaseRef(*pkg, releaseID)
	if err != nil {
		return ResearchToolResult{}, err
	}
	release, err := r.knowledge.LoadKnowledgeRelease(releaseID)
	if err != nil {
		return ResearchToolResult{}, err
	}
	if agentTraceReleaseContentHash(release.ContentHash) != agentTraceReleaseContentHash(ref.ContentHash) {
		return ResearchToolResult{}, fmt.Errorf("pinned release %q content hash changed", releaseID)
	}
	statement := ""
	claimCitations := map[string]bool{}
	for _, claim := range release.Analysis.Claims {
		if claim.ID == claimID {
			statement = strings.TrimSpace(claim.Statement)
			claimCitations = stringBoolSet(claim.CitationIDs...)
			break
		}
	}
	if statement == "" {
		return ResearchToolResult{}, fmt.Errorf("claim %q is not present in pinned release %q", claimID, releaseID)
	}
	if !claimCitations[citationID] {
		return ResearchToolResult{}, fmt.Errorf("citation %q does not support claim %q", citationID, claimID)
	}
	citation, err := resolveAgentPackageReleaseCitationContext(ctx, r.knowledge, *pkg, releaseID, claimID, citationID)
	if err != nil {
		return ResearchToolResult{}, err
	}
	bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
		SearchedSources: []string{ResearchSourceKnowledge},
		Items: []ResearchWorkerEvidenceCandidate{{
			SourceType: ResearchEvidenceSourceKnowledge, SourceRole: ResearchEvidenceRoleExternalEvidence,
			Content: statement, Locator: ResearchEvidenceLocator{ReleaseID: releaseID, MessageRef: claimID, ConversationRef: citationID},
			Privacy: ResearchEvidencePrivacyPublic, Selected: true,
		}},
	})
	if err != nil {
		return ResearchToolResult{}, err
	}
	return ResearchToolResult{
		Knowledge: []AgentPackageEvidence{{ReleaseID: releaseID, ClaimID: claimID, Statement: statement, CitationIDs: []string{citationID}}},
		Citations: []AgentScopedCitation{citation}, PromotedEvidence: bundle.Evidence,
	}, nil
}

func (r *ResearchToolRegistry) searchPriorRuns(_ context.Context, request ResearchToolRequest) (ResearchToolResult, error) {
	query, err := requiredResearchToolString(request.Arguments, "query")
	if err != nil {
		return ResearchToolResult{}, err
	}
	limit, err := researchToolLimit(request.Arguments)
	if err != nil {
		return ResearchToolResult{}, err
	}
	rows, err := r.research.db.Query(`SELECT c.run_id, c.conclusion_id, c.conclusion_text,
		c.evidence_ids_json, c.confidence FROM research_conclusions c
		JOIN research_runs run ON run.run_id = c.run_id
		WHERE run.status = ? AND run.run_id <> ? AND run.package_id = ? AND run.package_version = ?
		AND lower(c.conclusion_text) LIKE ? ESCAPE '\'
		ORDER BY c.created_at DESC, c.conclusion_id ASC LIMIT ?`, ResearchCompleted, request.RunID,
		request.PackageID, request.PackageVersion, "%"+escapeResearchToolLike(strings.ToLower(query))+"%", limit)
	if err != nil {
		return ResearchToolResult{}, err
	}
	defer rows.Close()
	result := ResearchToolResult{PriorConclusions: []ResearchPriorConclusion{}}
	allowedEvidence := map[string]bool{}
	for rows.Next() {
		var item ResearchPriorConclusion
		var evidenceJSON string
		if err := rows.Scan(&item.RunID, &item.ConclusionID, &item.Text, &evidenceJSON, &item.Confidence); err != nil {
			return ResearchToolResult{}, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceIDs); err != nil {
			return ResearchToolResult{}, err
		}
		result.PriorConclusions = append(result.PriorConclusions, item)
		for _, evidenceID := range item.EvidenceIDs {
			allowedEvidence[item.RunID+"\x00"+evidenceID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return ResearchToolResult{}, err
	}
	verifications, err := parseResearchLocatorVerifications(request.Arguments["verified_locators"])
	if err != nil {
		return ResearchToolResult{}, err
	}
	for locatorHash, contentHash := range verifications {
		var priorRunID, evidenceID, sourceRole, authorID, subjectJSON, occurredAt, excerpt, storedContentHash string
		err := r.research.db.QueryRow(`SELECT run_id, evidence_id, source_role, author_identity_id,
			subject_identity_ids_json, occurred_at, content_excerpt, content_hash
			FROM research_evidence WHERE locator_hash = ? AND content_hash = ? AND privacy = ? LIMIT 1`,
			locatorHash, contentHash, ResearchEvidencePrivacyPrivate).Scan(
			&priorRunID, &evidenceID, &sourceRole, &authorID, &subjectJSON, &occurredAt, &excerpt, &storedContentHash)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return ResearchToolResult{}, err
		}
		if !allowedEvidence[priorRunID+"\x00"+evidenceID] || storedContentHash != contentHash {
			continue
		}
		var subjectIDs []string
		if err := json.Unmarshal([]byte(subjectJSON), &subjectIDs); err != nil {
			return ResearchToolResult{}, err
		}
		bundle, err := NormalizeResearchWorkerResult(ResearchWorkerResult{
			SearchedSources: []string{ResearchSourcePriorRuns},
			Items: []ResearchWorkerEvidenceCandidate{{
				SourceType: ResearchEvidenceSourcePriorRun, SourceRole: ResearchEvidenceRoleUserHistory,
				AuthorIdentityID: authorID, SubjectIdentityIDs: subjectIDs, OccurredAt: occurredAt,
				Content: excerpt, Privacy: ResearchEvidencePrivacyPrivate, Selected: true,
				Locator: ResearchEvidenceLocator{PriorRunID: priorRunID, MessageRef: evidenceID},
			}},
		})
		if err != nil {
			return ResearchToolResult{}, err
		}
		result.PromotedEvidence = append(result.PromotedEvidence, bundle.Evidence...)
	}
	sort.Slice(result.PromotedEvidence, func(i, j int) bool {
		return result.PromotedEvidence[i].EvidenceID < result.PromotedEvidence[j].EvidenceID
	})
	return result, nil
}

func migrateResearchToolAudits(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS research_tool_audits (
		audit_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES research_runs(run_id) ON DELETE CASCADE,
		tool_name TEXT NOT NULL, argument_fingerprint TEXT NOT NULL, package_id TEXT NOT NULL,
		package_version TEXT NOT NULL, package_hash TEXT NOT NULL DEFAULT '', policy_decision TEXT NOT NULL,
		outcome TEXT NOT NULL, result_fingerprint TEXT NOT NULL, duration_ms INTEGER NOT NULL,
		promoted_evidence_ids_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL
	)`)
	return err
}

func (r *ResearchToolRegistry) saveAudit(started time.Time, request ResearchToolRequest, audit ResearchToolAuditRecord) error {
	createdAt := time.Now().UTC()
	audit.AuditID = "research-tool-audit-" + strings.TrimPrefix(researchToolFingerprint(map[string]any{
		"run_id": request.RunID, "tool": audit.ToolName, "started_at": started.Format(time.RFC3339Nano),
	}), "sha256:")[:32]
	audit.RunID = request.RunID
	audit.PackageID = request.PackageID
	audit.PackageVersion = request.PackageVersion
	audit.DurationMS = createdAt.Sub(started).Milliseconds()
	audit.CreatedAt = createdAt.Format(time.RFC3339Nano)
	promotedJSON, err := json.Marshal(audit.PromotedEvidenceIDs)
	if err != nil {
		return err
	}
	_, err = r.research.db.Exec(`INSERT INTO research_tool_audits (
		audit_id, run_id, tool_name, argument_fingerprint, package_id, package_version, package_hash,
		policy_decision, outcome, result_fingerprint, duration_ms, promoted_evidence_ids_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, audit.AuditID, audit.RunID, audit.ToolName,
		audit.ArgumentFingerprint, audit.PackageID, audit.PackageVersion, audit.PackageHash,
		audit.PolicyDecision, audit.Outcome, audit.ResultFingerprint, audit.DurationMS, string(promotedJSON), audit.CreatedAt)
	return err
}

func (s *ResearchStore) ListResearchToolAudits(runID string) ([]ResearchToolAuditRecord, error) {
	if err := migrateResearchToolAudits(s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT audit_id, run_id, tool_name, argument_fingerprint, package_id,
		package_version, package_hash, policy_decision, outcome, result_fingerprint, duration_ms,
		promoted_evidence_ids_json, created_at FROM research_tool_audits WHERE run_id = ?
		ORDER BY created_at ASC, audit_id ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResearchToolAuditRecord{}
	for rows.Next() {
		var item ResearchToolAuditRecord
		var promotedJSON string
		if err := rows.Scan(&item.AuditID, &item.RunID, &item.ToolName, &item.ArgumentFingerprint,
			&item.PackageID, &item.PackageVersion, &item.PackageHash, &item.PolicyDecision, &item.Outcome,
			&item.ResultFingerprint, &item.DurationMS, &promotedJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(promotedJSON), &item.PromotedEvidenceIDs); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func researchToolPackageScope(pkg AgentPackage) ResearchToolPackageScope {
	scope := ResearchToolPackageScope{
		PackageID: pkg.PackageID, PackageVersion: pkg.Version, PackageHash: pkg.ContentHash,
		ReleaseHashes: make(map[string]string, len(pkg.Releases)),
	}
	for _, release := range pkg.Releases {
		scope.ReleaseHashes[release.ReleaseID] = agentTraceReleaseContentHash(release.ContentHash)
	}
	return scope
}

func researchToolFingerprint(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte("null")
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func requiredResearchToolString(arguments map[string]any, name string) (string, error) {
	value, ok := arguments[name].(string)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func researchToolLimit(arguments map[string]any) (int, error) {
	value, ok := arguments["limit"]
	if !ok {
		return researchToolDefaultLimit, nil
	}
	limit := 0
	switch typed := value.(type) {
	case int:
		limit = typed
	case float64:
		if typed == float64(int(typed)) {
			limit = int(typed)
		}
	}
	if limit <= 0 || limit > researchToolMaxLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", researchToolMaxLimit)
	}
	return limit, nil
}

func escapeResearchToolLike(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "%", "\\%")
	return strings.ReplaceAll(value, "_", "\\_")
}

func parseResearchLocatorVerifications(value any) (map[string]string, error) {
	result := map[string]string{}
	if value == nil {
		return result, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) > researchToolMaxLimit {
		return nil, fmt.Errorf("verified_locators must be a bounded array")
	}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("verified locator must be an object")
		}
		locatorHash, locatorOK := item["locator_hash"].(string)
		contentHash, contentOK := item["content_hash"].(string)
		locatorHash = strings.TrimSpace(locatorHash)
		contentHash = strings.TrimSpace(contentHash)
		if !locatorOK || !contentOK || !strings.HasPrefix(locatorHash, "sha256:") || !strings.HasPrefix(contentHash, "sha256:") {
			return nil, fmt.Errorf("verified locator requires locator_hash and content_hash")
		}
		result[locatorHash] = contentHash
	}
	return result, nil
}
