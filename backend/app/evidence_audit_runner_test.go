package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvidenceAuditRunnerSelectsPrimaryClaimsAndQueriesEverySupportingRelease(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	input, err := PrepareEvidenceAuditInput(
		store, pkg.PackageID, pkg.Version, "Audit the book claims", "Evidence comparison only.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := input.SelectedClaims, []string{"Synthetic grounded statement", "Primary claim two"}; !sameStringSet(got, want) {
		t.Fatalf("selected claims = %#v, want %#v", got, want)
	}
	audit, _, err := CreateEvidenceAudit(store, input, "runner-selection", testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},{"release_id":"support-b","citation_id":"support-b-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		`{"candidate_verdict":"contradicted","rationale":"conflict","evidence":[{"release_id":"support-a","citation_id":"support-a-c2","stance":"contradicts"},{"release_id":"support-b","citation_id":"support-b-c2","stance":"contradicts"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != EvidenceAuditCompleted || len(completed.ClaimAudits) != 2 {
		t.Fatalf("completed audit = %#v", completed)
	}
	if got := []string{completed.ClaimAudits[0].Verdict, completed.ClaimAudits[1].Verdict}; !sameStringSet(got, []string{
		EvidenceAuditVerdictSupported, EvidenceAuditVerdictContradicted,
	}) {
		t.Fatalf("verdicts = %#v", got)
	}
	for _, messages := range client.calls {
		prompt := messages[len(messages)-1].Content
		for _, releaseID := range []string{"support-a", "support-b"} {
			if !strings.Contains(prompt, releaseID) {
				t.Fatalf("prompt did not query every supporting release: %s", prompt)
			}
		}
		if strings.Contains(prompt, "private-body-marker") {
			t.Fatalf("model prompt leaked source body: %s", prompt)
		}
	}
}

func TestEvidenceAuditRunnerComputesAllVerdictsAndDeduplicatesEvidence(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		verdict string
		count   int
	}{
		{
			name: "supported",
			answer: `{"candidate_verdict":"contradicted","rationale":"candidate is advisory only","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"supports"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictSupported,
			count:   2,
		},
		{
			name: "contradicted",
			answer: `{"candidate_verdict":"supported","rationale":"candidate is advisory only","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"contradicts"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"contradicts"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictContradicted,
			count:   2,
		},
		{
			name: "mixed",
			answer: `{"candidate_verdict":"mixed","rationale":"mixed","evidence":[` +
				`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"},` +
				`{"release_id":"support-b","citation_id":"support-b-c1","stance":"contradicts"}],` +
				`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictMixed,
			count:   2,
		},
		{
			name: "insufficient",
			answer: `{"candidate_verdict":"supported","rationale":"none","evidence":[],` +
				`"limitations":["No eligible evidence."],"knowledge_gaps":[],"review_actions":[]}`,
			verdict: EvidenceAuditVerdictInsufficient,
			count:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-verdict-"+tt.name, "Evidence comparison only.")
			completed, err := RunEvidenceAudit(
				context.Background(), store, audit.AuditID,
				&evidenceAuditFakeClient{answers: []string{tt.answer}}, evidenceAuditRunnerConfig(),
			)
			if err != nil {
				t.Fatal(err)
			}
			claim := completed.ClaimAudits[0]
			if claim.Verdict != tt.verdict || len(claim.Evidence) != tt.count {
				t.Fatalf("claim audit = %#v", claim)
			}
			if claim.ComputedConfidence != ComputeEvidenceAuditConfidence(claim.Evidence, evidenceConflictCount(claim.Evidence)) {
				t.Fatalf("confidence was not computed in code: %#v", claim)
			}
		})
	}
}

func TestEvidenceAuditRunnerDowngradesWithoutIndependentSupportingSources(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 2)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-downgrade", "Evidence comparison only.")
	answer := `{"candidate_verdict":"supported","rationale":"one publication","evidence":[` +
		`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],` +
		`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID,
		&evidenceAuditFakeClient{answers: []string{answer}}, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := completed.ClaimAudits[0]
	if claim.Verdict != EvidenceAuditVerdictInsufficient || claim.ComputedConfidence != 0 {
		t.Fatalf("independent-source downgrade = %#v", claim)
	}
}

func TestEvidenceAuditRunnerGroupsAndCapsSupportingEvidenceAcrossReleases(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStoreWithLimits(t, 1, 1, 1)
	loaded, releases, err := loadEvidenceAuditPackageSnapshot(store, pkg.PackageID, pkg.Version)
	if err != nil {
		t.Fatal(err)
	}
	retrieved, err := retrieveEvidenceAuditSupportingEvidence(
		context.Background(), store, loaded, releases, "Synthetic grounded statement",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(retrieved) != 1 {
		t.Fatalf("retrieved evidence = %d, want package-wide cap 1: %#v", len(retrieved), retrieved)
	}
}

func TestEvidenceAuditRunnerFailsClosedAndPersistsFailedTrace(t *testing.T) {
	tests := []struct {
		name   string
		client BookKnowledgeLLMClient
		edit   func(*BookKnowledgeStore, AgentPackage, *EvidenceAudit)
	}{
		{name: "invalid json", client: &evidenceAuditFakeClient{answers: []string{`not-json`}}},
		{name: "unresolved citation", client: &evidenceAuditFakeClient{answers: []string{
			`{"candidate_verdict":"supported","rationale":"bad citation","evidence":[{"release_id":"support-a","citation_id":"missing","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		}}},
		{name: "model failure", client: &evidenceAuditFakeClient{err: errors.New("synthetic model failure")}},
		{name: "timeout", client: evidenceAuditBlockingClient{}},
		{
			name:   "release hash changed",
			client: &evidenceAuditFakeClient{answers: []string{`{}`}},
			edit: func(store *BookKnowledgeStore, _ AgentPackage, _ *EvidenceAudit) {
				path := store.KnowledgeReleasePath("support-a")
				payload, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				changed := strings.Replace(string(payload), strings.Repeat("a", 64), strings.Repeat("f", 64), 1)
				if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-failure-"+tt.name, "Evidence comparison only.")
			if tt.edit != nil {
				tt.edit(store, pkg, audit)
			}
			cfg := evidenceAuditRunnerConfig()
			cfg.Timeout = 10 * time.Millisecond
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, tt.client, cfg); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			failed, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil || failed.Status != EvidenceAuditFailed || failed.TraceID == "" {
				t.Fatalf("failed audit = %#v err=%v", failed, err)
			}
			trace, err := store.LoadAgentTrace(failed.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeFailed {
				t.Fatalf("failed trace = %#v err=%v", trace, err)
			}
		})
	}
}

func TestEvidenceAuditRunnerSafelyAbstainsFromMedicalAdvice(t *testing.T) {
	for _, scope := range []string{
		"Diagnose me and recommend an individual treatment.",
		"Evidence audit: Should I undergo surgery?",
		"Evidence audit: Should this patient undergo surgery?",
		"Clinical trial evidence: Should patient John undergo surgery?",
		"Review the evidence: should this 52-year-old person start treatment?",
		"Clinical trial evidence: Can I have this procedure?",
		"Would this test be appropriate for me?",
		"Is this therapy right for me?",
		"Should I stop aspirin?",
		"What dose should I take?",
		"Could another medicine work better for me?",
		"Could this rash be melanoma?",
		"Is chemotherapy right for my case?",
		"我是否应该停药？",
		"能不能给我换药？",
		"这个药要不要加量？",
		"阿司匹林怎么吃",
		"我的头痛是什么病？",
		"证据审计：我是否应该做手术？",
		"证据审计：患者张三是否应该做手术？",
		"文献综述：这个 62 岁病例要不要接受治疗？",
		"临床试验证据：这个检查适合我吗？",
		"我能不能接受这个治疗？",
	} {
		t.Run(scope, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(
				t, store, pkg, "runner-medical-advice-"+sha256Fingerprint([]byte(scope)), scope,
			)
			client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
			completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			if err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 0 || completed.ClaimAudits[0].Verdict != EvidenceAuditVerdictInsufficient {
				t.Fatalf("unsafe advice path = %#v calls=%d", completed, len(client.calls))
			}
			trace, err := store.LoadAgentTrace(completed.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeAbstained {
				t.Fatalf("abstained trace = %#v err=%v", trace, err)
			}
		})
	}
}

func TestEvidenceAuditRunnerAllowsPopulationEvidenceQuestions(t *testing.T) {
	for _, scope := range []string{
		"Population-level PICO: does surgery improve five-year survival in adults?",
		"Compare evidence for screening tests in adults aged 50 to 75.",
		"群体级PICO：手术是否改善成年患者五年生存率？",
	} {
		t.Run(scope, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(
				t, store, pkg, "runner-population-evidence-"+sha256Fingerprint([]byte(scope)), scope,
			)
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"population evidence","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			}}
			completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			if err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 || completed.ClaimAudits[0].Verdict == EvidenceAuditVerdictInsufficient {
				t.Fatalf("population evidence path = %#v calls=%d", completed, len(client.calls))
			}
		})
	}
}

func TestEvidenceAuditRunnerFailsClosedWhenLaterClaimModelOutcomeIsUnknown(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-claim-checkpoint", "Evidence comparison only.")
	first := &evidenceAuditSequenceClient{
		answers: []string{
			`{"candidate_verdict":"supported","rationale":"first","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
		},
		blockAfterAnswers: true,
	}
	cfg := evidenceAuditRunnerConfig()
	cfg.Timeout = 2 * time.Second
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, cfg); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != EvidenceAuditRunning {
		t.Fatalf("audit status after resumable interruption = %q", running.Status)
	}

	second := &evidenceAuditFakeClient{answers: []string{
		`must not be called`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig()); err == nil ||
		!strings.Contains(err.Error(), "model_outcome_unknown") {
		t.Fatalf("resume error = %v, want model_outcome_unknown", err)
	}
	failed, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || failed.Status != EvidenceAuditFailed ||
		failed.FailureCode != "model_outcome_unknown" ||
		len(first.calls) != 2 || len(second.calls) != 0 {
		t.Fatalf("failed=%#v err=%v first_calls=%d second_calls=%d", failed, err, len(first.calls), len(second.calls))
	}
}

func TestEvidenceAuditRunnerRecoversCandidateWhenCheckpointFollowupFails(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-followup", "Evidence comparison only.")
	previous := evidenceAuditRuntimeStageHook
	failed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "checkpoint" && !failed {
			failed = true
			return errors.New("synthetic checkpoint followup failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
	first := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"saved candidate","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || running.Status != EvidenceAuditRunning {
		t.Fatalf("running audit = %#v err=%v", running, err)
	}
	second := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	completed, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != EvidenceAuditCompleted || len(first.calls) != 1 || len(second.calls) != 0 {
		t.Fatalf("checkpoint recovery completed=%#v first_calls=%d second_calls=%d", completed, len(first.calls), len(second.calls))
	}
}

func TestEvidenceAuditRunnerCheckpointDoesNotPersistPromptOrSourceBody(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-checkpoint-privacy", "Evidence comparison only.")
	privateMarker := "private-source-body-must-not-persist"
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"` + privateMarker + `","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":["` + privateMarker + `"],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err != nil {
		t.Fatal(err)
	}
	checkpointRoot := store.evidenceAuditExecutionDir(audit.AuditID)
	err := filepath.WalkDir(checkpointRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(payload), privateMarker) ||
			strings.Contains(string(payload), "Pinned supporting evidence") {
			t.Fatalf("checkpoint leaked private model or prompt content: %s", payload)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceAuditRunnerDoesNotRetryUnknownInFlightModelOutcome(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-model-outcome-unknown", "Evidence comparison only.")
	first := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"returned but not persisted","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	previous := evidenceAuditRuntimeStageHook
	crashed := false
	evidenceAuditRuntimeStageHook = func(_ context.Context, stage string) error {
		if stage == "model_completed_before_checkpoint" && !crashed {
			crashed = true
			return errors.New("synthetic process crash after model response")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })

	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, first, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("first run unexpectedly succeeded")
	}
	running, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || running.Status != EvidenceAuditRunning {
		t.Fatalf("audit after simulated crash = %#v err=%v", running, err)
	}

	evidenceAuditRuntimeStageHook = previous
	second := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, second, evidenceAuditRunnerConfig()); err == nil ||
		!strings.Contains(err.Error(), "model_outcome_unknown") {
		t.Fatalf("resume error = %v, want model_outcome_unknown", err)
	}
	failed, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil || failed.Status != EvidenceAuditFailed ||
		failed.FailureCode != "model_outcome_unknown" {
		t.Fatalf("failed audit = %#v err=%v", failed, err)
	}
	if len(first.calls) != 1 || len(second.calls) != 0 {
		t.Fatalf("model calls first=%d second=%d", len(first.calls), len(second.calls))
	}
}

func TestEvidenceAuditRunnerSerializesConcurrentExecutionWithoutDuplicateModelCalls(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-concurrent-checkpoint", "Evidence comparison only.")
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"one call","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			defer wait.Done()
			_, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent run failed: %v", err)
		}
	}
	if len(client.calls) != 1 {
		t.Fatalf("concurrent execution called model %d times", len(client.calls))
	}
}

func TestEvidenceAuditRunnerTraceFinalCitationsMatchOnlyReportEvidence(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-selected-citations", "Evidence comparison only.")
	answer := `{"candidate_verdict":"supported","rationale":"one selected item","evidence":[` +
		`{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],` +
		`"limitations":[],"knowledge_gaps":[],"review_actions":[]}`
	completed, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID,
		&evidenceAuditFakeClient{answers: []string{answer}}, evidenceAuditRunnerConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.LoadAgentTrace(completed.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Retrievals) < 2 {
		t.Fatalf("retrieval trace lost unselected hits: %#v", trace.Retrievals)
	}
	if len(trace.Final.Citations) != 1 ||
		trace.Final.Citations[0].CitationID != completed.ClaimAudits[0].Evidence[0].CitationID {
		t.Fatalf("final citations = %#v, report evidence = %#v", trace.Final.Citations, completed.ClaimAudits[0].Evidence)
	}
}

func TestEvidenceAuditRunnerRecoversTerminalProtocolFailures(t *testing.T) {
	tests := []struct {
		name        string
		installFail func(t *testing.T)
		firstStatus string
		traceExists bool
	}{
		{
			name:        "report store failure",
			firstStatus: EvidenceAuditCompleted,
			traceExists: true,
			installFail: func(t *testing.T) {
				previous := evidenceAuditStorageFault
				manifestWrites := 0
				evidenceAuditStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditFaultManifestBeforePublish {
						manifestWrites++
						if manifestWrites == 2 {
							return errors.New("synthetic report manifest failure")
						}
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditStorageFault = previous })
			},
		},
		{
			name:        "trace save failure",
			firstStatus: EvidenceAuditRunning,
			installFail: func(t *testing.T) {
				previous := evidenceAuditTraceStorageFault
				failed := false
				evidenceAuditTraceStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditTraceFaultBeforeSave && !failed {
						failed = true
						return errors.New("synthetic trace save failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
			},
		},
		{
			name:        "trace finalize failure",
			firstStatus: EvidenceAuditCompleted,
			traceExists: true,
			installFail: func(t *testing.T) {
				previous := evidenceAuditTraceStorageFault
				failed := false
				evidenceAuditTraceStorageFault = func(stage, _ string) error {
					if stage == evidenceAuditTraceFaultBeforeFinalize && !failed {
						failed = true
						return errors.New("synthetic trace finalize failure")
					}
					return nil
				}
				t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-recovery-"+tt.name, "Evidence comparison only.")
			client := &evidenceAuditFakeClient{answers: []string{
				`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
			}}
			tt.installFail(t)
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err == nil {
				t.Fatal("first run unexpectedly succeeded")
			}
			first, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil {
				t.Fatal(err)
			}
			if first.Status != tt.firstStatus {
				t.Fatalf("audit status after injected failure = %q, want %q", first.Status, tt.firstStatus)
			}
			_, traceErr := store.LoadAgentTrace(first.TraceID)
			if (traceErr == nil) != tt.traceExists {
				t.Fatalf("trace existence after injected failure = %v, want %v", traceErr == nil, tt.traceExists)
			}
			recovered, err := store.LoadEvidenceAudit(audit.AuditID)
			if err == nil && recovered.Status != EvidenceAuditCompleted {
				recovered, err = RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig())
			}
			if err != nil {
				t.Fatalf("recovery run failed: %v", err)
			}
			if recovered.Status != EvidenceAuditCompleted {
				t.Fatalf("recovered audit = %#v", recovered)
			}
			trace, err := store.LoadAgentTrace(recovered.TraceID)
			if err != nil || trace.Final.Outcome != AgentTraceOutcomeCompleted {
				t.Fatalf("recovered trace = %#v err=%v", trace, err)
			}
			if len(client.calls) != 1 {
				t.Fatalf("recovery reran model %d times", len(client.calls))
			}
		})
	}
}

func TestEvidenceAuditRunnerFailsClosedWhenTracePreparationCannotPersist(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-trace-prepare-failure", "Evidence comparison only.")
	previous := evidenceAuditTraceStorageFault
	evidenceAuditTraceStorageFault = func(stage, _ string) error {
		if stage == evidenceAuditTraceFaultBeforePrepare {
			return errors.New("synthetic trace prepare failure")
		}
		return nil
	}
	t.Cleanup(func() { evidenceAuditTraceStorageFault = previous })
	client := &evidenceAuditFakeClient{answers: []string{
		`{"candidate_verdict":"supported","rationale":"support","evidence":[{"release_id":"support-a","citation_id":"support-a-c1","stance":"supports"}],"limitations":[],"knowledge_gaps":[],"review_actions":[]}`,
	}}
	if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig()); err == nil {
		t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
	}
	stored, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == EvidenceAuditCompleted {
		t.Fatalf("audit completed without a recoverable trace: %#v", stored)
	}
}

func TestEvidenceAuditRunnerReadSideReconcilesPreparedFailure(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-failed-read-reconcile", "Evidence comparison only.")
	if _, err := StartEvidenceAudit(
		store, audit.AuditID, "trace-failed-read-reconcile",
		time.Date(2026, 7, 19, 17, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
	originalWriter := writeEvidenceAuditManifestFile
	failedOnce := false
	writeEvidenceAuditManifestFile = func(path string, payload []byte) error {
		if !failedOnce {
			failedOnce = true
			return errors.New("synthetic failed terminal publish interruption")
		}
		return originalWriter(path, payload)
	}
	t.Cleanup(func() { writeEvidenceAuditManifestFile = originalWriter })
	client := &evidenceAuditFakeClient{answers: []string{`not-json`}}
	if _, err := RunEvidenceAudit(
		context.Background(), store, audit.AuditID, client, evidenceAuditRunnerConfig(),
	); err == nil {
		t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
	}
	writeEvidenceAuditManifestFile = originalWriter
	reconciled, err := store.LoadEvidenceAudit(audit.AuditID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Status != EvidenceAuditFailed ||
		reconciled.FailureCode != "invalid_model_output" ||
		len(client.calls) != 1 {
		t.Fatalf("reconciled failed audit = %#v calls=%d", reconciled, len(client.calls))
	}
	if _, err := os.Stat(store.EvidenceAuditTraceTerminalPath(audit.AuditID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed terminal was not cleaned after read reconciliation: %v", err)
	}
}

func TestEvidenceAuditRunnerContextCoversRetrievalAndCitationResolution(t *testing.T) {
	for _, stage := range []string{"retrieval", "citation"} {
		t.Run(stage, func(t *testing.T) {
			store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
			audit := createEvidenceAuditRunnerTask(t, store, pkg, "runner-context-"+stage, "Evidence comparison only.")
			previous := evidenceAuditRuntimeStageHook
			evidenceAuditRuntimeStageHook = func(ctx context.Context, current string) error {
				if current != stage {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			}
			t.Cleanup(func() { evidenceAuditRuntimeStageHook = previous })
			cfg := evidenceAuditRunnerConfig()
			cfg.Timeout = 10 * time.Millisecond
			client := &evidenceAuditFakeClient{answers: []string{`must not be called`}}
			if _, err := RunEvidenceAudit(context.Background(), store, audit.AuditID, client, cfg); err == nil {
				t.Fatal("RunEvidenceAudit() unexpectedly succeeded")
			}
			if len(client.calls) != 0 {
				t.Fatalf("model called after %s timeout", stage)
			}
			failed, err := store.LoadEvidenceAudit(audit.AuditID)
			if err != nil || failed.Status != EvidenceAuditFailed {
				t.Fatalf("failed audit = %#v err=%v", failed, err)
			}
		})
	}
}

type evidenceAuditFakeClient struct {
	mu      sync.Mutex
	answers []string
	err     error
	calls   [][]BookKnowledgeMessage
}

func (c *evidenceAuditFakeClient) Chat(_ context.Context, _ BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, append([]BookKnowledgeMessage(nil), messages...))
	if c.err != nil {
		return "", c.err
	}
	index := len(c.calls) - 1
	if index >= len(c.answers) {
		return "", errors.New("missing fake model answer")
	}
	return c.answers[index], nil
}

type evidenceAuditBlockingClient struct{}

func (evidenceAuditBlockingClient) Chat(ctx context.Context, _ BookTokenPlanConfig, _ []BookKnowledgeMessage) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

type evidenceAuditSequenceClient struct {
	mu                sync.Mutex
	answers           []string
	blockAfterAnswers bool
	calls             [][]BookKnowledgeMessage
}

func (c *evidenceAuditSequenceClient) Chat(ctx context.Context, _ BookTokenPlanConfig, messages []BookKnowledgeMessage) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]BookKnowledgeMessage(nil), messages...))
	index := len(c.calls) - 1
	if index < len(c.answers) {
		answer := c.answers[index]
		c.mu.Unlock()
		return answer, nil
	}
	c.mu.Unlock()
	if c.blockAfterAnswers {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "", errors.New("missing fake model answer")
}

func evidenceAuditRunnerConfig() EvidenceAuditRunnerConfig {
	return EvidenceAuditRunnerConfig{
		ModelConfig: BookTokenPlanConfig{APIKey: "synthetic-test-key", BaseURL: "https://invalid.test/v1"},
		Timeout:     time.Second,
		Now:         func() time.Time { return testAgentPackageTime().Add(2 * time.Hour) },
	}
}

func createEvidenceAuditRunnerTask(
	t *testing.T,
	store *BookKnowledgeStore,
	pkg AgentPackage,
	idempotencyKey, scope string,
) *EvidenceAudit {
	t.Helper()
	input, err := PrepareEvidenceAuditInput(store, pkg.PackageID, pkg.Version, "Clinical trial evidence", scope)
	if err != nil {
		t.Fatal(err)
	}
	audit, _, err := CreateEvidenceAudit(store, input, idempotencyKey, testAgentPackageTime())
	if err != nil {
		t.Fatal(err)
	}
	return audit
}

func evidenceAuditRunnerTestStore(t *testing.T, maxClaims, minimumSources int) (*BookKnowledgeStore, AgentPackage) {
	return evidenceAuditRunnerTestStoreWithLimits(
		t, maxClaims, minimumSources, agentEvidenceMaxEvidencePerClaim,
	)
}

func evidenceAuditRunnerTestStoreWithLimits(
	t *testing.T,
	maxClaims, minimumSources, maxEvidence int,
) (*BookKnowledgeStore, AgentPackage) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentPackageTestRelease()
	primary.ReleaseID = "primary"
	primary.ContentHash = "sha256:" + strings.Repeat("1", 64)
	primary.Analysis.Claims = []BookAnalysisClaim{
		{ID: "claim-1", Statement: "Synthetic grounded statement", CitationIDs: []string{"citation-1"}},
		{ID: "primary-c2", Statement: "Primary claim two", CitationIDs: []string{"primary-citation-2"}},
		{ID: "primary-c3", Statement: "Primary claim three", CitationIDs: []string{"primary-citation-3"}},
	}
	primary.Citations = []BookKnowledgeCitation{
		{CitationID: "citation-1", BookID: primary.BookID, ChunkID: "chunk-1"},
		{CitationID: "primary-citation-2", BookID: primary.BookID, ChunkID: "primary-chunk-2"},
		{CitationID: "primary-citation-3", BookID: primary.BookID, ChunkID: "primary-chunk-3"},
	}
	if err := store.saveKnowledgeRelease(primary); err != nil {
		t.Fatal(err)
	}
	supportTypes := []string{"wechat_mp_article", "dedao_course_article"}
	refs := []AgentPackageReleaseRef{{
		ReleaseID: primary.ReleaseID, ContentHash: primary.ContentHash,
		CitationIDs: []string{"citation-1", "primary-citation-2", "primary-citation-3"},
	}}
	roles := []AgentPackageEvidenceReleaseRole{{ReleaseID: primary.ReleaseID, Role: AgentEvidenceReleasePrimary}}
	for index, releaseID := range []string{"support-a", "support-b"} {
		release := agentPackageTestRelease()
		release.ReleaseID = releaseID
		release.BookID = "book-" + releaseID
		release.Book.BookID = release.BookID
		release.Book.SourceType = supportTypes[index]
		release.Book.SourceHTML = "private-body-marker"
		release.ContentHash = "sha256:" + strings.Repeat(string(rune('a'+index)), 64)
		release.Analysis.Claims = []BookAnalysisClaim{
			{ID: releaseID + "-claim-1", Statement: "Synthetic grounded statement supporting evidence", CitationIDs: []string{releaseID + "-c1"}},
			{ID: releaseID + "-claim-2", Statement: "Primary claim two supporting evidence", CitationIDs: []string{releaseID + "-c2"}},
		}
		release.Citations = []BookKnowledgeCitation{
			{CitationID: releaseID + "-c1", BookID: release.BookID, ChunkID: releaseID + "-chunk-1"},
			{CitationID: releaseID + "-c2", BookID: release.BookID, ChunkID: releaseID + "-chunk-2"},
		}
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, AgentPackageReleaseRef{
			ReleaseID: releaseID, ContentHash: release.ContentHash,
			CitationIDs: []string{releaseID + "-c1", releaseID + "-c2"},
		})
		roles = append(roles, AgentPackageEvidenceReleaseRole{ReleaseID: releaseID, Role: AgentEvidenceReleaseSupporting})
	}
	pkg := validAgentPackageV2()
	pkg.PackageID = "book-agent-clinical-trials-truth"
	pkg.Releases = refs
	pkg.RetrievalPolicy.Strategy = "lexical"
	pkg.RetrievalPolicy.AllowedSourceTypes = []string{"dedao_ebook", "wechat_mp_article", "dedao_course_article"}
	pkg.RetrievalPolicy.MaxContextChunks = 5
	pkg.ModelPolicy.Fallbacks = []string{"qwen3.7-max"}
	pkg.EvidencePolicy.ReleaseRoles = roles
	pkg.EvidencePolicy.MaxClaims = maxClaims
	pkg.EvidencePolicy.MaxEvidencePerClaim = maxEvidence
	pkg.EvidencePolicy.MinimumIndependentSources = minimumSources
	pkg.UIManifest.Capabilities = append(pkg.UIManifest.Capabilities, "evidence")
	pkg, err := FinalizeAgentPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	published, _, err := PublishAgentPackage(
		store, pkg, "publish-evidence-runner", AgentReadOnlyToolIDs(), testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return store, *published
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	wanted := make(map[string]int, len(right))
	for _, value := range right {
		wanted[value]++
	}
	for _, value := range left {
		wanted[value]--
	}
	for _, count := range wanted {
		if count != 0 {
			return false
		}
	}
	return true
}
