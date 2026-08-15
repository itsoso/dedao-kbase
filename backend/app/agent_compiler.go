package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	AgentCompilationRequestSchemaVersion = "agent-compilation-request.v1"
	AgentCompilationSchemaVersion        = "agent-compilation.v1"
	AgentCompilerVersion                 = "deterministic-agent-compiler.v1"

	AgentCompilationModeDual     = "dual"
	AgentCompilationModeEvidence = "evidence"
	AgentCompilationModeStudy    = "study"

	AgentCompilationStatusReady   = "ready"
	AgentCompilationStatusPartial = "partial"
	AgentCompilationStatusBlocked = "blocked"

	AgentCompilationCandidateStudy    = "study"
	AgentCompilationCandidateEvidence = "evidence"

	AgentCompilationCandidateReady   = "ready"
	AgentCompilationCandidateBlocked = "blocked"

	AgentCompilationIssueSupportingReleaseRequired = "supporting_release_required"
	AgentCompilationIssueReleaseNotInAssembly      = "release_not_in_assembly"
	AgentCompilationIssueReleaseInvalid            = "release_invalid"
	AgentCompilationIssueReleaseNotIndependent     = "release_not_independent"
	AgentCompilationIssueReleaseNotRelated         = "release_not_related"
	AgentCompilationIssueMissingCitations          = "missing_citations"

	AgentCompilationNextActionEvaluate            = "run_trusted_evaluation"
	AgentCompilationNextActionSelectSupport       = "select_supporting_release"
	AgentCompilationNextActionRepairEvidence      = "repair_release_evidence"
	AgentCompilationNextActionSelectLatestRelease = "select_latest_release"

	agentCompilationMaxSupportingReleases = 16
	agentCompilationMaxCandidates         = 2
	agentCompilationMaxIssuesPerCandidate = 8
	agentCompilationMaxNextActions        = 4
	agentCompilationMaxReleaseIDRunes     = 128
	agentCompilationMaxIssueMessageRunes  = 256
	agentCompilationMaxNextActionRunes    = 128
	agentCompilationMaxDiscoveryReleases  = 500
)

var agentCompilationVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

type AgentCompilationRequest struct {
	SchemaVersion        string   `json:"schema_version"`
	Mode                 string   `json:"mode"`
	PrimaryReleaseID     string   `json:"primary_release_id"`
	SupportingReleaseIDs []string `json:"supporting_release_ids,omitempty"`
	Version              string   `json:"version"`
	ResearchEnabled      bool     `json:"research_enabled,omitempty"`
}

type AgentCompilation struct {
	SchemaVersion   string                      `json:"schema_version"`
	CompilerVersion string                      `json:"compiler_version"`
	CompilationID   string                      `json:"compilation_id"`
	Mode            string                      `json:"mode"`
	AssemblyID      string                      `json:"assembly_id"`
	ReleaseIDs      []string                    `json:"release_ids"`
	Status          string                      `json:"status"`
	Candidates      []AgentCompilationCandidate `json:"candidates"`
}

type AgentCompilationCandidate struct {
	Kind        string                  `json:"kind"`
	Status      string                  `json:"status"`
	Package     *AgentPackage           `json:"package,omitempty"`
	Issues      []AgentCompilationIssue `json:"issues,omitempty"`
	NextActions []string                `json:"next_actions,omitempty"`
}

type AgentCompilationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateAgentCompilationRequest(request AgentCompilationRequest) error {
	if request.SchemaVersion != AgentCompilationRequestSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentCompilationRequestSchemaVersion)
	}
	switch request.Mode {
	case AgentCompilationModeDual, AgentCompilationModeEvidence, AgentCompilationModeStudy:
	default:
		return fmt.Errorf("mode must be dual, evidence, or study")
	}
	if strings.TrimSpace(request.PrimaryReleaseID) == "" {
		return fmt.Errorf("primary_release_id is required")
	}
	if request.PrimaryReleaseID != strings.TrimSpace(request.PrimaryReleaseID) {
		return fmt.Errorf("primary_release_id must use canonical form without surrounding whitespace")
	}
	if utf8.RuneCountInString(request.PrimaryReleaseID) > agentCompilationMaxReleaseIDRunes {
		return fmt.Errorf(
			"primary_release_id must not exceed %d characters",
			agentCompilationMaxReleaseIDRunes,
		)
	}
	if !agentCompilationVersionPattern.MatchString(request.Version) {
		return fmt.Errorf("version must be a semantic version")
	}
	if len(request.SupportingReleaseIDs) > agentCompilationMaxSupportingReleases {
		return fmt.Errorf(
			"supporting_release_ids must not exceed %d items",
			agentCompilationMaxSupportingReleases,
		)
	}
	seen := make(map[string]struct{}, len(request.SupportingReleaseIDs))
	for index, releaseID := range request.SupportingReleaseIDs {
		canonical := strings.TrimSpace(releaseID)
		if canonical == "" {
			return fmt.Errorf("supporting_release_ids[%d] is required", index)
		}
		if canonical != releaseID {
			return fmt.Errorf(
				"supporting_release_ids[%d] must use canonical form without surrounding whitespace",
				index,
			)
		}
		if utf8.RuneCountInString(releaseID) > agentCompilationMaxReleaseIDRunes {
			return fmt.Errorf(
				"supporting_release_ids[%d] must not exceed %d characters",
				index,
				agentCompilationMaxReleaseIDRunes,
			)
		}
		if releaseID == request.PrimaryReleaseID {
			return fmt.Errorf("primary release must not be repeated as a supporting release")
		}
		if _, duplicate := seen[releaseID]; duplicate {
			return fmt.Errorf("duplicate supporting release %q", boundedEvidenceID(releaseID))
		}
		seen[releaseID] = struct{}{}
	}
	return nil
}

func ValidateAgentCompilation(compilation AgentCompilation) error {
	if compilation.SchemaVersion != AgentCompilationSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AgentCompilationSchemaVersion)
	}
	if compilation.CompilerVersion != AgentCompilerVersion {
		return fmt.Errorf("compiler_version must be %q", AgentCompilerVersion)
	}
	if err := requireContractFields(map[string]string{
		"compilation_id": compilation.CompilationID,
		"assembly_id":    compilation.AssemblyID,
	}); err != nil {
		return err
	}
	switch compilation.Mode {
	case AgentCompilationModeDual, AgentCompilationModeEvidence, AgentCompilationModeStudy:
	default:
		return fmt.Errorf("mode must be dual, evidence, or study")
	}
	if len(compilation.ReleaseIDs) == 0 {
		return fmt.Errorf("release_ids is required")
	}
	if len(compilation.ReleaseIDs) > agentCompilationMaxSupportingReleases+1 {
		return fmt.Errorf(
			"release_ids must not exceed %d items",
			agentCompilationMaxSupportingReleases+1,
		)
	}
	if len(compilation.Candidates) == 0 || len(compilation.Candidates) > agentCompilationMaxCandidates {
		return fmt.Errorf("candidates must contain between 1 and %d items", agentCompilationMaxCandidates)
	}

	seenReleases := make(map[string]struct{}, len(compilation.ReleaseIDs))
	for index, releaseID := range compilation.ReleaseIDs {
		canonical := strings.TrimSpace(releaseID)
		if canonical == "" {
			return fmt.Errorf("release_ids[%d] is required", index)
		}
		if canonical != releaseID ||
			utf8.RuneCountInString(releaseID) > agentCompilationMaxReleaseIDRunes {
			return fmt.Errorf(
				"release_ids[%d] must use canonical form and not exceed %d characters",
				index,
				agentCompilationMaxReleaseIDRunes,
			)
		}
		if _, duplicate := seenReleases[releaseID]; duplicate {
			return fmt.Errorf("release_ids contains duplicate %q", boundedEvidenceID(releaseID))
		}
		seenReleases[releaseID] = struct{}{}
	}

	readyCount := 0
	blockedCount := 0
	seenKinds := make(map[string]struct{}, len(compilation.Candidates))
	for index, candidate := range compilation.Candidates {
		switch candidate.Kind {
		case AgentCompilationCandidateStudy, AgentCompilationCandidateEvidence:
		default:
			return fmt.Errorf("candidates[%d].kind is invalid", index)
		}
		if _, duplicate := seenKinds[candidate.Kind]; duplicate {
			return fmt.Errorf("candidates contains duplicate kind %q", candidate.Kind)
		}
		seenKinds[candidate.Kind] = struct{}{}
		if len(candidate.Issues) > agentCompilationMaxIssuesPerCandidate {
			return fmt.Errorf(
				"candidates[%d].issues must not exceed %d items",
				index,
				agentCompilationMaxIssuesPerCandidate,
			)
		}
		if len(candidate.NextActions) > agentCompilationMaxNextActions {
			return fmt.Errorf(
				"candidates[%d].next_actions must not exceed %d items",
				index,
				agentCompilationMaxNextActions,
			)
		}
		seenActions := make(map[string]struct{}, len(candidate.NextActions))
		for actionIndex, action := range candidate.NextActions {
			if strings.TrimSpace(action) == "" ||
				utf8.RuneCountInString(action) > agentCompilationMaxNextActionRunes {
				return fmt.Errorf(
					"candidates[%d].next_actions[%d] must contain between 1 and %d characters",
					index,
					actionIndex,
					agentCompilationMaxNextActionRunes,
				)
			}
			if _, duplicate := seenActions[action]; duplicate {
				return fmt.Errorf("candidates[%d].next_actions contains duplicates", index)
			}
			seenActions[action] = struct{}{}
		}
		for issueIndex, issue := range candidate.Issues {
			if strings.TrimSpace(issue.Code) == "" {
				return fmt.Errorf("candidates[%d].issues[%d].code is required", index, issueIndex)
			}
			if strings.TrimSpace(issue.Message) == "" {
				return fmt.Errorf("candidates[%d].issues[%d].message is required", index, issueIndex)
			}
			if utf8.RuneCountInString(issue.Message) > agentCompilationMaxIssueMessageRunes {
				return fmt.Errorf(
					"candidates[%d].issues[%d].message exceeds %d characters",
					index,
					issueIndex,
					agentCompilationMaxIssueMessageRunes,
				)
			}
		}
		switch candidate.Status {
		case AgentCompilationCandidateReady:
			readyCount++
			if candidate.Package == nil {
				return fmt.Errorf("ready candidates[%d] requires a package", index)
			}
			expectedSchemaVersion := AgentPackageSchemaVersionV1
			if candidate.Kind == AgentCompilationCandidateEvidence {
				expectedSchemaVersion = AgentPackageSchemaVersionV2
			}
			if candidate.Package.SchemaVersion != expectedSchemaVersion && candidate.Package.SchemaVersion != AgentPackageSchemaVersionV4 {
				return fmt.Errorf(
					"ready candidates[%d] kind %q requires package schema_version %q",
					index,
					candidate.Kind,
					expectedSchemaVersion,
				)
			}
			if candidate.Package.SchemaVersion == AgentPackageSchemaVersionV4 && candidate.Package.ResearchPolicy == nil {
				return fmt.Errorf("ready candidates[%d] v4 package requires research_policy", index)
			}
			if len(candidate.Issues) != 0 {
				return fmt.Errorf("ready candidates[%d] must not contain issues", index)
			}
		case AgentCompilationCandidateBlocked:
			blockedCount++
			if candidate.Package != nil {
				return fmt.Errorf("blocked candidates[%d] must not contain a package", index)
			}
			if len(candidate.Issues) == 0 {
				return fmt.Errorf("blocked candidates[%d].issues is required", index)
			}
		default:
			return fmt.Errorf("candidates[%d].status is invalid", index)
		}
	}

	expectedKinds := map[string][]string{
		AgentCompilationModeDual: {
			AgentCompilationCandidateStudy,
			AgentCompilationCandidateEvidence,
		},
		AgentCompilationModeEvidence: {AgentCompilationCandidateEvidence},
		AgentCompilationModeStudy:    {AgentCompilationCandidateStudy},
	}[compilation.Mode]
	if len(compilation.Candidates) != len(expectedKinds) {
		return fmt.Errorf(
			"mode %q requires %d candidates",
			compilation.Mode,
			len(expectedKinds),
		)
	}
	for index, expectedKind := range expectedKinds {
		if compilation.Candidates[index].Kind != expectedKind {
			return fmt.Errorf(
				"mode %q requires candidate %d to use kind %q",
				compilation.Mode,
				index,
				expectedKind,
			)
		}
	}

	expectedStatus := AgentCompilationStatusPartial
	switch {
	case readyCount == len(compilation.Candidates):
		expectedStatus = AgentCompilationStatusReady
	case blockedCount == len(compilation.Candidates):
		expectedStatus = AgentCompilationStatusBlocked
	}
	if compilation.Status != expectedStatus {
		return fmt.Errorf(
			"status %q does not agree with candidate status; expected %q",
			compilation.Status,
			expectedStatus,
		)
	}
	return nil
}

func CompileAgentPackages(
	store *BookKnowledgeStore,
	request AgentCompilationRequest,
) (*AgentCompilation, error) {
	if err := ValidateAgentCompilationRequest(request); err != nil {
		return nil, err
	}
	if store == nil {
		store = DefaultBookKnowledgeStore()
	}
	manifest, err := store.loadKnowledgeReleaseManifest()
	if err != nil {
		return nil, fmt.Errorf("load release manifest: %w", err)
	}
	latestRecords, err := latestKnowledgeAssemblyReleaseRecords(manifest.Releases)
	if err != nil {
		return nil, fmt.Errorf("resolve latest releases: %w", err)
	}
	normalizedRequest := request
	normalizedRequest.SupportingReleaseIDs = sortedUniqueStrings(request.SupportingReleaseIDs)
	if len(normalizedRequest.SupportingReleaseIDs) == 0 &&
		(normalizedRequest.Mode == AgentCompilationModeDual ||
			normalizedRequest.Mode == AgentCompilationModeEvidence) {
		normalizedRequest.SupportingReleaseIDs = discoverAgentCompilationSupport(
			store,
			latestRecords,
			normalizedRequest.PrimaryReleaseID,
		)
	}
	selectedReleaseIDs := append(
		[]string{normalizedRequest.PrimaryReleaseID},
		normalizedRequest.SupportingReleaseIDs...,
	)
	selectedReleaseIDs = sortedUniqueStrings(selectedReleaseIDs)

	latestReleaseIDs := make(map[string]struct{}, len(latestRecords))
	for _, record := range latestRecords {
		latestReleaseIDs[record.ReleaseID] = struct{}{}
	}
	scopedReleaseIDs := make([]string, 0, len(selectedReleaseIDs))
	for _, releaseID := range selectedReleaseIDs {
		if _, latest := latestReleaseIDs[releaseID]; latest {
			scopedReleaseIDs = append(scopedReleaseIDs, releaseID)
		}
	}
	var assembly *KnowledgeReleaseAssembly
	if len(scopedReleaseIDs) == 0 {
		assembly = agentCompilationEmptyAssembly()
	} else {
		assembly, err = BuildKnowledgeReleaseAssembly(
			store,
			KnowledgeReleaseAssemblyQuery{
				Limit:      knowledgeAssemblyMaxLimit,
				ReleaseIDs: scopedReleaseIDs,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("build scoped release assembly: %w", err)
		}
	}
	assemblyReleaseIDs := stringSet(assembly.ReleaseIDs)
	releases := make(map[string]*KnowledgeRelease, len(selectedReleaseIDs))
	for _, releaseID := range selectedReleaseIDs {
		if !assemblyReleaseIDs[releaseID] {
			continue
		}
		release, loadErr := store.LoadKnowledgeRelease(releaseID)
		if loadErr != nil {
			return nil, fmt.Errorf("load selected release %q: %w", boundedEvidenceID(releaseID), loadErr)
		}
		adaptKnowledgeAssemblyReleaseForRead(release)
		releases[releaseID] = release
	}

	candidates := make([]AgentCompilationCandidate, 0, agentCompilationMaxCandidates)
	switch normalizedRequest.Mode {
	case AgentCompilationModeDual:
		candidates = append(
			candidates,
			compileStudyAgentCandidate(store, normalizedRequest, assembly, releases),
			compileEvidenceAgentCandidate(store, normalizedRequest, assembly, releases),
		)
	case AgentCompilationModeEvidence:
		candidates = append(
			candidates,
			compileEvidenceAgentCandidate(store, normalizedRequest, assembly, releases),
		)
	case AgentCompilationModeStudy:
		candidates = append(
			candidates,
			compileStudyAgentCandidate(store, normalizedRequest, assembly, releases),
		)
	}
	for _, candidate := range candidates {
		if candidate.Package == nil {
			continue
		}
		for _, ref := range candidate.Package.Releases {
			selectedReleaseIDs = append(selectedReleaseIDs, ref.ReleaseID)
		}
	}
	selectedReleaseIDs = sortedUniqueStrings(selectedReleaseIDs)

	compilation := AgentCompilation{
		SchemaVersion:   AgentCompilationSchemaVersion,
		CompilerVersion: AgentCompilerVersion,
		Mode:            normalizedRequest.Mode,
		AssemblyID:      assembly.AssemblyID,
		ReleaseIDs:      selectedReleaseIDs,
		Status:          deriveAgentCompilationStatus(candidates),
		Candidates:      candidates,
	}
	compilationID, err := agentCompilationID(normalizedRequest, compilation)
	if err != nil {
		return nil, err
	}
	compilation.CompilationID = compilationID
	if err := ValidateAgentCompilation(compilation); err != nil {
		return nil, err
	}
	return &compilation, nil
}

func compileStudyAgentCandidate(
	store *BookKnowledgeStore,
	request AgentCompilationRequest,
	assembly *KnowledgeReleaseAssembly,
	releases map[string]*KnowledgeRelease,
) AgentCompilationCandidate {
	release, issue := requireAgentCompilationRelease(
		request.PrimaryReleaseID,
		assembly,
		releases,
	)
	if issue != nil {
		return blockedAgentCompilationCandidate(AgentCompilationCandidateStudy, *issue)
	}
	citationIDs := agentCompilationReleaseCitationIDs(*release)
	if len(citationIDs) == 0 {
		return blockedAgentCompilationCandidate(
			AgentCompilationCandidateStudy,
			AgentCompilationIssue{
				Code:    AgentCompilationIssueMissingCitations,
				Message: "The primary release has no claim citations that can be pinned.",
			},
		)
	}
	pkg := AgentPackage{
		SchemaVersion:  AgentPackageSchemaVersionV1,
		PackageID:      opaqueAgentCompilationPackageID(release.BookID, AgentCompilationCandidateStudy),
		Version:        request.Version,
		LifecycleState: AgentPackageDraft,
		Releases: []AgentPackageReleaseRef{{
			ReleaseID:   release.ReleaseID,
			ContentHash: release.ContentHash,
			CitationIDs: citationIDs,
		}},
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy:           "lexical",
			AllowedSourceTypes: []string{release.Book.SourceType},
			RequireCitations:   true,
			MaxContextChunks:   8,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: "reasoning",
			Fallbacks:           []string{"qwen3.7-max"},
			MaxCostUSD:          0.25,
			TimeoutMS:           30000,
		},
		PromptProfiles: []AgentPackagePromptProfile{{
			ProfileID:    "grounded-answer.v1",
			OutputSchema: "grounded-answer.v1",
		}},
		ToolPolicy: allReadOnlyAgentCompilationTools(),
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy:       BookUsageStandard,
			AbstentionReasons: []string{"insufficient_evidence", "outside_scope"},
			EscalationTarget:  "human_review",
		},
		EvaluationPolicy: agentCompilationEvaluationPolicy(false),
		UIManifest: AgentPackageUIManifest{
			Capabilities: []string{"reader", "search", "grounded_chat", "evidence", "quiz"},
		},
	}
	applyAgentCompilationResearchPolicy(&pkg, request.ResearchEnabled)
	return finalizeAgentCompilationCandidate(store, AgentCompilationCandidateStudy, pkg)
}

func compileEvidenceAgentCandidate(
	store *BookKnowledgeStore,
	request AgentCompilationRequest,
	assembly *KnowledgeReleaseAssembly,
	releases map[string]*KnowledgeRelease,
) AgentCompilationCandidate {
	primary, issue := requireAgentCompilationRelease(
		request.PrimaryReleaseID,
		assembly,
		releases,
	)
	if issue != nil {
		return blockedAgentCompilationCandidate(AgentCompilationCandidateEvidence, *issue)
	}
	publications := agentCompilationReleasePublications(releases)
	primaryPublication, ok := publications[primary.ReleaseID]
	if !ok {
		return blockedAgentCompilationCandidate(
			AgentCompilationCandidateEvidence,
			AgentCompilationIssue{
				Code:    AgentCompilationIssueReleaseInvalid,
				Message: "The primary release has no Assembly publication identity.",
			},
		)
	}
	supportingReleaseIDs := append([]string(nil), request.SupportingReleaseIDs...)
	if len(supportingReleaseIDs) == 0 {
		return blockedAgentCompilationCandidate(
			AgentCompilationCandidateEvidence,
			AgentCompilationIssue{
				Code:    AgentCompilationIssueSupportingReleaseRequired,
				Message: "An independently eligible supporting release is required.",
			},
		)
	}

	selected := make([]*KnowledgeRelease, 0, len(supportingReleaseIDs)+1)
	selected = append(selected, primary)
	for _, releaseID := range sortedUniqueStrings(supportingReleaseIDs) {
		if !stringSet(assembly.ReleaseIDs)[releaseID] {
			return blockedAgentCompilationCandidate(
				AgentCompilationCandidateEvidence,
				AgentCompilationIssue{
					Code:    AgentCompilationIssueReleaseNotInAssembly,
					Message: "The selected supporting release is not part of the latest Release Assembly.",
				},
			)
		}
		publication, exists := publications[releaseID]
		if !exists || !publication.IndependentSourceEligible ||
			publication.Key == primaryPublication.Key {
			return blockedAgentCompilationCandidate(
				AgentCompilationCandidateEvidence,
				AgentCompilationIssue{
					Code: AgentCompilationIssueReleaseNotIndependent,
					Message: "The supporting release requires a distinct, " +
						"independently eligible Assembly publication identity.",
				},
			)
		}
		if releases[releaseID] == nil && stringSet(assembly.ReleaseIDs)[releaseID] {
			release, loadErr := store.LoadKnowledgeRelease(releaseID)
			if loadErr != nil {
				return blockedAgentCompilationCandidate(
					AgentCompilationCandidateEvidence,
					AgentCompilationIssue{
						Code:    AgentCompilationIssueReleaseInvalid,
						Message: "The supporting release could not be loaded from the Assembly snapshot.",
					},
				)
			}
			adaptKnowledgeAssemblyReleaseForRead(release)
			releases[releaseID] = release
		}
		release, supportIssue := requireAgentCompilationRelease(releaseID, assembly, releases)
		if supportIssue != nil {
			return blockedAgentCompilationCandidate(AgentCompilationCandidateEvidence, *supportIssue)
		}
		if !agentCompilationReleasesRelated(*primary, *release) {
			return blockedAgentCompilationCandidate(
				AgentCompilationCandidateEvidence,
				AgentCompilationIssue{
					Code: AgentCompilationIssueReleaseNotRelated,
					Message: "The supporting release has no shared assertion or " +
						"explicit conflict with the primary release.",
				},
			)
		}
		selected = append(selected, release)
	}

	refs := make([]AgentPackageReleaseRef, 0, len(selected))
	roles := make([]AgentPackageEvidenceReleaseRole, 0, len(selected))
	sourceTypes := make([]string, 0, len(selected))
	for index, release := range selected {
		citationIDs := agentCompilationReleaseCitationIDs(*release)
		if len(citationIDs) == 0 {
			return blockedAgentCompilationCandidate(
				AgentCompilationCandidateEvidence,
				AgentCompilationIssue{
					Code: AgentCompilationIssueMissingCitations,
					Message: fmt.Sprintf(
						"Release %q has no claim citations that can be pinned.",
						boundedEvidenceID(release.ReleaseID),
					),
				},
			)
		}
		refs = append(refs, AgentPackageReleaseRef{
			ReleaseID:   release.ReleaseID,
			ContentHash: release.ContentHash,
			CitationIDs: citationIDs,
		})
		role := AgentEvidenceReleaseSupporting
		if index == 0 {
			role = AgentEvidenceReleasePrimary
		}
		roles = append(roles, AgentPackageEvidenceReleaseRole{
			ReleaseID: release.ReleaseID,
			Role:      role,
		})
		sourceTypes = append(sourceTypes, release.Book.SourceType)
	}
	pkg := AgentPackage{
		SchemaVersion:  AgentPackageSchemaVersionV2,
		PackageID:      opaqueAgentCompilationPackageID(primary.BookID, AgentCompilationCandidateEvidence),
		Version:        request.Version,
		LifecycleState: AgentPackageDraft,
		Releases:       refs,
		RetrievalPolicy: AgentPackageRetrievalPolicy{
			Strategy:           "lexical",
			AllowedSourceTypes: sortedUniqueStrings(sourceTypes),
			RequireCitations:   true,
			MaxContextChunks:   8,
		},
		ModelPolicy: AgentPackageModelPolicy{
			PreferredCapability: "reasoning",
			Fallbacks:           []string{"qwen3.7-max"},
			MaxCostUSD:          0.25,
			TimeoutMS:           30000,
		},
		PromptProfiles: []AgentPackagePromptProfile{{
			ProfileID:    "evidence-audit.v1",
			OutputSchema: AgentEvidenceReportSchemaV1,
		}},
		ToolPolicy: allReadOnlyAgentCompilationTools(),
		SafetyPolicy: AgentPackageSafetyPolicy{
			UsagePolicy:       BookUsageEvidenceOnly,
			AbstentionReasons: []string{"insufficient_evidence", "conflicting_evidence", "outside_scope"},
			EscalationTarget:  "human_review",
		},
		EvaluationPolicy: agentCompilationEvaluationPolicy(true),
		EvidencePolicy: &AgentPackageEvidencePolicy{
			ReleaseRoles:              roles,
			MinimumIndependentSources: 1,
			MaxClaims:                 agentEvidenceMaxClaims,
			MaxEvidencePerClaim:       agentEvidenceMaxEvidencePerClaim,
			AllowedVerdicts: []string{
				AgentEvidenceVerdictSupported,
				AgentEvidenceVerdictContradicted,
				AgentEvidenceVerdictMixed,
				AgentEvidenceVerdictInsufficient,
			},
			FreshnessPolicy: AgentPackageEvidenceFreshnessPolicy{
				MaxAgeDays:             365,
				RequirePublicationDate: true,
			},
			ReportSchema: AgentEvidenceReportSchemaV1,
		},
		UIManifest: AgentPackageUIManifest{
			Capabilities: []string{"reader", "search", "grounded_chat", "evidence"},
		},
	}
	applyAgentCompilationResearchPolicy(&pkg, request.ResearchEnabled)
	return finalizeAgentCompilationCandidate(store, AgentCompilationCandidateEvidence, pkg)
}

func agentCompilationReleasePublications(
	releases map[string]*KnowledgeRelease,
) map[string]KnowledgePublicationIdentity {
	publications := make(map[string]KnowledgePublicationIdentity)
	for releaseID, release := range releases {
		if release != nil {
			publications[releaseID] = canonicalKnowledgeAssemblyPublicationIdentity(release.Book)
		}
	}
	return publications
}

func discoverAgentCompilationSupport(
	store *BookKnowledgeStore,
	latestRecords []KnowledgeReleaseRecord,
	primaryReleaseID string,
) []string {
	if store == nil {
		return nil
	}
	var primary *KnowledgeRelease
	for _, record := range latestRecords {
		if record.ReleaseID != primaryReleaseID {
			continue
		}
		loaded, err := store.LoadKnowledgeRelease(record.ReleaseID)
		if err == nil {
			adaptKnowledgeAssemblyReleaseForRead(loaded)
			primary = loaded
		}
		break
	}
	if primary == nil {
		return nil
	}
	primaryPublication := canonicalKnowledgeAssemblyPublicationIdentity(primary.Book)
	candidates := append([]KnowledgeReleaseRecord(nil), latestRecords...)
	sortKnowledgeReleaseRecordsNewestFirst(candidates)
	if len(candidates) > agentCompilationMaxDiscoveryReleases {
		candidates = candidates[:agentCompilationMaxDiscoveryReleases]
	}
	for _, record := range candidates {
		if record.ReleaseID == primaryReleaseID {
			continue
		}
		candidate, err := store.LoadKnowledgeRelease(record.ReleaseID)
		if err != nil {
			continue
		}
		adaptKnowledgeAssemblyReleaseForRead(candidate)
		publication := canonicalKnowledgeAssemblyPublicationIdentity(candidate.Book)
		if !publication.IndependentSourceEligible ||
			publication.Key == primaryPublication.Key ||
			!agentCompilationReleasesRelated(*primary, *candidate) {
			continue
		}
		if _, err := BuildKnowledgeReleaseAssembly(
			store,
			KnowledgeReleaseAssemblyQuery{
				Limit:      knowledgeAssemblyMaxLimit,
				ReleaseIDs: []string{primaryReleaseID, record.ReleaseID},
			},
		); err != nil {
			continue
		}
		return []string{record.ReleaseID}
	}
	return nil
}

func agentCompilationReleasesRelated(primary, support KnowledgeRelease) bool {
	if primary.Analysis == nil || support.Analysis == nil {
		return false
	}
	assertions := make(map[string]struct{}, len(primary.Analysis.Claims))
	for _, claim := range primary.Analysis.Claims {
		base, _ := splitKnowledgeAssemblyClaimPolarity(
			normalizeKnowledgeAssemblyClaim(claim.Statement),
		)
		if base != "" {
			assertions[base] = struct{}{}
		}
	}
	for _, claim := range support.Analysis.Claims {
		base, _ := splitKnowledgeAssemblyClaimPolarity(
			normalizeKnowledgeAssemblyClaim(claim.Statement),
		)
		if _, related := assertions[base]; related && base != "" {
			return true
		}
	}
	return false
}

func agentCompilationEmptyAssembly() *KnowledgeReleaseAssembly {
	return &KnowledgeReleaseAssembly{
		AssemblyID: "assembly-empty",
		ReleaseIDs: []string{},
	}
}

func requireAgentCompilationRelease(
	releaseID string,
	assembly *KnowledgeReleaseAssembly,
	releases map[string]*KnowledgeRelease,
) (*KnowledgeRelease, *AgentCompilationIssue) {
	if assembly == nil || !stringSet(assembly.ReleaseIDs)[releaseID] {
		return nil, &AgentCompilationIssue{
			Code:    AgentCompilationIssueReleaseNotInAssembly,
			Message: "The selected release is not part of the latest Release Assembly.",
		}
	}
	release := releases[releaseID]
	if release == nil {
		return nil, &AgentCompilationIssue{
			Code:    AgentCompilationIssueReleaseInvalid,
			Message: "The selected release could not be loaded from the Assembly snapshot.",
		}
	}
	return release, nil
}

func agentCompilationReleaseCitationIDs(release KnowledgeRelease) []string {
	if release.Analysis == nil {
		return nil
	}
	citationIDs := make([]string, 0, len(release.Citations))
	for _, claim := range release.Analysis.Claims {
		citationIDs = append(citationIDs, claim.CitationIDs...)
	}
	return sortedUniqueStrings(citationIDs)
}

func allReadOnlyAgentCompilationTools() AgentPackageToolPolicy {
	toolIDs := AgentReadOnlyToolIDs()
	rules := make([]AgentPackageToolRule, 0, len(toolIDs))
	for _, toolID := range toolIDs {
		parts := strings.SplitN(toolID, "/", 2)
		if len(parts) != 2 {
			continue
		}
		rules = append(rules, AgentPackageToolRule{
			MCPServer: parts[0],
			ToolName:  parts[1],
			Decision:  AgentToolAllow,
		})
	}
	return AgentPackageToolPolicy{Tools: rules}
}

func applyAgentCompilationResearchPolicy(pkg *AgentPackage, enabled bool) {
	if pkg == nil || !enabled {
		return
	}
	pkg.SchemaVersion = AgentPackageSchemaVersionV4
	pkg.ResearchPolicy = &AgentPackageResearchPolicy{
		Modes:          []string{ResearchModeAuto, ResearchModeQuick, ResearchModeDeep},
		AllowedSources: []string{ResearchSourceKnowledge, ResearchSourceChatlog, ResearchSourcePriorRuns},
		AllowedTools:   ResearchAgentToolIDs(), MaxIterations: 8, MaxEvidenceItems: 300,
		MaxQuotedChars: 80000, MaxCostUSD: 10, RequireVerification: true,
	}
	for _, toolID := range ResearchAgentToolIDs() {
		server, tool, ok := strings.Cut(toolID, "/")
		if ok {
			pkg.ToolPolicy.Tools = append(pkg.ToolPolicy.Tools, AgentPackageToolRule{
				MCPServer: server, ToolName: tool, Decision: AgentToolAllow,
			})
		}
	}
	pkg.ModelPolicy.MaxCostUSD = pkg.ResearchPolicy.MaxCostUSD
	pkg.EvaluationPolicy.SuiteVersion = "research-agent-v1"
	pkg.EvaluationPolicy.MinimumScores = researchEvaluationMinimumScores()
	if pkg.EvidencePolicy != nil {
		for _, metric := range trustedEvidenceAuditEvaluationMetrics {
			pkg.EvaluationPolicy.MinimumScores[metric] = 1
		}
	}
	pkg.UIManifest.Capabilities = append(pkg.UIManifest.Capabilities, "deep_research")
}

func agentCompilationEvaluationPolicy(evidence bool) AgentPackageEvaluationPolicy {
	scores := map[string]float64{
		"retrieval":           0.8,
		"retrieval_precision": 0.8,
		"citations":           1,
		"faithfulness":        0.9,
		"abstention":          1,
		"tool_choice":         1,
		"tool_arguments":      1,
		"task_completion":     1,
		"latency":             1,
		"cost":                1,
	}
	suiteVersion := "book-agent-v1"
	if evidence {
		suiteVersion = "clinical-evidence-audit-v2"
		for _, metric := range []string{
			"adjudication_consistency",
			"source_independence",
			"conflict_detection",
			"report_citation_completeness",
			"safe_insufficiency",
			"proofroom_projection_completeness",
		} {
			scores[metric] = 1
		}
	}
	return AgentPackageEvaluationPolicy{
		SuiteVersion:  suiteVersion,
		MinimumScores: scores,
	}
}

func finalizeAgentCompilationCandidate(
	store *BookKnowledgeStore,
	kind string,
	pkg AgentPackage,
) AgentCompilationCandidate {
	finalized, err := FinalizeAgentPackage(pkg)
	if err == nil {
		knownTools := AgentReadOnlyToolIDs()
		if finalized.SchemaVersion == AgentPackageSchemaVersionV4 {
			knownTools = AgentPackageKnownToolIDs()
		}
		err = ValidateAgentPackage(finalized, store, knownTools)
	}
	if err != nil {
		return blockedAgentCompilationCandidate(kind, AgentCompilationIssue{
			Code:    AgentCompilationIssueReleaseInvalid,
			Message: "The generated candidate failed package validation.",
		})
	}
	return AgentCompilationCandidate{
		Kind:        kind,
		Status:      AgentCompilationCandidateReady,
		Package:     &finalized,
		NextActions: []string{AgentCompilationNextActionEvaluate},
	}
}

func blockedAgentCompilationCandidate(
	kind string,
	issues ...AgentCompilationIssue,
) AgentCompilationCandidate {
	nextAction := AgentCompilationNextActionSelectSupport
	if kind == AgentCompilationCandidateStudy && len(issues) > 0 {
		switch issues[0].Code {
		case AgentCompilationIssueReleaseNotInAssembly:
			nextAction = AgentCompilationNextActionSelectLatestRelease
		default:
			nextAction = AgentCompilationNextActionRepairEvidence
		}
	}
	return AgentCompilationCandidate{
		Kind:        kind,
		Status:      AgentCompilationCandidateBlocked,
		Issues:      append([]AgentCompilationIssue(nil), issues...),
		NextActions: []string{nextAction},
	}
}

func deriveAgentCompilationStatus(candidates []AgentCompilationCandidate) string {
	ready := 0
	for _, candidate := range candidates {
		if candidate.Status == AgentCompilationCandidateReady {
			ready++
		}
	}
	switch {
	case ready == len(candidates):
		return AgentCompilationStatusReady
	case ready == 0:
		return AgentCompilationStatusBlocked
	default:
		return AgentCompilationStatusPartial
	}
}

func opaqueAgentCompilationPackageID(bookID, kind string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(bookID)))
	return "book-agent-" + hex.EncodeToString(sum[:8]) + "-" + kind
}

func agentCompilationID(
	request AgentCompilationRequest,
	compilation AgentCompilation,
) (string, error) {
	type candidateIdentity struct {
		Kind        string                  `json:"kind"`
		Status      string                  `json:"status"`
		ContentHash string                  `json:"content_hash,omitempty"`
		Issues      []AgentCompilationIssue `json:"issues,omitempty"`
	}
	candidates := make([]candidateIdentity, 0, len(compilation.Candidates))
	for _, candidate := range compilation.Candidates {
		identity := candidateIdentity{
			Kind:   candidate.Kind,
			Status: candidate.Status,
			Issues: append([]AgentCompilationIssue(nil), candidate.Issues...),
		}
		if candidate.Package != nil {
			identity.ContentHash = candidate.Package.ContentHash
		}
		candidates = append(candidates, identity)
	}
	seed := struct {
		CompilerVersion string                  `json:"compiler_version"`
		AssemblyID      string                  `json:"assembly_id"`
		Request         AgentCompilationRequest `json:"request"`
		Status          string                  `json:"status"`
		Candidates      []candidateIdentity     `json:"candidates"`
	}{
		CompilerVersion: AgentCompilerVersion,
		AssemblyID:      compilation.AssemblyID,
		Request:         request,
		Status:          compilation.Status,
		Candidates:      candidates,
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "compilation-" + hex.EncodeToString(sum[:]), nil
}

func boundedAgentCompilationIssueMessage(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= agentCompilationMaxIssueMessageRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:agentCompilationMaxIssueMessageRunes])
}
