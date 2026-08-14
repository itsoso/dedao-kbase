package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	evidenceAuditModelOutputSchema         = "evidence-audit-model.v1"
	evidenceAuditModelSystemPromptTemplate = "Return one strict JSON object for schema {{schema}}. " +
		"Use only the listed pinned evidence. candidate_verdict is advisory; code decides the final verdict. " +
		"Evidence stance must be supports or contradicts. Do not provide diagnosis, treatment, or individual medical advice."
	evidenceAuditModelUserPromptTemplate = "Source claim: {{source_claim}}\n" +
		"Pinned supporting evidence (metadata and statements only):\n{{evidence}}"
)

var (
	ErrEvidenceAuditModelOutcomeUnknown = errors.New("model_outcome_unknown: requires_manual_retry")
	ErrEvidenceAuditExecutionBusy       = errors.New("evidence audit execution busy")
)

var (
	evidenceAuditEnglishAgePattern      = regexp.MustCompile(`(?i)\bage\s*[:=]?\s*\d+|\b\d+\s*[- ]?year[- ]old\b`)
	evidenceAuditChineseAgePattern      = regexp.MustCompile(`年龄\s*[:：]?\s*\d+|\d+\s*岁`)
	evidenceAuditDecisionSubjectPattern = regexp.MustCompile(
		`(?i)\b(?:should|can|could|would)\s+([a-z][a-z'-]*)`,
	)
	evidenceAuditEnglishPICOPattern = regexp.MustCompile(
		`(?i)\bin\s+(?:adults|patients|children|participants)\s+with\s+[^?]{1,180},\s*does\s+[^?]{1,180}\s+(?:improve|reduce|increase|affect|prevent)\s+[^?]{1,180}\s+compared\s+with\s+[^?]{1,180}\??$`,
	)
	evidenceAuditChineseIndividualDecisionPattern = regexp.MustCompile(
		`[\p{Han}]{2,4}(?:做|接受|进行|使用|服用).{0,30}(?:手术|治疗|检查|药).{0,20}(?:合适|应该|可以|是否|吗)`,
	)
	evidenceAuditEnglishNamedDecisionPattern = regexp.MustCompile(
		`(?i)\b(?:appropriate|suitable|right|recommended)\s+for\s+[A-Z][a-z'-]+\b|\bshould\s+[A-Z][a-z'-]+\b`,
	)
)

type EvidenceAuditRunnerConfig struct {
	ModelConfig BookTokenPlanConfig
	Timeout     time.Duration
	// BootstrapTimeout bounds loading the audit and its runtime descriptor when
	// Timeout is not explicitly supplied. The package timeout starts after the
	// descriptor has been validated and covers the remaining workflow.
	BootstrapTimeout time.Duration
	Now              func() time.Time
	LeaseOwner       string
	observer         *evidenceAuditObserver
}

var evidenceAuditRuntimeStageHook = func(context.Context, string) error { return nil }

var evidenceAuditStageNames = []string{
	"package_validation", "claim_selection", "retrieval", "citation_resolution",
	"model", "report_persistence", "trace_persistence",
}

type evidenceAuditObserver struct {
	now               func() time.Time
	stages            map[string]*evidenceAuditStageState
	citationAttempted int
	citationResolved  int
	publications      map[string]bool
	freshness         []AgentTraceFreshnessDecision
	freshnessSeen     map[string]bool
	reservedCostUSD   float64
	abstentionReason  string
	usage             AgentTraceUsage
	usageIncomplete   bool
}

type evidenceAuditStageState struct {
	status    string
	startedAt time.Time
	duration  time.Duration
}

func newEvidenceAuditObserver(config EvidenceAuditRunnerConfig) *evidenceAuditObserver {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	stages := make(map[string]*evidenceAuditStageState, len(evidenceAuditStageNames))
	for _, name := range evidenceAuditStageNames {
		stages[name] = &evidenceAuditStageState{status: "pending"}
	}
	return &evidenceAuditObserver{
		now: now, stages: stages, publications: map[string]bool{},
		freshnessSeen: map[string]bool{},
		usage:         AgentTraceUsage{Status: "unknown"},
	}
}

func (o *evidenceAuditObserver) begin(name string) {
	if o == nil {
		return
	}
	stage := o.stages[name]
	if stage == nil || !stage.startedAt.IsZero() {
		return
	}
	stage.startedAt = o.now()
}

func (o *evidenceAuditObserver) end(name, status string) {
	if o == nil {
		return
	}
	stage := o.stages[name]
	if stage == nil {
		return
	}
	if !stage.startedAt.IsZero() {
		elapsed := o.now().Sub(stage.startedAt)
		if elapsed > 0 {
			stage.duration += elapsed
		}
		stage.startedAt = time.Time{}
	}
	if stage.status != "failed" {
		stage.status = status
	}
}

func (o *evidenceAuditObserver) fail(name string) {
	o.end(name, "failed")
}

func (o *evidenceAuditObserver) snapshot(traceOutcome string) *AgentTraceObservability {
	if o == nil {
		return nil
	}
	stages := make([]AgentTraceStage, 0, len(evidenceAuditStageNames))
	for _, name := range evidenceAuditStageNames {
		stage := o.stages[name]
		status := stage.status
		if status == "pending" && stage.startedAt.IsZero() {
			status = "skipped"
		}
		stages = append(stages, AgentTraceStage{
			Name: name, Status: status, DurationMS: stage.duration.Milliseconds(),
			Definition: evidenceAuditStageDefinition(name),
		})
	}
	rate := float64(0)
	if o.citationAttempted > 0 {
		rate = float64(o.citationResolved) / float64(o.citationAttempted)
	}
	reason := ""
	if traceOutcome == AgentTraceOutcomeAbstained {
		reason = trimRunes(strings.TrimSpace(o.abstentionReason), 256)
	}
	return &AgentTraceObservability{
		Stages: stages, CitationResolutionRate: rate,
		IndependentPublicationSourceCount: len(o.publications),
		FreshnessDecisions:                append([]AgentTraceFreshnessDecision(nil), o.freshness...),
		ReservedCostUSD:                   o.reservedCostUSD,
		AbstentionReason:                  reason, Usage: o.usage,
		TerminalProtocol: "prepared-report-trace-receipt-audit-publish.v2",
	}
}

func (o *evidenceAuditObserver) recordFreshness(
	releaseID, citationID, publishedAt, decision string,
) {
	if o == nil || len(o.freshness) >= evidenceAuditMaxListItems {
		return
	}
	key := releaseID + "\x00" + citationID
	if o.freshnessSeen[key] {
		return
	}
	o.freshnessSeen[key] = true
	o.freshness = append(o.freshness, AgentTraceFreshnessDecision{
		ReleaseID: releaseID, CitationID: citationID,
		PublishedAt: strings.TrimSpace(publishedAt), Decision: decision,
	})
}

func (o *evidenceAuditObserver) recordReservation(value float64) {
	if o == nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	o.reservedCostUSD += value
}

func (o *evidenceAuditObserver) mergeUsage(value AgentTraceUsage) {
	if o == nil {
		return
	}
	if validateAgentTraceUsage(value) != nil || value.Status == "unknown" {
		o.usageIncomplete = true
		o.usage = AgentTraceUsage{Status: "unknown"}
		return
	}
	if o.usageIncomplete {
		return
	}
	if o.usage.Status == "unknown" {
		o.usage = AgentTraceUsage{Status: "reported", CostStatus: value.CostStatus}
	}
	if o.usage.TotalTokens+value.TotalTokens > 5_000_000 ||
		o.usage.CostUSD+value.CostUSD > 1_000_000 {
		o.usageIncomplete = true
		o.usage = AgentTraceUsage{Status: "unknown"}
		return
	}
	o.usage.PromptTokens += value.PromptTokens
	o.usage.CompletionTokens += value.CompletionTokens
	o.usage.TotalTokens += value.TotalTokens
	if o.usage.CostStatus == "unknown" || value.CostStatus == "unknown" {
		o.usage.CostStatus = "unknown"
		o.usage.CostUSD = 0
		return
	}
	o.usage.CostStatus = "reported"
	o.usage.CostUSD += value.CostUSD
}

func agentTraceUsageFromProvider(usage *BookKnowledgeLLMUsage) AgentTraceUsage {
	if usage == nil || usage.PromptTokens < 0 || usage.CompletionTokens < 0 ||
		usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens ||
		usage.TotalTokens > 5_000_000 ||
		(usage.CostUSD != nil && (*usage.CostUSD < 0 || *usage.CostUSD > 1_000_000)) {
		return AgentTraceUsage{Status: "unknown"}
	}
	value := AgentTraceUsage{
		Status: "reported", PromptTokens: usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens,
		CostStatus: "unknown",
	}
	if usage.CostUSD != nil {
		value.CostStatus = "reported"
		value.CostUSD = *usage.CostUSD
	}
	return value
}

func evidenceAuditStageDefinition(name string) string {
	switch name {
	case "report_persistence":
		return "immutable_report_preparation"
	case "trace_persistence":
		return "durable_trace_terminal_preparation"
	default:
		return ""
	}
}

func evidenceAuditChatWithUsage(
	ctx context.Context,
	client BookKnowledgeLLMClient,
	config BookTokenPlanConfig,
	messages []BookKnowledgeMessage,
) (BookKnowledgeLLMResult, error) {
	if enhanced, ok := client.(BookKnowledgeLLMClientWithResult); ok {
		return enhanced.ChatWithResult(ctx, config, messages)
	}
	content, err := client.Chat(ctx, config, messages)
	return BookKnowledgeLLMResult{Content: content}, err
}

type evidenceAuditModelDecision struct {
	CandidateVerdict string                       `json:"candidate_verdict"`
	Rationale        string                       `json:"rationale"`
	Evidence         []evidenceAuditModelEvidence `json:"evidence"`
	Limitations      []string                     `json:"limitations"`
	KnowledgeGaps    []string                     `json:"knowledge_gaps"`
	ReviewActions    []string                     `json:"review_actions"`
}

type evidenceAuditModelEvidence struct {
	ReleaseID  string `json:"release_id"`
	CitationID string `json:"citation_id"`
	Stance     string `json:"stance"`
}

type evidenceAuditRetrievedItem struct {
	Evidence AgentPackageEvidence
	Ref      EvidenceAuditEvidenceRef
}

func PrepareEvidenceAuditInput(
	store *BookKnowledgeStore,
	packageID, version, subject, scope string,
) (EvidenceAuditInput, error) {
	pkg, releases, err := loadEvidenceAuditPackageSnapshot(store, packageID, version)
	if err != nil {
		return EvidenceAuditInput{}, err
	}
	primary, ok := evidenceAuditReleaseByRole(pkg, releases, AgentEvidenceReleasePrimary)
	if !ok || primary.Analysis == nil {
		return EvidenceAuditInput{}, fmt.Errorf("evidence audit primary release has no structured claims")
	}
	selectedClaims := make([]string, 0, pkg.EvidencePolicy.MaxClaims)
	for _, claim := range primary.Analysis.Claims {
		statement := strings.TrimSpace(claim.Statement)
		if statement == "" {
			continue
		}
		selectedClaims = append(selectedClaims, statement)
		if len(selectedClaims) == pkg.EvidencePolicy.MaxClaims {
			break
		}
	}
	if len(selectedClaims) == 0 {
		return EvidenceAuditInput{}, fmt.Errorf("evidence audit primary release has no selectable claims")
	}
	releaseRefs, err := evidenceAuditInputReleaseRefs(pkg, releases)
	if err != nil {
		return EvidenceAuditInput{}, err
	}
	model := normalizeBookTokenPlanModel(firstAgentPackageModel(pkg.ModelPolicy))
	if model == "" {
		return EvidenceAuditInput{}, fmt.Errorf("model_policy has no executable fallback model")
	}
	input := EvidenceAuditInput{
		SchemaVersion: EvidenceAuditSchemaVersion,
		Package: EvidenceAuditPackageRef{
			PackageID: pkg.PackageID, Version: pkg.Version, ContentHash: pkg.ContentHash,
		},
		EvidencePolicy: EvidenceAuditPolicySnapshot{
			MinimumIndependentSources: pkg.EvidencePolicy.MinimumIndependentSources,
			MaxClaims:                 pkg.EvidencePolicy.MaxClaims,
			MaxEvidencePerClaim:       pkg.EvidencePolicy.MaxEvidencePerClaim,
		},
		Model: EvidenceAuditModelIdentity{
			Provider: "tokenplan", Model: model, Route: "evidence-audit",
		},
		Retrieval: EvidenceAuditRetrievalIdentity{
			Strategy:         pkg.RetrievalPolicy.Strategy,
			IndexVersion:     "package-" + strings.TrimPrefix(pkg.ContentHash, "sha256:")[:16],
			RerankerVersion:  pkg.RetrievalPolicy.RerankerVersion,
			EmbeddingVersion: pkg.RetrievalPolicy.EmbeddingVersion,
		},
		Releases:       releaseRefs,
		Subject:        strings.TrimSpace(subject),
		Scope:          strings.TrimSpace(scope),
		SelectedClaims: selectedClaims,
	}
	if _, err := EvidenceAuditInputHash(input); err != nil {
		return EvidenceAuditInput{}, err
	}
	return input, nil
}

func RunEvidenceAudit(
	ctx context.Context,
	store *BookKnowledgeStore,
	auditID string,
	client BookKnowledgeLLMClient,
	config EvidenceAuditRunnerConfig,
) (*EvidenceAudit, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence audit store is required")
	}
	startedAt := time.Now()
	bootstrapTimeout := config.BootstrapTimeout
	if bootstrapTimeout <= 0 {
		bootstrapTimeout = 30 * time.Second
	}
	runCtx, bootstrapCancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer bootstrapCancel()
	if config.Timeout > 0 {
		bootstrapCancel()
		var explicitCancel context.CancelFunc
		runCtx, explicitCancel = context.WithDeadline(ctx, startedAt.Add(config.Timeout))
		defer explicitCancel()
	}
	auditID = strings.TrimSpace(auditID)
	audit, err := store.LoadEvidenceAudit(auditID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.LeaseOwner) != "" {
		if err := store.ValidateEvidenceAuditLease(auditID, config.LeaseOwner, evidenceAuditRunnerNow(config)); err != nil {
			return nil, err
		}
	}
	config.observer = newEvidenceAuditObserver(config)
	config.observer.begin("package_validation")
	traceID := audit.TraceID
	if audit.Status == EvidenceAuditQueued {
		traceID, err = newAgentRuntimeTraceID()
		if err != nil {
			traceID = "trace-" + strings.TrimPrefix(audit.InputHash, "sha256:")[:24]
		}
		audit, err = StartEvidenceAudit(store, audit.AuditID, traceID, evidenceAuditRunnerNow(config))
		if err != nil {
			reloaded, loadErr := store.LoadEvidenceAudit(auditID)
			if loadErr != nil || (reloaded.Status != EvidenceAuditRunning &&
				reloaded.Status != EvidenceAuditCompleted &&
				reloaded.Status != EvidenceAuditFailed) {
				return nil, err
			}
			audit = reloaded
			traceID = audit.TraceID
		}
	}
	failEarly := func(cause error) (*EvidenceAudit, error) {
		config.observer.fail("package_validation")
		return nil, failEvidenceAuditRun(store, *audit, nil, nil, traceID, config, cause)
	}
	if err := evidenceAuditRunnerStage(runCtx, "load"); err != nil {
		return failEarly(err)
	}
	descriptor, err := store.LoadAgentPackageV2RuntimeDescriptorContext(
		runCtx,
		audit.Package.PackageID, audit.Package.Version, audit.Package.ContentHash,
	)
	if err != nil {
		return failEarly(fmt.Errorf("load package runtime descriptor: %w", err))
	}
	if config.Timeout <= 0 {
		bootstrapCancel()
		var packageCancel context.CancelFunc
		runCtx, packageCancel = context.WithTimeout(
			ctx, time.Duration(descriptor.TimeoutMS)*time.Millisecond,
		)
		defer packageCancel()
	}
	if err := evidenceAuditRunnerStage(runCtx, "package_descriptor_loaded"); err != nil {
		return failEarly(err)
	}
	unlockExecution, err := store.acquireEvidenceAuditExecutionLock(
		runCtx, auditID,
	)
	if err != nil {
		if errors.Is(err, ErrEvidenceAuditExecutionBusy) || errors.Is(err, ErrEvidenceAuditLeaseLost) {
			return nil, err
		}
		return failEarly(err)
	}
	defer unlockExecution()
	audit, err = store.LoadEvidenceAudit(auditID)
	if err != nil {
		return failEarly(fmt.Errorf("reload evidence audit after execution lock: %w", err))
	}
	if recovered, ok, recoverErr := recoverEvidenceAuditTerminal(runCtx, store, *audit, config); ok || recoverErr != nil {
		return recovered, recoverErr
	}
	switch audit.Status {
	case EvidenceAuditCompleted:
		if _, err := store.LoadAgentTrace(audit.TraceID); err != nil {
			return nil, fmt.Errorf("completed evidence audit trace is unavailable: %w", err)
		}
		return audit, nil
	case EvidenceAuditFailed:
		if _, err := store.LoadAgentTrace(audit.TraceID); err != nil {
			return nil, fmt.Errorf("failed evidence audit trace is unavailable: %w", err)
		}
		return nil, fmt.Errorf("evidence audit %q already failed: %s", audit.AuditID, audit.FailureCode)
	case EvidenceAuditQueued, EvidenceAuditRunning:
	default:
		return nil, fmt.Errorf("evidence audit %q has unsupported status %q", audit.AuditID, audit.Status)
	}
	pkg, runErr := loadEvidenceAuditPackageContext(
		runCtx, store, audit.Package.PackageID, audit.Package.Version,
	)
	var releases map[string]KnowledgeRelease
	if runErr == nil {
		runErr = evidenceAuditRunnerStage(runCtx, "package_loaded")
	}
	if runErr == nil {
		releases, runErr = loadEvidenceAuditReleasesContext(runCtx, store, pkg)
	}
	if runErr == nil && pkg.ContentHash != audit.Package.ContentHash {
		runErr = fmt.Errorf("published package hash changed")
	}
	if runErr == nil {
		config.observer.begin("claim_selection")
		runErr = validateEvidenceAuditRunnerInput(*audit, pkg, releases)
		if runErr == nil {
			config.observer.end("claim_selection", "completed")
		} else {
			config.observer.fail("claim_selection")
		}
	}
	if runErr != nil {
		config.observer.fail("package_validation")
		return nil, failEvidenceAuditRun(store, *audit, nil, nil, traceID, config, runErr)
	}
	config.observer.end("package_validation", "completed")
	if runErr = evidenceAuditRunnerStage(runCtx, "policy"); runErr != nil {
		return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, runErr)
	}
	if evidenceAuditRequestsMedicalAdvice(audit.Subject) || evidenceAuditRequestsMedicalAdvice(audit.Scope) {
		if !evidenceAuditVerdictAllowed(pkg, EvidenceAuditVerdictInsufficient) {
			return nil, failEvidenceAuditRun(
				store, *audit, pkg, nil, traceID, config,
				fmt.Errorf("evidence_policy.allowed_verdicts excludes required insufficient abstention"),
			)
		}
		config.observer.abstentionReason = "individual_medical_advice_out_of_scope"
		claims := make([]EvidenceAuditClaim, 0, len(audit.SelectedClaims))
		for _, claim := range audit.SelectedClaims {
			claims = append(claims, EvidenceAuditClaim{
				SourceClaim: claim, NormalizedStatement: claim,
				Verdict: EvidenceAuditVerdictInsufficient, ComputedConfidence: 0,
				Limitations:   []string{"Individual diagnosis or treatment advice is outside this evidence audit."},
				KnowledgeGaps: []string{"A licensed clinician must assess individual context."},
				ReviewActions: []string{"Use the audit only as evidence review, not medical advice."},
			})
		}
		return completeEvidenceAuditRun(
			runCtx, store, *audit, pkg, nil, claims, traceID, config, AgentTraceOutcomeAbstained,
		)
	}
	if client == nil {
		client = NewTokenPlanChatClient(nil)
	}
	modelConfig, err := evidenceAuditRunnerModelConfig(pkg, config)
	if err != nil {
		return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, err)
	}
	allRetrieved := make([]evidenceAuditRetrievedItem, 0)
	claimAudits := make([]EvidenceAuditClaim, 0, len(audit.SelectedClaims))
	executionPlan, err := store.prepareEvidenceAuditExecutionPlan(*audit)
	if err != nil {
		return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, err)
	}
	checkpoints, err := store.loadEvidenceAuditClaimCandidates(executionPlan)
	if err != nil {
		return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, err)
	}
	costLedger := newEvidenceAuditCostLedger(pkg.ModelPolicy.MaxCostUSD)
	for claimIndex := 0; claimIndex < len(audit.SelectedClaims); claimIndex++ {
		if checkpoint, ok := checkpoints[claimIndex]; ok {
			if err := costLedger.Restore(checkpoint.ReservationUSD); err != nil {
				return nil, failEvidenceAuditRun(store, *audit, pkg, nil, traceID, config, err)
			}
			config.observer.recordReservation(checkpoint.ReservationUSD)
		}
	}
	for claimIndex, sourceClaim := range audit.SelectedClaims {
		config.observer.begin("retrieval")
		retrieved, retrievalErr := retrieveEvidenceAuditSupportingEvidence(
			runCtx, store, pkg, releases, sourceClaim, evidenceAuditReferenceTime(*audit, config), config.observer,
		)
		if retrievalErr != nil {
			config.observer.fail("retrieval")
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, retrievalErr)
		}
		config.observer.end("retrieval", "completed")
		allRetrieved = append(allRetrieved, retrieved...)
		messages := buildEvidenceAuditModelMessages(pkg, sourceClaim, retrieved)
		claimModelConfig := modelConfig
		reservationUSD := float64(0)
		if checkpoint, ok := checkpoints[claimIndex]; ok {
			reservationUSD = checkpoint.ReservationUSD
			if err := applyAgentRuntimeCostBudget(&claimModelConfig, messages, reservationUSD); err != nil {
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
			}
		} else {
			reservationUSD, err = costLedger.Reserve(
				messages, &claimModelConfig, len(audit.SelectedClaims)-claimIndex,
			)
			if err != nil {
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
			}
			config.observer.recordReservation(reservationUSD)
		}
		requestIdentity, err := evidenceAuditModelRequestIdentity(
			executionPlan, claimIndex, claimModelConfig, messages,
		)
		if err != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
		}
		retrievalFingerprint, err := evidenceAuditRetrievalSnapshotFingerprint(audit.Releases, retrieved)
		if err != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
		}
		var decision evidenceAuditModelDecision
		if checkpoint, ok := checkpoints[claimIndex]; ok {
			if checkpoint.RequestIdentity != requestIdentity ||
				checkpoint.RetrievalFingerprint != retrievalFingerprint {
				return nil, failEvidenceAuditRun(
					store, *audit, pkg, allRetrieved, traceID, config,
					fmt.Errorf("checkpoint identity no longer matches canonical model request and retrieval snapshot"),
				)
			}
			decision = checkpoint.Decision
			config.observer.mergeUsage(checkpoint.Usage)
		} else {
			invocation, created, err := store.beginEvidenceAuditModelInvocation(
				executionPlan, claimIndex, requestIdentity,
			)
			if err != nil {
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
			}
			if !created {
				if invocation.Status == evidenceAuditInvocationInFlight {
					return nil, failEvidenceAuditRun(
						store, *audit, pkg, allRetrieved, traceID, config,
						ErrEvidenceAuditModelOutcomeUnknown,
					)
				}
				return nil, failEvidenceAuditRun(
					store, *audit, pkg, allRetrieved, traceID, config,
					fmt.Errorf("completed model invocation is missing its candidate"),
				)
			}
			if err := evidenceAuditRunnerStage(runCtx, "model"); err != nil {
				config.observer.fail("model")
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, err)
			}
			config.observer.begin("model")
			result, modelErr := evidenceAuditChatWithUsage(runCtx, client, claimModelConfig, messages)
			if modelErr != nil {
				config.observer.fail("model")
				if errors.Is(modelErr, context.Canceled) || errors.Is(modelErr, context.DeadlineExceeded) {
					modelErr = ErrEvidenceAuditModelOutcomeUnknown
				}
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, modelErr)
			}
			config.observer.end("model", "completed")
			checkpointUsage := agentTraceUsageFromProvider(result.Usage)
			config.observer.mergeUsage(checkpointUsage)
			raw := result.Content
			if err := evidenceAuditRunnerStage(runCtx, "model_completed_before_checkpoint"); err != nil {
				config.observer.fail("model")
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, failEvidenceAuditRun(
						store, *audit, pkg, allRetrieved, traceID, config,
						ErrEvidenceAuditModelOutcomeUnknown,
					)
				}
				return nil, err
			}
			if err := evidenceAuditRunnerStage(runCtx, "model"); err != nil {
				config.observer.fail("model")
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, failEvidenceAuditRun(
						store, *audit, pkg, allRetrieved, traceID, config,
						ErrEvidenceAuditModelOutcomeUnknown,
					)
				}
				return nil, err
			}
			parsed, parseErr := parseEvidenceAuditModelDecision(raw)
			if parseErr != nil {
				return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, parseErr)
			}
			parsed = evidenceAuditCheckpointDecision(parsed)
			checkpoint, checkpointErr := store.saveEvidenceAuditClaimCandidate(
				executionPlan, claimIndex, requestIdentity, retrievalFingerprint,
				parsed, checkpointUsage, reservationUSD,
			)
			if checkpointErr != nil {
				return nil, failEvidenceAuditRun(
					store, *audit, pkg, allRetrieved, traceID, config, checkpointErr,
				)
			}
			checkpoints[claimIndex] = checkpoint
			decision = checkpoint.Decision
			if checkpointErr := store.completeEvidenceAuditModelInvocation(
				executionPlan, invocation, checkpoint.CandidateHash,
			); checkpointErr != nil {
				return nil, failEvidenceAuditRun(
					store, *audit, pkg, allRetrieved, traceID, config, checkpointErr,
				)
			}
			if checkpointErr := evidenceAuditRunnerStage(runCtx, "checkpoint"); checkpointErr != nil {
				if errors.Is(checkpointErr, context.Canceled) ||
					errors.Is(checkpointErr, context.DeadlineExceeded) {
					return nil, failEvidenceAuditRun(
						store, *audit, pkg, allRetrieved, traceID, config, checkpointErr,
					)
				}
				return nil, checkpointErr
			}
		}
		claimAudit, decisionErr := decideEvidenceAuditClaim(pkg, sourceClaim, retrieved, decision)
		if decisionErr != nil {
			return nil, failEvidenceAuditRun(store, *audit, pkg, allRetrieved, traceID, config, decisionErr)
		}
		claimAudits = append(claimAudits, claimAudit)
	}
	return completeEvidenceAuditRun(
		runCtx, store, *audit, pkg, allRetrieved, claimAudits, traceID, config, AgentTraceOutcomeCompleted,
	)
}

type evidenceAuditCostLedger struct {
	maxUSD      float64
	reservedUSD float64
}

func newEvidenceAuditCostLedger(maxUSD float64) *evidenceAuditCostLedger {
	return &evidenceAuditCostLedger{maxUSD: maxUSD}
}

func (l *evidenceAuditCostLedger) Remaining() float64 {
	if l == nil {
		return 0
	}
	remaining := l.maxUSD - l.reservedUSD
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (l *evidenceAuditCostLedger) Restore(reservationUSD float64) error {
	if l == nil || reservationUSD <= 0 || math.IsNaN(reservationUSD) ||
		math.IsInf(reservationUSD, 0) || reservationUSD > l.Remaining()+1e-9 {
		return fmt.Errorf("model_policy.max_cost_usd checkpoint reservation exceeds audit budget")
	}
	l.reservedUSD += reservationUSD
	return nil
}

func (l *evidenceAuditCostLedger) Reserve(
	messages []BookKnowledgeMessage,
	config *BookTokenPlanConfig,
	remainingClaims int,
) (float64, error) {
	if l == nil || remainingClaims <= 0 {
		return 0, fmt.Errorf("model_policy.max_cost_usd cost ledger is invalid")
	}
	share := l.Remaining() / float64(remainingClaims)
	if err := applyAgentRuntimeCostBudget(config, messages, share); err != nil {
		return 0, err
	}
	reservation := agentRuntimeEstimatedMaxCostUSD(messages, config.MaxTokens)
	if reservation <= 0 || reservation > l.Remaining()+1e-9 {
		return 0, fmt.Errorf("model_policy.max_cost_usd cost budget is exhausted before the model call")
	}
	l.reservedUSD += reservation
	return reservation, nil
}

func evidenceAuditCheckpointDecision(
	decision evidenceAuditModelDecision,
) evidenceAuditModelDecision {
	return evidenceAuditModelDecision{
		CandidateVerdict: decision.CandidateVerdict,
		Rationale:        "Candidate evaluated against immutable pinned evidence.",
		Evidence:         append([]evidenceAuditModelEvidence(nil), decision.Evidence...),
		Limitations:      evidenceAuditBoundedStrings(decision.Limitations),
		KnowledgeGaps:    evidenceAuditBoundedStrings(decision.KnowledgeGaps),
		ReviewActions:    evidenceAuditBoundedStrings(decision.ReviewActions),
	}
}

func evidenceAuditRetrievalSnapshotFingerprint(
	releases []EvidenceAuditReleaseRef,
	retrieved []evidenceAuditRetrievedItem,
) (string, error) {
	type releaseIdentity struct {
		ReleaseID   string `json:"release_id"`
		ContentHash string `json:"content_hash"`
	}
	type evidenceIdentity struct {
		ReleaseID   string `json:"release_id"`
		ContentHash string `json:"content_hash"`
		ClaimID     string `json:"claim_id"`
		ChunkID     string `json:"chunk_id"`
		CitationID  string `json:"citation_id"`
	}
	releaseValues := make([]releaseIdentity, 0, len(releases))
	for _, release := range releases {
		releaseValues = append(releaseValues, releaseIdentity{
			ReleaseID: release.ReleaseID, ContentHash: release.ContentHash,
		})
	}
	sort.Slice(releaseValues, func(i, j int) bool {
		return releaseValues[i].ReleaseID < releaseValues[j].ReleaseID
	})
	evidenceValues := make([]evidenceIdentity, 0, len(retrieved))
	for _, item := range retrieved {
		evidenceValues = append(evidenceValues, evidenceIdentity{
			ReleaseID: item.Ref.ReleaseID, ContentHash: item.Ref.ContentHash,
			ClaimID: item.Ref.ClaimID, ChunkID: item.Ref.ChunkID,
			CitationID: item.Ref.CitationID,
		})
	}
	sort.Slice(evidenceValues, func(i, j int) bool {
		left, right := evidenceValues[i], evidenceValues[j]
		if left.ReleaseID != right.ReleaseID {
			return left.ReleaseID < right.ReleaseID
		}
		if left.ClaimID != right.ClaimID {
			return left.ClaimID < right.ClaimID
		}
		if left.ChunkID != right.ChunkID {
			return left.ChunkID < right.ChunkID
		}
		return left.CitationID < right.CitationID
	})
	payload, err := json.Marshal(struct {
		Releases []releaseIdentity  `json:"releases"`
		Evidence []evidenceIdentity `json:"evidence"`
	}{Releases: releaseValues, Evidence: evidenceValues})
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func evidenceAuditModelRequestIdentity(
	plan evidenceAuditExecutionPlan,
	claimIndex int,
	config BookTokenPlanConfig,
	messages []BookKnowledgeMessage,
) (string, error) {
	messagesPayload, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		AuditID            string `json:"audit_id"`
		InputHash          string `json:"input_hash"`
		ClaimIndex         int    `json:"claim_index"`
		ClaimHash          string `json:"claim_hash"`
		Model              string `json:"model"`
		MaxTokens          int    `json:"max_tokens"`
		EnableThinking     *bool  `json:"enable_thinking,omitempty"`
		OutputSchemaHash   string `json:"output_schema_hash"`
		PromptTemplateHash string `json:"prompt_template_hash"`
		MessagesHash       string `json:"messages_hash"`
	}{
		AuditID: plan.AuditID, InputHash: plan.InputHash,
		ClaimIndex: claimIndex, ClaimHash: plan.ClaimHashes[claimIndex],
		Model: normalizeBookTokenPlanModel(config.Model), MaxTokens: config.MaxTokens,
		EnableThinking:   config.EnableThinking,
		OutputSchemaHash: sha256Fingerprint([]byte(evidenceAuditModelOutputSchema)),
		PromptTemplateHash: sha256Fingerprint([]byte(
			evidenceAuditModelSystemPromptTemplate + "\x00" + evidenceAuditModelUserPromptTemplate,
		)),
		MessagesHash: sha256Fingerprint(messagesPayload),
	})
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func loadEvidenceAuditPackageSnapshot(
	store *BookKnowledgeStore,
	packageID, version string,
) (AgentPackage, map[string]KnowledgeRelease, error) {
	return loadEvidenceAuditPackageSnapshotContext(
		context.Background(), store, packageID, version,
	)
}

func loadEvidenceAuditPackageSnapshotContext(
	ctx context.Context,
	store *BookKnowledgeStore,
	packageID, version string,
) (AgentPackage, map[string]KnowledgeRelease, error) {
	pkg, err := loadEvidenceAuditPackageContext(ctx, store, packageID, version)
	if err != nil {
		return AgentPackage{}, nil, err
	}
	releases, err := loadEvidenceAuditReleasesContext(ctx, store, pkg)
	if err != nil {
		return AgentPackage{}, nil, err
	}
	return pkg, releases, nil
}

func loadEvidenceAuditPackageContext(
	ctx context.Context,
	store *BookKnowledgeStore,
	packageID, version string,
) (AgentPackage, error) {
	if err := ctx.Err(); err != nil {
		return AgentPackage{}, err
	}
	if err := evidenceAuditRunnerStage(ctx, "package_load"); err != nil {
		return AgentPackage{}, err
	}
	if err := evidenceAuditRunnerStage(ctx, "package_artifact_load"); err != nil {
		return AgentPackage{}, err
	}
	pkg, err := loadRunnableAgentPackageContext(ctx, store, packageID, version, "evidence")
	if err != nil {
		return AgentPackage{}, err
	}
	if err := ctx.Err(); err != nil {
		return AgentPackage{}, err
	}
	if (pkg.SchemaVersion != AgentPackageSchemaVersionV2 && pkg.SchemaVersion != AgentPackageSchemaVersionV3) || pkg.EvidencePolicy == nil {
		return AgentPackage{}, fmt.Errorf("evidence audit requires a published evidence-capable package with evidence_policy")
	}
	return *pkg, nil
}

func loadEvidenceAuditReleasesContext(
	ctx context.Context,
	store *BookKnowledgeStore,
	pkg AgentPackage,
) (map[string]KnowledgeRelease, error) {
	if err := evidenceAuditRunnerStage(ctx, "release_load"); err != nil {
		return nil, err
	}
	releases := make(map[string]KnowledgeRelease, len(pkg.Releases))
	for _, ref := range pkg.Releases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, loadErr := store.LoadKnowledgeRelease(ref.ReleaseID)
		if loadErr != nil {
			return nil, fmt.Errorf("load pinned release %q: %w", ref.ReleaseID, loadErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if agentTraceReleaseContentHash(release.ContentHash) != agentTraceReleaseContentHash(ref.ContentHash) {
			return nil, fmt.Errorf("pinned release %q content hash changed", ref.ReleaseID)
		}
		releases[ref.ReleaseID] = *release
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return releases, nil
}

func evidenceAuditInputReleaseRefs(
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
) ([]EvidenceAuditReleaseRef, error) {
	roles := make(map[string]string, len(pkg.EvidencePolicy.ReleaseRoles))
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		roles[role.ReleaseID] = role.Role
	}
	result := make([]EvidenceAuditReleaseRef, 0, len(pkg.Releases))
	for _, pkgRef := range pkg.Releases {
		release := releases[pkgRef.ReleaseID]
		sourceType := strings.ToLower(strings.TrimSpace(release.Book.SourceType))
		ref := EvidenceAuditReleaseRef{
			ReleaseID: pkgRef.ReleaseID, ContentHash: agentTraceReleaseContentHash(pkgRef.ContentHash),
			Role: roles[pkgRef.ReleaseID], SourceType: sourceType,
			PublicationIdentity: evidenceAuditPublicationIdentity(release),
		}
		allowed := stringBoolSet(pkgRef.CitationIDs...)
		seen := map[string]bool{}
		for _, claim := range release.Analysis.Claims {
			for _, citationID := range resolveAgentClaimCitationIDs(release.Citations, claim.CitationIDs) {
				if !allowed[citationID] || seen[citationID] {
					continue
				}
				citation, ok := evidenceAuditCitationByID(release, citationID)
				if !ok || strings.TrimSpace(citation.ChunkID) == "" {
					return nil, fmt.Errorf("pinned citation %q in release %q is unresolved", citationID, release.ReleaseID)
				}
				seen[citationID] = true
				ref.Citations = append(ref.Citations, EvidenceAuditCitationRef{
					CitationID: citationID, ClaimID: claim.ID, ChunkID: citation.ChunkID,
				})
			}
		}
		if len(ref.Citations) == 0 {
			return nil, fmt.Errorf("pinned release %q has no resolvable citation identity", release.ReleaseID)
		}
		result = append(result, ref)
	}
	return result, nil
}

func evidenceAuditReleaseByRole(
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
	role string,
) (KnowledgeRelease, bool) {
	for _, item := range pkg.EvidencePolicy.ReleaseRoles {
		if item.Role == role {
			release, ok := releases[item.ReleaseID]
			return release, ok
		}
	}
	return KnowledgeRelease{}, false
}

func validateEvidenceAuditRunnerInput(
	audit EvidenceAudit,
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
) error {
	expected, err := evidenceAuditInputReleaseRefs(pkg, releases)
	if err != nil {
		return err
	}
	if len(expected) != len(audit.Releases) {
		return fmt.Errorf("audit pinned release set does not match package")
	}
	expectedByID := make(map[string]EvidenceAuditReleaseRef, len(expected))
	for _, release := range expected {
		expectedByID[release.ReleaseID] = release
	}
	for _, release := range audit.Releases {
		expectedRelease, ok := expectedByID[release.ReleaseID]
		if !ok || expectedRelease.ContentHash != release.ContentHash ||
			expectedRelease.Role != release.Role ||
			expectedRelease.PublicationIdentity != release.PublicationIdentity {
			return fmt.Errorf("audit pinned release %q no longer matches package snapshot", release.ReleaseID)
		}
	}
	primary, ok := evidenceAuditReleaseByRole(pkg, releases, AgentEvidenceReleasePrimary)
	if !ok || primary.Analysis == nil {
		return fmt.Errorf("primary release is unavailable")
	}
	allowedClaims := map[string]bool{}
	for _, claim := range primary.Analysis.Claims {
		allowedClaims[strings.TrimSpace(claim.Statement)] = true
	}
	for _, selected := range audit.SelectedClaims {
		if !allowedClaims[selected] {
			return fmt.Errorf("selected claim is outside the pinned primary release")
		}
	}
	return nil
}

func retrieveEvidenceAuditSupportingEvidence(
	ctx context.Context,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	releases map[string]KnowledgeRelease,
	sourceClaim string,
	auditTime time.Time,
	observer *evidenceAuditObserver,
) ([]evidenceAuditRetrievedItem, error) {
	roles := make(map[string]string, len(pkg.EvidencePolicy.ReleaseRoles))
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		roles[role.ReleaseID] = role.Role
	}
	result := make([]evidenceAuditRetrievedItem, 0)
	seen := map[string]bool{}
	for _, role := range pkg.EvidencePolicy.ReleaseRoles {
		if role.Role != AgentEvidenceReleaseSupporting {
			continue
		}
		if err := evidenceAuditRunnerStage(ctx, "retrieval"); err != nil {
			return nil, err
		}
		release := releases[role.ReleaseID]
		found, err := searchAgentPackageReleaseEvidenceContext(
			ctx, store, pkg, release.ReleaseID, sourceClaim, pkg.EvidencePolicy.MaxEvidencePerClaim,
		)
		if err != nil {
			return nil, err
		}
		for _, item := range found {
			for _, citationID := range item.CitationIDs {
				observer.begin("citation_resolution")
				observer.citationAttempted++
				if err := evidenceAuditRunnerStage(ctx, "citation"); err != nil {
					observer.fail("citation_resolution")
					return nil, err
				}
				citation, err := resolveAgentPackageReleaseCitationContext(
					ctx, store, pkg, release.ReleaseID, item.ClaimID, citationID,
				)
				if err != nil || strings.TrimSpace(citation.ChunkID) == "" {
					observer.fail("citation_resolution")
					if err == nil {
						err = fmt.Errorf("citation has no immutable chunk identity")
					}
					return nil, fmt.Errorf(
						"citation %q in release %q cannot be resolved: %w",
						citationID, release.ReleaseID, err,
					)
				}
				observer.citationResolved++
				observer.end("citation_resolution", "completed")
				publishedAt := strings.TrimSpace(citation.PublishedAt)
				if publishedAt == "" {
					publishedAt = strings.TrimSpace(release.Book.PublishedAt)
				}
				freshness, freshnessErr := evidenceAuditFreshnessDecision(
					publishedAt, auditTime, pkg.EvidencePolicy.FreshnessPolicy,
				)
				if freshnessErr != nil {
					return nil, fmt.Errorf(
						"citation %q in release %q freshness: %w",
						citationID, release.ReleaseID, freshnessErr,
					)
				}
				observer.recordFreshness(release.ReleaseID, citationID, publishedAt, freshness)
				if freshness == EvidenceAuditFreshnessStale ||
					(freshness == EvidenceAuditFreshnessMissing &&
						pkg.EvidencePolicy.FreshnessPolicy.RequirePublicationDate) {
					continue
				}
				ref := EvidenceAuditEvidenceRef{
					ReleaseID: release.ReleaseID, ContentHash: agentTraceReleaseContentHash(release.ContentHash),
					Role: EvidenceAuditReleaseSupporting, SourceType: strings.ToLower(strings.TrimSpace(release.Book.SourceType)),
					PublicationIdentity: evidenceAuditPublicationIdentity(release),
					ClaimID:             item.ClaimID, ChunkID: citation.ChunkID, CitationID: citationID,
					PublishedAt: publishedAt, FreshnessDecision: freshness,
				}
				observer.publications[ref.PublicationIdentity] = true
				key := evidenceAuditEvidenceIdentity(ref)
				if seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, evidenceAuditRetrievedItem{
					Evidence: AgentPackageEvidence{
						ReleaseID: release.ReleaseID, ClaimID: item.ClaimID, Statement: item.Statement,
						CitationIDs: []string{citationID}, Score: item.Score,
					},
					Ref: ref,
				})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Ref.PublicationIdentity != result[j].Ref.PublicationIdentity {
			return result[i].Ref.PublicationIdentity < result[j].Ref.PublicationIdentity
		}
		if result[i].Ref.SourceType != result[j].Ref.SourceType {
			return result[i].Ref.SourceType < result[j].Ref.SourceType
		}
		return evidenceAuditEvidenceIdentity(result[i].Ref) < evidenceAuditEvidenceIdentity(result[j].Ref)
	})
	return capEvidenceAuditSupportingGroups(result, pkg.EvidencePolicy.MaxEvidencePerClaim), nil
}

func evidenceAuditFreshnessDecision(
	publishedAt string,
	auditTime time.Time,
	policy AgentPackageEvidenceFreshnessPolicy,
) (string, error) {
	publishedAt = strings.TrimSpace(publishedAt)
	if publishedAt == "" {
		return EvidenceAuditFreshnessMissing, nil
	}
	published, err := parseEvidenceAuditPublicationDate(publishedAt)
	if err != nil {
		return "", err
	}
	if auditTime.IsZero() {
		auditTime = time.Now()
	}
	if published.After(auditTime) {
		return "", fmt.Errorf("publication date is after the audit clock")
	}
	if auditTime.Sub(published) > time.Duration(policy.MaxAgeDays)*24*time.Hour {
		return EvidenceAuditFreshnessStale, nil
	}
	return EvidenceAuditFreshnessFresh, nil
}

func evidenceAuditReferenceTime(audit EvidenceAudit, config EvidenceAuditRunnerConfig) time.Time {
	for _, value := range []string{audit.StartedAt, audit.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed
		}
	}
	return evidenceAuditRunnerNow(config)
}

func capEvidenceAuditSupportingGroups(
	evidence []evidenceAuditRetrievedItem,
	limit int,
) []evidenceAuditRetrievedItem {
	if limit <= 0 || len(evidence) <= limit {
		return evidence
	}
	groups := map[string][]evidenceAuditRetrievedItem{}
	keys := make([]string, 0)
	for _, item := range evidence {
		key := item.Ref.PublicationIdentity + "\x00" + item.Ref.SourceType
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], item)
	}
	sort.Strings(keys)
	result := make([]evidenceAuditRetrievedItem, 0, limit)
	for offset := 0; len(result) < limit; offset++ {
		added := false
		for _, key := range keys {
			if offset >= len(groups[key]) {
				continue
			}
			result = append(result, groups[key][offset])
			added = true
			if len(result) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return result
}

func evidenceAuditCitationByID(release KnowledgeRelease, citationID string) (BookKnowledgeCitation, bool) {
	for _, citation := range release.Citations {
		if citation.CitationID == citationID {
			return citation, true
		}
	}
	return BookKnowledgeCitation{}, false
}

func buildEvidenceAuditModelMessages(
	pkg AgentPackage,
	sourceClaim string,
	evidence []evidenceAuditRetrievedItem,
) []BookKnowledgeMessage {
	var evidenceBuilder strings.Builder
	for _, item := range evidence {
		fmt.Fprintf(
			&evidenceBuilder,
			"- release_id=%s citation_id=%s claim_id=%s publication_identity=%s source_type=%s\n  %s\n",
			item.Ref.ReleaseID, item.Ref.CitationID, item.Ref.ClaimID,
			item.Ref.PublicationIdentity, item.Ref.SourceType, item.Evidence.Statement,
		)
	}
	userPrompt := strings.NewReplacer(
		"{{source_claim}}", sourceClaim,
		"{{evidence}}", evidenceBuilder.String(),
	).Replace(evidenceAuditModelUserPromptTemplate)
	return []BookKnowledgeMessage{
		{
			Role: "system",
			Content: strings.ReplaceAll(
				evidenceAuditModelSystemPromptTemplate, "{{schema}}", evidenceAuditModelOutputSchema,
			),
		},
		{Role: "user", Content: userPrompt},
	}
}

func parseEvidenceAuditModelDecision(raw string) (evidenceAuditModelDecision, error) {
	var decision evidenceAuditModelDecision
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return decision, fmt.Errorf("invalid evidence audit model JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return decision, fmt.Errorf("invalid evidence audit model JSON: %w", err)
	}
	switch decision.CandidateVerdict {
	case EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted,
		EvidenceAuditVerdictMixed, EvidenceAuditVerdictInsufficient:
	default:
		return decision, fmt.Errorf("invalid candidate verdict %q", decision.CandidateVerdict)
	}
	if strings.TrimSpace(decision.Rationale) == "" {
		return decision, fmt.Errorf("model rationale is required")
	}
	for index, item := range decision.Evidence {
		if strings.TrimSpace(item.ReleaseID) == "" || strings.TrimSpace(item.CitationID) == "" {
			return decision, fmt.Errorf("model evidence[%d] requires release_id and citation_id", index)
		}
		if item.Stance != "supports" && item.Stance != "contradicts" {
			return decision, fmt.Errorf("model evidence[%d] has invalid stance %q", index, item.Stance)
		}
	}
	return decision, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return err
}

func decideEvidenceAuditClaim(
	pkg AgentPackage,
	sourceClaim string,
	retrieved []evidenceAuditRetrievedItem,
	decision evidenceAuditModelDecision,
) (EvidenceAuditClaim, error) {
	if !evidenceAuditVerdictAllowed(pkg, decision.CandidateVerdict) {
		if !evidenceAuditVerdictAllowed(pkg, EvidenceAuditVerdictInsufficient) {
			return EvidenceAuditClaim{}, fmt.Errorf(
				"evidence_policy.allowed_verdicts excludes candidate %q and insufficient fallback",
				decision.CandidateVerdict,
			)
		}
		decision.Evidence = nil
	}
	available := make(map[string]evidenceAuditRetrievedItem, len(retrieved))
	for _, item := range retrieved {
		available[item.Ref.ReleaseID+"\x00"+item.Ref.CitationID] = item
	}
	selected := make([]EvidenceAuditEvidenceRef, 0, len(decision.Evidence))
	seen := map[string]bool{}
	supports, contradicts := 0, 0
	for _, modelRef := range decision.Evidence {
		item, ok := available[modelRef.ReleaseID+"\x00"+modelRef.CitationID]
		if !ok {
			return EvidenceAuditClaim{}, fmt.Errorf(
				"model citation %q is outside retrieved pinned evidence", modelRef.CitationID,
			)
		}
		key := evidenceAuditEvidenceIdentity(item.Ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		ref := item.Ref
		if modelRef.Stance == "contradicts" {
			ref.Conflict = true
			contradicts++
		} else {
			supports++
		}
		selected = append(selected, ref)
	}
	if len(selected) > pkg.EvidencePolicy.MaxEvidencePerClaim {
		selected = selected[:pkg.EvidencePolicy.MaxEvidencePerClaim]
	}
	supports, contradicts = 0, 0
	for _, ref := range selected {
		if ref.Conflict {
			contradicts++
		} else {
			supports++
		}
	}
	verdict := EvidenceAuditVerdictInsufficient
	switch {
	case supports > 0 && contradicts > 0:
		verdict = EvidenceAuditVerdictMixed
	case supports > 0:
		verdict = EvidenceAuditVerdictSupported
	case contradicts > 0:
		verdict = EvidenceAuditVerdictContradicted
	}
	publications := map[string]bool{}
	for _, ref := range selected {
		publications[ref.PublicationIdentity] = true
	}
	if verdict != EvidenceAuditVerdictInsufficient &&
		len(publications) < pkg.EvidencePolicy.MinimumIndependentSources {
		verdict = EvidenceAuditVerdictInsufficient
		selected = nil
	}
	if !evidenceAuditVerdictAllowed(pkg, verdict) {
		if !evidenceAuditVerdictAllowed(pkg, EvidenceAuditVerdictInsufficient) {
			return EvidenceAuditClaim{}, fmt.Errorf(
				"evidence_policy.allowed_verdicts excludes computed verdict %q and insufficient fallback",
				verdict,
			)
		}
		verdict = EvidenceAuditVerdictInsufficient
		selected = nil
	}
	conflicts := evidenceConflictCount(selected)
	return EvidenceAuditClaim{
		SourceClaim: sourceClaim, NormalizedStatement: sourceClaim, Verdict: verdict,
		Evidence: selected, ComputedConfidence: ComputeEvidenceAuditConfidence(selected, conflicts),
		Limitations:   evidenceAuditBoundedStrings(decision.Limitations),
		KnowledgeGaps: evidenceAuditBoundedStrings(decision.KnowledgeGaps),
		ReviewActions: evidenceAuditBoundedStrings(decision.ReviewActions),
	}, nil
}

func evidenceAuditVerdictAllowed(pkg AgentPackage, verdict string) bool {
	for _, allowed := range pkg.EvidencePolicy.AllowedVerdicts {
		if strings.TrimSpace(allowed) == verdict {
			return true
		}
	}
	return false
}

func evidenceConflictCount(evidence []EvidenceAuditEvidenceRef) int {
	count := 0
	for _, ref := range evidence {
		if ref.Conflict {
			count++
		}
	}
	return count
}

func evidenceAuditBoundedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > evidenceAuditMaxTextBytes {
			value = value[:evidenceAuditMaxTextBytes]
		}
		result = append(result, value)
		if len(result) == evidenceAuditMaxListItems {
			break
		}
	}
	return result
}

func evidenceAuditRunnerModelConfig(
	pkg AgentPackage,
	config EvidenceAuditRunnerConfig,
) (BookTokenPlanConfig, error) {
	cfg := config.ModelConfig
	var err error
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg, err = LoadBookTokenPlanConfig()
		if err != nil {
			return BookTokenPlanConfig{}, err
		}
	}
	cfg.Model = normalizeBookTokenPlanModel(firstAgentPackageModel(pkg.ModelPolicy))
	return cfg, nil
}

func completeEvidenceAuditRun(
	ctx context.Context,
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg AgentPackage,
	retrieved []evidenceAuditRetrievedItem,
	claims []EvidenceAuditClaim,
	traceID string,
	config EvidenceAuditRunnerConfig,
	traceOutcome string,
) (*EvidenceAudit, error) {
	counts := map[string]int{}
	limitations := []string{"Audit is limited to immutable Package-pinned releases."}
	reviewItems := make([]string, 0)
	for _, claim := range claims {
		counts[claim.Verdict]++
		reviewItems = append(reviewItems, claim.ReviewActions...)
	}
	if len(reviewItems) == 0 {
		reviewItems = []string{"Review evidence applicability and unresolved gaps."}
	}
	report := EvidenceAudit{
		AuditID:        audit.AuditID,
		InputHash:      audit.InputHash,
		Package:        audit.Package,
		EvidencePolicy: audit.EvidencePolicy,
		Model:          audit.Model,
		Retrieval:      audit.Retrieval,
		Releases:       append([]EvidenceAuditReleaseRef(nil), audit.Releases...),
		Subject:        audit.Subject,
		Scope:          audit.Scope,
		SelectedClaims: append([]string(nil), audit.SelectedClaims...),
		ClaimAudits:    claims,
		Summary: EvidenceAuditSummary{
			Conclusion:    "Deterministic audit completed within the pinned evidence scope.",
			VerdictCounts: counts, Limitations: limitations,
		},
		Proofroom: EvidenceAuditProofroomProjection{
			SchemaVersion: "proofroom-evidence-task.v1",
			Title:         "Review clinical evidence audit",
			ReviewItems:   evidenceAuditBoundedStrings(reviewItems),
		},
		TraceID: traceID,
	}
	fingerprint, err := evidenceAuditReportFingerprint(report)
	if err != nil {
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	if traceOutcome == AgentTraceOutcomeCompleted {
		selectedEvidence := 0
		for _, claim := range claims {
			selectedEvidence += len(claim.Evidence)
		}
		if selectedEvidence == 0 {
			traceOutcome = AgentTraceOutcomeAbstained
		}
	}
	config.observer.begin("report_persistence")
	if err := evidenceAuditRunnerStage(ctx, "persist"); err != nil {
		config.observer.fail("report_persistence")
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	preparedReport, err := PrepareEvidenceAuditCompletion(
		store, report, evidenceAuditRunnerNow(config),
	)
	if err != nil {
		config.observer.fail("report_persistence")
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	config.observer.end("report_persistence", "completed")
	config.observer.begin("trace_persistence")
	trace, err := buildEvidenceAuditTrace(
		store, audit, pkg, retrieved, claims, traceID, traceOutcome, fingerprint, config,
	)
	if err != nil {
		config.observer.fail("trace_persistence")
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	terminal := evidenceAuditTraceTerminal{
		Version: evidenceAuditTraceTerminalVersion, AuditID: audit.AuditID,
		InputHash: audit.InputHash, TraceID: traceID, ReportFingerprint: fingerprint,
		Report: preparedReport, Trace: trace,
	}
	if err := store.prepareEvidenceAuditTraceTerminal(terminal); err != nil {
		config.observer.fail("trace_persistence")
		return nil, failEvidenceAuditRun(
			store, audit, pkg, retrieved, traceID, config,
			fmt.Errorf("prepare evidence audit terminal: %w", err),
		)
	}
	if err := ctx.Err(); err != nil {
		config.observer.fail("trace_persistence")
		_ = store.removeEvidenceAuditTraceTerminal(audit.AuditID)
		return nil, failEvidenceAuditRun(store, audit, pkg, retrieved, traceID, config, err)
	}
	if err := store.finalizeEvidenceAuditTraceTerminal(terminal); err != nil {
		config.observer.fail("trace_persistence")
		if _, loadErr := store.LoadAgentTrace(traceID); loadErr == nil {
			return nil, fmt.Errorf("finalize evidence audit trace: %w", err)
		}
		_ = store.removeEvidenceAuditTraceTerminal(audit.AuditID)
		return nil, failEvidenceAuditRun(
			store, audit, pkg, retrieved, traceID, config,
			fmt.Errorf("finalize evidence audit trace: %w", err),
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	completed, err := PublishEvidenceAuditCompletion(store, audit.AuditID)
	if err != nil {
		return nil, err
	}
	if err := store.removeEvidenceAuditTraceTerminal(audit.AuditID); err != nil {
		return nil, err
	}
	return completed, nil
}

func failEvidenceAuditRun(
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg any,
	retrieved []evidenceAuditRetrievedItem,
	traceID string,
	config EvidenceAuditRunnerConfig,
	runErr error,
) error {
	var packageValue AgentPackage
	if concrete, ok := pkg.(AgentPackage); ok {
		packageValue = concrete
	}
	if concrete, ok := pkg.(*AgentPackage); ok && concrete != nil {
		packageValue = *concrete
	}
	failureCode := evidenceAuditFailureCode(runErr)
	failureSummary := "Evidence audit failed closed."
	fingerprint := sha256Fingerprint([]byte(failureCode))
	if config.observer != nil {
		config.observer.begin("trace_persistence")
	}
	trace, traceErr := buildEvidenceAuditTrace(
		store, audit, packageValue, retrieved, nil, traceID, AgentTraceOutcomeFailed, fingerprint, config,
	)
	if traceErr != nil {
		return fmt.Errorf("%v; build failed trace: %w", runErr, traceErr)
	}
	terminal := evidenceAuditTraceTerminal{
		Version: evidenceAuditTraceTerminalVersion, AuditID: audit.AuditID,
		InputHash: audit.InputHash, TraceID: traceID, ReportFingerprint: fingerprint,
		FailureCode: failureCode, FailureSummary: failureSummary, Trace: trace,
	}
	if traceErr := store.prepareEvidenceAuditTraceTerminal(terminal); traceErr != nil {
		if config.observer != nil {
			config.observer.fail("trace_persistence")
		}
		return fmt.Errorf("%v; prepare failed trace: %w", runErr, traceErr)
	}
	if traceErr := store.finalizeEvidenceAuditTraceTerminal(terminal); traceErr != nil {
		if config.observer != nil {
			config.observer.fail("trace_persistence")
		}
		return fmt.Errorf("%v; finalize failed trace: %w", runErr, traceErr)
	}
	_, failErr := FailEvidenceAudit(
		store, audit.AuditID, failureCode, failureSummary, evidenceAuditRunnerNow(config),
	)
	if failErr != nil {
		return fmt.Errorf("%v; persist failed audit: %w", runErr, failErr)
	}
	if traceErr := store.removeEvidenceAuditTraceTerminal(audit.AuditID); traceErr != nil {
		return fmt.Errorf("%v; remove failed terminal: %w", runErr, traceErr)
	}
	return runErr
}

func recoverEvidenceAuditTerminal(
	ctx context.Context,
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	config EvidenceAuditRunnerConfig,
) (*EvidenceAudit, bool, error) {
	terminal, err := store.loadEvidenceAuditTraceTerminal(audit.AuditID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("load prepared evidence audit terminal: %w", err)
	}
	if terminal.InputHash != audit.InputHash || terminal.TraceID != audit.TraceID {
		return nil, true, fmt.Errorf("prepared evidence audit terminal does not match running audit")
	}
	if err := evidenceAuditRunnerStage(ctx, "persist"); err != nil {
		return nil, true, err
	}
	switch terminal.Trace.Final.Outcome {
	case AgentTraceOutcomeCompleted, AgentTraceOutcomeAbstained:
		if audit.Status != EvidenceAuditRunning {
			return nil, true, fmt.Errorf(
				"prepared successful terminal cannot recover audit in %q", audit.Status,
			)
		}
		if err := store.finalizeEvidenceAuditTraceTerminal(*terminal); err != nil {
			return nil, true, fmt.Errorf("recover evidence audit trace: %w", err)
		}
		if _, err := PublishEvidenceAuditCompletion(store, audit.AuditID); err != nil {
			return nil, true, fmt.Errorf("recover completed evidence audit: %w", err)
		}
	case AgentTraceOutcomeFailed:
		if audit.Status != EvidenceAuditQueued && audit.Status != EvidenceAuditRunning {
			return nil, true, fmt.Errorf(
				"prepared failed terminal cannot recover audit in %q", audit.Status,
			)
		}
		if err := store.finalizeEvidenceAuditTraceTerminal(*terminal); err != nil {
			return nil, true, fmt.Errorf("recover evidence audit trace: %w", err)
		}
		if _, err := FailEvidenceAudit(
			store, audit.AuditID, terminal.FailureCode, terminal.FailureSummary,
			evidenceAuditRunnerNow(config),
		); err != nil {
			return nil, true, fmt.Errorf("recover failed evidence audit: %w", err)
		}
	default:
		return nil, true, fmt.Errorf("unsupported prepared terminal outcome")
	}
	if err := store.removeEvidenceAuditTraceTerminal(audit.AuditID); err != nil {
		return nil, true, fmt.Errorf("remove recovered evidence audit terminal: %w", err)
	}
	recovered, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		return nil, true, err
	}
	if recovered.Status == EvidenceAuditFailed {
		return nil, true, fmt.Errorf(
			"evidence audit %q failed closed: %s", recovered.AuditID, recovered.FailureCode,
		)
	}
	return recovered, true, nil
}

func evidenceAuditFailureCode(err error) string {
	if errors.Is(err, ErrEvidenceAuditModelOutcomeUnknown) {
		return "model_outcome_unknown"
	}
	if errors.Is(err, context.Canceled) {
		return "runner_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "runner_timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "content hash changed"), strings.Contains(message, "no longer matches"):
		return "release_changed"
	case strings.Contains(message, "json"), strings.Contains(message, "candidate verdict"):
		return "invalid_model_output"
	case strings.Contains(message, "citation"):
		return "unresolved_citation"
	default:
		return "runner_failed"
	}
}

func buildEvidenceAuditTrace(
	store *BookKnowledgeStore,
	audit EvidenceAudit,
	pkg AgentPackage,
	retrieved []evidenceAuditRetrievedItem,
	claims []EvidenceAuditClaim,
	traceID, outcome, responseFingerprint string,
	config EvidenceAuditRunnerConfig,
) (AgentTrace, error) {
	if config.observer == nil {
		config.observer = newEvidenceAuditObserver(config)
	}
	if len(claims) > 0 {
		config.observer.publications = map[string]bool{}
		for _, claim := range claims {
			for _, evidence := range claim.Evidence {
				if evidence.Role == EvidenceAuditReleaseSupporting {
					config.observer.publications[evidence.PublicationIdentity] = true
				}
			}
		}
	}
	releases := make([]AgentTraceReleaseRef, 0, len(audit.Releases))
	for _, release := range audit.Releases {
		version := "unknown"
		if loaded, err := store.LoadKnowledgeRelease(release.ReleaseID); err == nil && strings.TrimSpace(loaded.Version) != "" {
			version = loaded.Version
		}
		releases = append(releases, AgentTraceReleaseRef{
			ReleaseID: release.ReleaseID, Version: version, ContentHash: release.ContentHash,
		})
	}
	retrievals := make([]AgentTraceRetrieval, 0, len(retrieved))
	evidenceIDs := make(map[string]string, len(retrieved))
	seenEvidence := map[string]bool{}
	for _, item := range retrieved {
		evidenceID := agentRuntimeEvidenceID(item.Evidence) + ":" + item.Ref.CitationID
		evidenceIDs[item.Ref.ReleaseID+"\x00"+item.Ref.ClaimID+"\x00"+item.Ref.CitationID] = evidenceID
		if seenEvidence[evidenceID] {
			continue
		}
		seenEvidence[evidenceID] = true
		retrievals = append(retrievals, AgentTraceRetrieval{
			EvidenceID: evidenceID, ReleaseID: item.Ref.ReleaseID,
			Score: item.Evidence.Score, Rank: len(retrievals) + 1,
		})
	}
	citations := make([]AgentTraceCitation, 0)
	seenCitations := map[string]bool{}
	if outcome == AgentTraceOutcomeCompleted {
		for _, claim := range claims {
			for _, ref := range claim.Evidence {
				key := ref.ReleaseID + "\x00" + ref.ClaimID + "\x00" + ref.CitationID
				evidenceID := evidenceIDs[key]
				if evidenceID == "" {
					return AgentTrace{}, fmt.Errorf(
						"report citation %q in release %q is outside retrieved evidence",
						ref.CitationID, ref.ReleaseID,
					)
				}
				if seenCitations[key] {
					continue
				}
				seenCitations[key] = true
				citations = append(citations, AgentTraceCitation{
					CitationID: ref.CitationID, ReleaseID: ref.ReleaseID, EvidenceID: evidenceID,
					PublishedAt: ref.PublishedAt, FreshnessDecision: ref.FreshnessDecision,
				})
			}
		}
	}
	strategy := audit.Retrieval.Strategy
	route := AgentTraceRetrievalRoute{Strategy: strategy}
	if strategy == "vector" || strategy == "hybrid" {
		route.EmbeddingIdentity = agentPackageSemanticEmbedderIdentity(pkg.RetrievalPolicy)
		route.RerankerVersion = audit.Retrieval.RerankerVersion
	}
	model := audit.Model.Model
	capability := "evidence-audit"
	if strings.TrimSpace(pkg.ModelPolicy.PreferredCapability) != "" {
		capability = pkg.ModelPolicy.PreferredCapability
	}
	now := evidenceAuditRunnerNow(config)
	startedAt := now
	if parsed, err := time.Parse(time.RFC3339Nano, audit.StartedAt); err == nil {
		startedAt = parsed
	}
	return AgentTrace{
		SchemaVersion: AgentTraceSchemaVersion,
		TraceID:       traceID,
		Package: AgentTracePackageRef{
			PackageID: audit.Package.PackageID, Version: audit.Package.Version,
			ContentHash: audit.Package.ContentHash,
		},
		EvidenceAudit:  &AgentTraceEvidenceAuditRef{AuditID: audit.AuditID, InputHash: audit.InputHash},
		Releases:       releases,
		RetrievalRoute: route,
		Retrievals:     retrievals,
		ModelRoute: AgentTraceModelRoute{
			Provider: audit.Model.Provider, Model: model, Capability: capability,
		},
		ToolCalls: []AgentTraceToolCall{},
		Final: AgentTraceFinal{
			Outcome: outcome, ResponseFingerprint: responseFingerprint, Citations: citations,
		},
		StartedAt:     startedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:   now.UTC().Format(time.RFC3339Nano),
		Observability: config.observer.snapshot(outcome),
	}, nil
}

func evidenceAuditReportFingerprint(report EvidenceAudit) (string, error) {
	payload, err := json.Marshal(struct {
		Claims    []EvidenceAuditClaim             `json:"claims"`
		Summary   EvidenceAuditSummary             `json:"summary"`
		Proofroom EvidenceAuditProofroomProjection `json:"proofroom"`
	}{report.ClaimAudits, report.Summary, report.Proofroom})
	if err != nil {
		return "", err
	}
	return sha256Fingerprint(payload), nil
}

func evidenceAuditRequestsMedicalAdvice(value string) bool {
	raw := strings.TrimSpace(value)
	value = strings.ToLower(raw)
	if raw == "" {
		return true
	}
	hasAny := func(markers ...string) bool {
		for _, marker := range markers {
			if strings.Contains(value, marker) {
				return true
			}
		}
		return false
	}
	paddedValue := " " + value + " "
	firstPersonContext := strings.Contains(paddedValue, " i ") ||
		strings.Contains(paddedValue, " me ") ||
		strings.Contains(paddedValue, " my ") || hasAny(
		"for me", "should i", "can i", "could i", "i need", "i want",
		"right for me", "appropriate for me",
		"我", "我的", "给我", "帮我", "本人", "家人", "孩子", "老人", "适合我",
	)
	specificPatientContext := hasAny(
		"this patient", "the patient", "this person", "case ", "year-old",
		"病例", "个案", "这个患者", "该患者", "这名患者",
	) || (strings.Contains(value, "patient ") &&
		!strings.Contains(value, "patient population") &&
		!strings.Contains(value, "patients ")) ||
		(strings.Contains(value, "患者") &&
			!strings.Contains(value, "成年患者") &&
			!strings.Contains(value, "儿童患者") &&
			!strings.Contains(value, "患者人群") &&
			!strings.Contains(value, "患者中"))
	ageOrNamedPersonContext := evidenceAuditEnglishAgePattern.MatchString(value) ||
		evidenceAuditChineseAgePattern.MatchString(value) ||
		evidenceAuditDecisionHasNonPopulationSubject(value) ||
		evidenceAuditEnglishNamedDecisionPattern.MatchString(raw) ||
		evidenceAuditChineseIndividualDecisionPattern.MatchString(raw)
	personalContext := firstPersonContext || specificPatientContext || ageOrNamedPersonContext
	diagnosisOrTreatment := hasAny(
		"diagnos", "treat", "therapy for", "prescri", "medical advice", "what do i have",
		"do i have", "cure me", "诊断", "治疗", "处方", "看病", "治好", "是什么病",
	)
	medicationContext := hasAny(
		"medicine", "medication", "drug", "aspirin", "tablet", "pill", "dose", "dosage",
		"药", "阿司匹林", "剂量", "用量", "服用", "吃",
	)
	clinicalContext := diagnosisOrTreatment || medicationContext || hasAny(
		"symptom", "rash", "pain", "fever", "cancer", "melanoma", "chemotherapy",
		"surgery", "operation", "procedure", "screening", "medical test", " test ", "therapy",
		"头痛", "发热", "皮疹", "肿瘤", "癌", "化疗", "症状", "手术", "检查", "治疗",
	)
	decisionRequest := hasAny(
		"should", "can i", "could i", "would it be better", "what dose", "how much",
		"could this", "is chemotherapy right", "stop", "start", "switch", "substitute",
		"replace", "increase", "decrease", "take",
		"need", "want", "undergo", "receive",
		"是否", "要不要", "该不该", "能不能", "可不可以", "怎么", "停", "换",
		"替代", "加量", "减量", "开始", "继续",
	)
	explicitAdvice := hasAny(
		"recommend a treatment", "individual treatment", "individual medical advice",
		"用药建议", "治疗方案", "开药", "给我诊断", "诊断我",
	)
	explicitAcademicPICO := evidenceAuditEnglishPICOPattern.MatchString(raw) ||
		evidenceAuditIsChinesePICO(raw)
	strongPersonalContext := firstPersonContext || ageOrNamedPersonContext || hasAny(
		"this patient", "the patient", "this person", "case ",
		"病例", "个案", "这个患者", "该患者", "这名患者",
	)
	if strongPersonalContext && (clinicalContext || decisionRequest || explicitAdvice) {
		return true
	}
	if personalContext && (decisionRequest || explicitAdvice) {
		return true
	}
	if medicationContext && decisionRequest {
		return true
	}
	if explicitAcademicPICO {
		return false
	}
	return false
}

func evidenceAuditIsChinesePICO(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "在") {
		return false
	}
	hasPopulation := strings.Contains(value, "患者中") ||
		strings.Contains(value, "人群中") ||
		strings.Contains(value, "受试者中")
	hasComparator := strings.Contains(value, "相比") || strings.Contains(value, "对比")
	hasOutcome := strings.Contains(value, "是否改善") ||
		strings.Contains(value, "是否降低") ||
		strings.Contains(value, "是否提高") ||
		strings.Contains(value, "是否减少") ||
		strings.Contains(value, "是否预防")
	return hasPopulation && hasComparator && hasOutcome
}

func evidenceAuditDecisionHasNonPopulationSubject(value string) bool {
	populationSubjects := map[string]bool{
		"patients": true, "adults": true, "children": true, "participants": true,
		"subjects": true, "people": true, "persons": true, "cohorts": true,
		"groups": true, "populations": true,
		"surgery": true, "screening": true, "treatment": true, "therapy": true,
		"procedure": true, "test": true, "drug": true, "medication": true,
	}
	for _, match := range evidenceAuditDecisionSubjectPattern.FindAllStringSubmatch(value, -1) {
		if len(match) == 2 && !populationSubjects[strings.ToLower(match[1])] {
			return true
		}
	}
	for _, marker := range []string{"是否应该", "是否应当", "要不要", "该不该", "能不能", "可不可以"} {
		index := strings.Index(value, marker)
		if index < 0 {
			continue
		}
		subject := strings.TrimSpace(value[:index])
		if separator := strings.LastIndexAny(subject, ":：,，。！？?;；"); separator >= 0 {
			subject = strings.TrimSpace(subject[separator+1:])
		}
		if subject == "" || !evidenceAuditHasExplicitPopulationSubject(subject) {
			return true
		}
	}
	return false
}

func evidenceAuditHasExplicitPopulationSubject(value string) bool {
	for _, marker := range []string{
		"患者群体", "患者人群", "成年患者", "儿童患者", "受试者", "研究人群",
		"队列人群", "成年人群", "儿童群体",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func evidenceAuditRunnerStage(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := evidenceAuditRuntimeStageHook(ctx, stage); err != nil {
		return err
	}
	return ctx.Err()
}

func evidenceAuditRunnerNow(config EvidenceAuditRunnerConfig) time.Time {
	if config.Now != nil {
		return config.Now().UTC()
	}
	return time.Now().UTC()
}
