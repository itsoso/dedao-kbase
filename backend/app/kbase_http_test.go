package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yann0917/dedao-gui/backend/services"
)

func TestKBaseHTTPHandlerHealthIncludesReleaseRevision(t *testing.T) {
	tests := []struct {
		name             string
		releaseRevision  string
		expectedRevision string
	}{
		{
			name:             "configured revision",
			releaseRevision:  "1234567890abcdef",
			expectedRevision: "1234567890abcdef",
		},
		{
			name:             "development default",
			expectedRevision: "development",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store:           NewBookKnowledgeStore(t.TempDir()),
				ReleaseRevision: tt.releaseRevision,
			})

			response := requestKBase(handler, http.MethodGet, "/health", "")
			if response.Code != http.StatusOK {
				t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("health Cache-Control=%q, want no-store", got)
			}

			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode health response: %v", err)
			}
			expected := map[string]any{
				"ok":       true,
				"service":  "dedao-kbase",
				"revision": tt.expectedRevision,
			}
			if !reflect.DeepEqual(payload, expected) {
				t.Fatalf("health response=%#v, want %#v", payload, expected)
			}
		})
	}
}

func TestKBaseHTTPHandlerRequiresBearerTokenForAPI(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "wrong-token")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong token = %d, want 401", resp.Code)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("status with correct token = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"42"`) {
		t.Fatalf("books response missing sample book: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerListsEmptyAgentPackagesAsArray(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "consumer-token",
	})

	response := requestKBase(handler, http.MethodGet, "/api/agent-packages?limit=1", "consumer-token")
	if response.Code != http.StatusOK {
		t.Fatalf("empty package list status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Packages []AgentPackageRecord `json:"packages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode empty package list: %v", err)
	}
	if payload.Packages == nil {
		t.Fatalf("empty package list encoded as null: %s", response.Body.String())
	}
	if len(payload.Packages) != 0 {
		t.Fatalf("empty package list = %#v", payload.Packages)
	}
}

func TestKBaseHTTPHandlerServesAuthenticatedAgentTraceWithoutPrivateFields(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	trace := agentTraceTestTrace()
	trace.PrivatePrompt = "must-not-leave-store"
	trace.SourceBodies = []string{"private source body"}
	trace.Credentials = "secret credential"
	if err := store.SaveAgentTrace(trace); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "consumer-token",
	})
	path := "/api/agent-traces/" + url.PathEscape(trace.TraceID)

	unauthorized := requestKBase(handler, http.MethodGet, path, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized trace status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	response := requestKBase(handler, http.MethodGet, path, "consumer-token")
	if response.Code != http.StatusOK {
		t.Fatalf("trace status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"trace_id":"`+trace.TraceID+`"`) ||
		strings.Contains(body, "must-not-leave-store") ||
		strings.Contains(body, "private source body") ||
		strings.Contains(body, "secret credential") {
		t.Fatalf("trace response exposed invalid content: %s", body)
	}
}

func TestKBaseHTTPHandlerResolvesCitationIdentityWithoutSourcePath(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "consumer-token",
	})
	response := requestKBase(
		handler,
		http.MethodGet,
		"/api/citations/42-citation-1?book_id=42",
		"consumer-token",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("citation status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"citation_id":"42-citation-1"`) ||
		!strings.Contains(body, `"chunk_id":"42-chunk-1"`) ||
		strings.Contains(body, "/tmp/book.html") ||
		strings.Contains(body, "source_html") ||
		strings.Contains(body, "source_account") ||
		strings.Contains(body, "source_item_key") ||
		strings.Contains(body, `"anchor"`) ||
		strings.Contains(body, `"note"`) {
		t.Fatalf("citation response is not a safe exact locator: %s", body)
	}
}

func TestKBaseHTTPHandlerPublishesAndReadsAgentPackages(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := agentToolPolicyTestPackage()
	savePassingAgentPackageTestEvaluation(t, store, pkg)
	payload, err := json.Marshal(AgentPackagePublishRequest{
		IdempotencyKey: "operator:http:1",
		Package:        pkg,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               store,
		AuthToken:           "consumer-token",
		AgentPublisherToken: "publisher-token",
	})

	unauthorized := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "", string(payload))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized publish status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	consumerPublish := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "consumer-token", string(payload))
	if consumerPublish.Code != http.StatusUnauthorized {
		t.Fatalf("consumer publish status=%d body=%s", consumerPublish.Code, consumerPublish.Body.String())
	}
	published := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "publisher-token", string(payload))
	if published.Code != http.StatusCreated || !strings.Contains(published.Body.String(), `"created":true`) ||
		!strings.Contains(published.Body.String(), `"lifecycle_state":"published"`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	replayed := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "publisher-token", string(payload))
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"created":false`) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}

	list := requestKBase(handler, http.MethodGet, "/api/agent-packages", "consumer-token")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"package_id":"agent-package-example"`) ||
		!strings.Contains(list.Body.String(), `"url":"/api/agent-packages/agent-package-example?version=1.0.0"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	detail := requestKBase(handler, http.MethodGet, "/api/agent-packages/agent-package-example", "consumer-token")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"content_hash":"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var detailPayload struct {
		Evaluation AgentEvaluationReport `json:"evaluation"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if !detailPayload.Evaluation.Passed || detailPayload.Evaluation.PackageContentHash != pkg.ContentHash ||
		detailPayload.Evaluation.SuiteVersion != pkg.EvaluationPolicy.SuiteVersion ||
		detailPayload.Evaluation.InputHash == "" || detailPayload.Evaluation.EvaluatorVersion == "" ||
		detailPayload.Evaluation.EvaluatedAt == "" {
		t.Fatalf("detail evaluation provenance = %#v", detailPayload.Evaluation)
	}
	versioned := requestKBase(handler, http.MethodGet, "/api/agent-packages/agent-package-example?version=1.0.0", "consumer-token")
	if versioned.Code != http.StatusOK || !strings.Contains(versioned.Body.String(), `"version":"1.0.0"`) {
		t.Fatalf("versioned detail status=%d body=%s", versioned.Code, versioned.Body.String())
	}

	changed := agentToolPolicyTestPackage()
	changed.Version = "2.0.0"
	changed, err = FinalizeAgentPackage(changed)
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, store, changed)
	conflictPayload, _ := json.Marshal(AgentPackagePublishRequest{IdempotencyKey: "operator:http:1", Package: changed})
	conflict := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "publisher-token", string(conflictPayload))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	if err := os.Remove(store.AgentPackageEvaluationPath(pkg.ContentHash)); err != nil {
		t.Fatalf("remove evaluation fixture: %v", err)
	}
	missingEvaluation := requestKBase(handler, http.MethodGet, "/api/agent-packages/agent-package-example", "consumer-token")
	if missingEvaluation.Code != http.StatusInternalServerError {
		t.Fatalf("detail without persisted evaluation status=%d body=%s", missingEvaluation.Code, missingEvaluation.Body.String())
	}
}

func TestKBaseHTTPHandlerEvaluatesAndPersistsAgentPackageBeforePublication(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	pkg := agentToolPolicyTestPackage()
	payload, err := json.Marshal(map[string]any{
		"package": pkg,
		"suite":   loadAgentEvaluationFixture(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               store,
		AuthToken:           "consumer-token",
		AgentPublisherToken: "publisher-token",
	})

	for name, token := range map[string]string{"missing": "", "consumer": "consumer-token"} {
		response := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/evaluate", token, string(payload))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s evaluation status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
	var forged map[string]any
	if err := json.Unmarshal(payload, &forged); err != nil {
		t.Fatal(err)
	}
	cases := forged["suite"].(map[string]any)["cases"].([]any)
	cases[0].(map[string]any)["evidence_audit"] = map[string]any{"status": EvidenceAuditCompleted}
	forgedPayload, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	rejected := requestJSONKBase(
		handler, http.MethodPost, "/api/agent-packages/evaluate",
		"publisher-token", string(forgedPayload),
	)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("embedded evidence audit status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	evaluated := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/evaluate", "publisher-token", string(payload))
	if evaluated.Code != http.StatusCreated || !strings.Contains(evaluated.Body.String(), `"created":true`) ||
		!strings.Contains(evaluated.Body.String(), `"passed":true`) {
		t.Fatalf("evaluation status=%d body=%s", evaluated.Code, evaluated.Body.String())
	}
	if _, err := store.LoadAgentPackageEvaluation(pkg.ContentHash); err != nil {
		t.Fatalf("production evaluation sidecar was not persisted: %v", err)
	}
	replayed := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/evaluate", "publisher-token", string(payload))
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"created":false`) {
		t.Fatalf("evaluation replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	publishPayload, _ := json.Marshal(AgentPackagePublishRequest{
		IdempotencyKey: "operator:evaluated-http:1",
		Package:        pkg,
	})
	published := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/publish", "publisher-token", string(publishPayload))
	if published.Code != http.StatusCreated {
		t.Fatalf("publish evaluated package status=%d body=%s", published.Code, published.Body.String())
	}
}

func TestKBaseHTTPHandlerControlledAgentRequiresCookieSessionAndBuildsDraft(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, store)
	sessionDirectory := t.TempDir()
	if err := os.Chmod(sessionDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path: filepath.Join(sessionDirectory, "browser-sessions.sqlite3"),
		TTL:  24 * time.Hour, RenewalInterval: time.Hour, MaxActive: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sessionStore.Close() })
	credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Controlled Agent Browser"})
	if err != nil {
		t.Fatal(err)
	}
	csrfToken, _, err := sessionStore.IssueCSRF(credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-token", AgentPublisherToken: "publisher-token",
		BrowserSessionSecret: "browser-secret",
		BrowserSessions: BrowserSessionHTTPConfig{
			Store: sessionStore, PublicOrigin: testBrowserSessionOrigin,
			TTL: 24 * time.Hour, RenewalInterval: time.Hour, MaxActive: 10,
		},
	})
	requestBody := `{"draft":{"release_id":"release-1","package_id":"controlled-agent","version":"1.0.0"}}`

	bearer := requestJSONKBase(handler, http.MethodPost, "/api/controlled-agent/draft", "consumer-token", requestBody)
	if bearer.Code != http.StatusUnauthorized {
		t.Fatalf("bearer controlled draft status=%d body=%s", bearer.Code, bearer.Body.String())
	}

	request := newKBaseBrowserCookieRequest(http.MethodPost, "/api/controlled-agent/draft", credentials.Token, requestBody)
	addKBaseBrowserSessionSecurityHeaders(request, csrfToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cookie controlled draft status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestKBaseHTTPHandlerAgentCompilationUsesReadOnlyAPIAuthAndReturnsCandidates(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	primary := agentCompilerTestRelease(
		"release-primary",
		"book-primary",
		"2026-07-26T10:00:00Z",
		"单一来源结论",
		"Publisher Primary",
		"dedao_ebook",
	)
	saveKnowledgeAssemblyRelease(t, store, primary)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               store,
		AuthToken:           "consumer-token",
		AgentPublisherToken: "publisher-token",
	})
	payload, err := json.Marshal(AgentCompilationRequest{
		SchemaVersion:    AgentCompilationRequestSchemaVersion,
		Mode:             AgentCompilationModeDual,
		PrimaryReleaseID: primary.ReleaseID,
		Version:          "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/agent-packages/compile"
	for name, token := range map[string]string{
		"missing":   "",
		"publisher": "publisher-token",
		"wrong":     "wrong-token",
	} {
		response := requestJSONKBase(handler, http.MethodPost, path, token, string(payload))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf(
				"%s compilation status=%d body=%s",
				name,
				response.Code,
				response.Body.String(),
			)
		}
	}
	response := requestJSONKBase(
		handler,
		http.MethodPost,
		path,
		"consumer-token",
		string(payload),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("compilation status=%d body=%s", response.Code, response.Body.String())
	}
	var compilation AgentCompilation
	if err := json.Unmarshal(response.Body.Bytes(), &compilation); err != nil {
		t.Fatal(err)
	}
	if compilation.Status != AgentCompilationStatusPartial ||
		len(compilation.Candidates) != 2 ||
		compilation.Candidates[0].Status != AgentCompilationCandidateReady ||
		compilation.Candidates[1].Status != AgentCompilationCandidateBlocked {
		t.Fatalf("compilation response = %#v", compilation)
	}

	wrongMethod := requestKBase(handler, http.MethodGet, path, "consumer-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"compilation wrong method status=%d body=%s",
			wrongMethod.Code,
			wrongMethod.Body.String(),
		)
	}
}

func TestKBaseHTTPHandlerAgentCompilationRejectsInvalidBodies(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               store,
		AuthToken:           "consumer-token",
		AgentPublisherToken: "publisher-token",
	})
	path := "/api/agent-packages/compile"
	valid := `{
		"schema_version":"agent-compilation-request.v1",
		"mode":"study",
		"primary_release_id":"release-primary",
		"version":"1.0.0"
	}`
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{
			name: "unknown field",
			body: `{
				"schema_version":"agent-compilation-request.v1",
				"mode":"study",
				"primary_release_id":"release-primary",
				"version":"1.0.0",
				"model":"caller-controlled"
			}`,
		},
		{name: "trailing JSON", body: valid + `{}`},
		{
			name: "body too large",
			body: `{"schema_version":"agent-compilation-request.v1","mode":"study","primary_release_id":"` +
				strings.Repeat("x", 70<<10) +
				`","version":"1.0.0"}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := requestJSONKBase(
				handler,
				http.MethodPost,
				path,
				"consumer-token",
				testCase.body,
			)
			if response.Code != http.StatusBadRequest {
				t.Fatalf(
					"status=%d body=%s",
					response.Code,
					response.Body.String(),
				)
			}
		})
	}
}

func TestKBaseHTTPHandlerAgentCompilationHidesStoreFailures(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               NewBookKnowledgeStore(root),
		AuthToken:           "consumer-token",
		AgentPublisherToken: "publisher-token",
	})
	response := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/agent-packages/compile",
		"consumer-token",
		`{
			"schema_version":"agent-compilation-request.v1",
			"mode":"study",
			"primary_release_id":"release-primary",
			"version":"1.0.0"
		}`,
	)
	if response.Code != http.StatusInternalServerError ||
		!strings.Contains(response.Body.String(), "agent compilation unavailable") ||
		strings.Contains(response.Body.String(), root) ||
		strings.Contains(response.Body.String(), "not a directory") {
		t.Fatalf(
			"store failure status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}
}

func TestKBaseHTTPHandlerSeparatesTrustedGoldInstallationFromPublisherEvaluation(t *testing.T) {
	store, pkg := evidenceAuditEvaluationStore(t)
	supported := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictSupported, false, "http-trusted-supported")
	conflicted := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictMixed, true, "http-trusted-conflicted")
	insufficient := persistEvidenceAuditEvaluationReport(t, store, pkg, EvidenceAuditVerdictInsufficient, false, "http-trusted-insufficient")
	submitted := evidenceAuditEvaluationSuite(pkg, supported, conflicted, insufficient)
	trusted := submitted
	trusted.Cases = append([]AgentEvaluationCase(nil), submitted.Cases...)
	for index := range trusted.Cases {
		trusted.Cases[index].AuditID = ""
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               store,
		AuthToken:           "admin-token",
		AgentPublisherToken: "publisher-token",
	})
	trustPayload, err := json.Marshal(AgentPackageTrustedEvaluationSuiteRequest{
		Package: pkg,
		Suite:   trusted,
	})
	if err != nil {
		t.Fatal(err)
	}
	publisherTrust := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/agent-packages/evaluation-suites/trust",
		"publisher-token",
		string(trustPayload),
	)
	if publisherTrust.Code != http.StatusUnauthorized {
		t.Fatalf("publisher installed trusted gold status=%d body=%s", publisherTrust.Code, publisherTrust.Body.String())
	}
	adminTrust := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/agent-packages/evaluation-suites/trust",
		"admin-token",
		string(trustPayload),
	)
	if adminTrust.Code != http.StatusCreated || !strings.Contains(adminTrust.Body.String(), `"trusted":true`) {
		t.Fatalf("admin trusted suite status=%d body=%s", adminTrust.Code, adminTrust.Body.String())
	}

	tampered := submitted
	tampered.Cases = append([]AgentEvaluationCase(nil), submitted.Cases...)
	tampered.Cases[0].ExpectedClaims = append(
		[]AgentEvaluationExpectedClaim(nil),
		submitted.Cases[0].ExpectedClaims...,
	)
	tampered.Cases[0].ExpectedClaims[0].Verdict = EvidenceAuditVerdictContradicted
	evaluatePayload, err := json.Marshal(AgentPackageEvaluationRequest{Package: pkg, Suite: tampered})
	if err != nil {
		t.Fatal(err)
	}
	evaluated := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/agent-packages/evaluate",
		"publisher-token",
		string(evaluatePayload),
	)
	if evaluated.Code != http.StatusBadRequest || !strings.Contains(evaluated.Body.String(), "trusted evaluation suite") {
		t.Fatalf("tampered publisher gold status=%d body=%s", evaluated.Code, evaluated.Body.String())
	}
	if _, err := store.LoadAgentPackageEvaluation(pkg.ContentHash); !os.IsNotExist(err) {
		t.Fatalf("tampered evaluation persisted sidecar: %v", err)
	}

	legacyReport, err := EvaluateAgentPackageDeterministically(
		store,
		pkg,
		submitted,
		testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	legacySuitePayload, err := encodeJSONFile(submitted)
	if err != nil {
		t.Fatal(err)
	}
	legacyReportPayload, err := encodeJSONFile(legacyReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.AgentPackageEvaluationDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(store.AgentPackageEvaluationSuitePath(pkg.ContentHash), legacySuitePayload); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(store.AgentPackageEvaluationPath(pkg.ContentHash), legacyReportPayload); err != nil {
		t.Fatal(err)
	}
	validPayload, err := json.Marshal(AgentPackageEvaluationRequest{Package: pkg, Suite: submitted})
	if err != nil {
		t.Fatal(err)
	}
	migrated := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/agent-packages/evaluate",
		"publisher-token",
		string(validPayload),
	)
	if migrated.Code != http.StatusOK ||
		!strings.Contains(migrated.Body.String(), `"migrated":true`) {
		t.Fatalf("legacy trusted evaluation migration status=%d body=%s", migrated.Code, migrated.Body.String())
	}
	stored, err := store.LoadAgentPackageEvaluation(pkg.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.TrustedSuiteHash == "" {
		t.Fatal("legacy evaluation migration did not persist trusted_suite_hash")
	}
}

func TestKBaseHTTPHandlerServesDedaoSubscribedLibrary(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:        NewBookKnowledgeStore(t.TempDir()),
		AuthToken:    "secret-token",
		DedaoLibrary: fakeDedaoLibrary{},
	})

	resp := requestKBase(handler, http.MethodGet, "/api/dedao/library?category=bauhinia&page=2&page_size=3", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("library status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"category":"bauhinia"`) || !strings.Contains(resp.Body.String(), `"title":"得到订阅课程"`) {
		t.Fatalf("library response missing subscribed course: %s", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"page":2`) || !strings.Contains(resp.Body.String(), `"page_size":3`) {
		t.Fatalf("library response missing pagination: %s", resp.Body.String())
	}

	ebooks := requestKBase(handler, http.MethodGet, "/api/dedao/library?category=ebook", "secret-token")
	if ebooks.Code != http.StatusOK || !strings.Contains(ebooks.Body.String(), `"title":"得到订阅电子书"`) {
		t.Fatalf("ebook library status=%d body=%s", ebooks.Code, ebooks.Body.String())
	}

	home := requestKBase(handler, http.MethodGet, "/api/dedao/home", "secret-token")
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `"courses"`) || !strings.Contains(home.Body.String(), `"ebooks"`) || !strings.Contains(home.Body.String(), `"odob"`) {
		t.Fatalf("home status=%d body=%s", home.Code, home.Body.String())
	}

	invalid := requestKBase(handler, http.MethodGet, "/api/dedao/library?category=bad", "secret-token")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid category status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	detail := requestKBase(handler, http.MethodGet, "/api/dedao/course?enid=course-enid", "secret-token")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"name":"得到订阅课程详情"`) || !strings.Contains(detail.Body.String(), `"title":"第一讲文章列表"`) {
		t.Fatalf("course detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	articles := requestKBase(handler, http.MethodGet, "/api/dedao/course/articles?enid=course-enid&count=30&max_id=1", "secret-token")
	if articles.Code != http.StatusOK || !strings.Contains(articles.Body.String(), `"title":"第一讲文章列表"`) {
		t.Fatalf("course articles status=%d body=%s", articles.Code, articles.Body.String())
	}

	article := requestKBase(handler, http.MethodGet, "/api/dedao/article?enid=article-enid", "secret-token")
	if article.Code != http.StatusOK || !strings.Contains(article.Body.String(), `"markdown":"# 正文标题`) {
		t.Fatalf("course article status=%d body=%s", article.Code, article.Body.String())
	}

	audio := requestKBase(handler, http.MethodGet, "/api/dedao/audio?enid=audio-enid&alias_id=audio-alias", "secret-token")
	if audio.Code != http.StatusOK || !strings.Contains(audio.Body.String(), `"title":"得到听书详情"`) || !strings.Contains(audio.Body.String(), `"markdown":"# 听书文稿`) {
		t.Fatalf("audio detail status=%d body=%s", audio.Code, audio.Body.String())
	}

	missingDetail := requestKBase(handler, http.MethodGet, "/api/dedao/course", "secret-token")
	if missingDetail.Code != http.StatusBadRequest {
		t.Fatalf("missing course detail enid status=%d body=%s", missingDetail.Code, missingDetail.Body.String())
	}
}

type recordingEvidenceAuditEnqueuer struct {
	mu       sync.Mutex
	auditIDs []string
	err      error
}

func (e *recordingEvidenceAuditEnqueuer) Enqueue(auditID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.auditIDs = append(e.auditIDs, auditID)
	return e.err
}

func TestKBaseHTTPHandlerCreatesListsAndLoadsEvidenceAuditsAsynchronously(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 2, 1)
	queue := &recordingEvidenceAuditEnqueuer{}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", AuditCoordinator: queue,
		AuditMaxBodyBytes: 4096, AuditRetrySigningKey: []byte("test-retry-signing-key-32-bytes!!"),
	})
	body := `{"subject":"Trial claim","scope":"Population evidence comparison","selected_claims":["Synthetic grounded statement"],"idempotency_key":"audit-http-1"}`
	created := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/"+pkg.PackageID+"/audits?version="+pkg.Version, "consumer-a", body)
	if created.Code != http.StatusAccepted || !strings.Contains(created.Body.String(), `"status":"queued"`) {
		t.Fatalf("create audit status=%d body=%s", created.Code, created.Body.String())
	}
	var createdPayload struct {
		Created bool           `json:"created"`
		Audit   *EvidenceAudit `json:"audit"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdPayload); err != nil || createdPayload.Audit == nil {
		t.Fatalf("decode created audit: payload=%#v err=%v", createdPayload, err)
	}
	replayed := requestJSONKBase(handler, http.MethodPost, "/api/agent-packages/"+pkg.PackageID+"/audits?version="+pkg.Version, "consumer-a", body)
	if replayed.Code != http.StatusAccepted || !strings.Contains(replayed.Body.String(), `"created":false`) ||
		!strings.Contains(replayed.Body.String(), createdPayload.Audit.AuditID) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	listed := requestKBase(handler, http.MethodGet, "/api/agent-audits?package_id="+pkg.PackageID+"&version="+pkg.Version, "consumer-a")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), createdPayload.Audit.AuditID) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	packageList := requestKBase(handler, http.MethodGet, "/api/agent-packages/"+pkg.PackageID+"/audits?version="+pkg.Version, "consumer-a")
	if packageList.Code != http.StatusOK || !strings.Contains(packageList.Body.String(), createdPayload.Audit.AuditID) {
		t.Fatalf("package list status=%d body=%s", packageList.Code, packageList.Body.String())
	}
	detail := requestKBase(handler, http.MethodGet, "/api/agent-audits/"+createdPayload.Audit.AuditID, "consumer-a")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"selected_claims":["Synthetic grounded statement"]`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	otherBearer := requestKBase(handler, http.MethodGet, "/api/agent-audits/"+createdPayload.Audit.AuditID, "consumer-b")
	if otherBearer.Code != http.StatusUnauthorized {
		t.Fatalf("other bearer status=%d body=%s", otherBearer.Code, otherBearer.Body.String())
	}
	if len(queue.auditIDs) != 2 || queue.auditIDs[0] != createdPayload.Audit.AuditID {
		t.Fatalf("enqueued audit IDs = %#v", queue.auditIDs)
	}
}

func TestKBaseHTTPHandlerEvidenceAuditValidationAndAvailabilityErrors(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	base := KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", AuditCoordinator: &recordingEvidenceAuditEnqueuer{},
		AuditMaxBodyBytes: 128,
	}
	handler := NewKBaseHTTPHandler(base)
	path := "/api/agent-packages/" + pkg.PackageID + "/audits?version=" + pkg.Version
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "invalid json", method: http.MethodPost, path: path, body: `{`, status: http.StatusBadRequest},
		{name: "missing version", method: http.MethodPost, path: "/api/agent-packages/" + pkg.PackageID + "/audits", body: `{}`, status: http.StatusBadRequest},
		{name: "invalid version", method: http.MethodPost, path: "/api/agent-packages/" + pkg.PackageID + "/audits?version=bad%2Fversion", body: `{}`, status: http.StatusBadRequest},
		{name: "missing package", method: http.MethodPost, path: "/api/agent-packages/missing/audits?version=2.0.0", body: `{"subject":"x","scope":"y","idempotency_key":"z"}`, status: http.StatusNotFound},
		{name: "too many claims", method: http.MethodPost, path: path, body: `{"subject":"x","scope":"y","selected_claims":["Synthetic grounded statement","Primary claim two"],"idempotency_key":"too-many"}`, status: http.StatusUnprocessableEntity},
		{name: "body too large", method: http.MethodPost, path: path, body: `{"subject":"` + strings.Repeat("x", 256) + `","scope":"y","idempotency_key":"large"}`, status: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestJSONKBase(handler, test.method, test.path, "secret-token", test.body)
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}

	unconfigured := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", AuditUnavailableReason: "TokenPlan API key is not configured",
	})
	response := requestJSONKBase(unconfigured, http.MethodPost, path, "secret-token", `{"subject":"x","scope":"y","idempotency_key":"unavailable"}`)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"audit_service_unavailable"`) {
		t.Fatalf("unconfigured status=%d body=%s", response.Code, response.Body.String())
	}
	missingAudit := requestKBase(handler, http.MethodGet, "/api/agent-audits/missing-audit", "secret-token")
	if missingAudit.Code != http.StatusNotFound {
		t.Fatalf("missing audit status=%d body=%s", missingAudit.Code, missingAudit.Body.String())
	}

	v1Store := NewBookKnowledgeStore(t.TempDir())
	saveAgentPackageTestRelease(t, v1Store)
	v1, err := FinalizeAgentPackage(validAgentPackage())
	if err != nil {
		t.Fatal(err)
	}
	savePassingAgentPackageTestEvaluation(t, v1Store, v1)
	publishedV1, _, err := PublishAgentPackage(
		v1Store, v1, "publish-http-v1", AgentReadOnlyToolIDs(), testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	v1Handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: v1Store, AuthToken: "secret-token", AuditCoordinator: &recordingEvidenceAuditEnqueuer{},
	})
	v1Response := requestJSONKBase(
		v1Handler, http.MethodPost,
		"/api/agent-packages/"+publishedV1.PackageID+"/audits?version="+publishedV1.Version,
		"secret-token", `{"subject":"x","scope":"y","idempotency_key":"v1"}`,
	)
	if v1Response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("v1 package status=%d body=%s", v1Response.Code, v1Response.Body.String())
	}
}

func TestKBaseHTTPHandlerEvidenceAuditIdempotencyConflictAndManualRetry(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	queue := &recordingEvidenceAuditEnqueuer{}
	now := testAgentPackageTime().Add(4 * time.Hour)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", AuditCoordinator: queue,
		AuditRetrySigningKey: []byte("test-retry-signing-key-32-bytes!!"),
		AuditNow:             func() time.Time { return now },
	})
	path := "/api/agent-packages/" + pkg.PackageID + "/audits?version=" + pkg.Version
	first := requestJSONKBase(handler, http.MethodPost, path, "consumer-a", `{"subject":"one","scope":"Population evidence comparison","idempotency_key":"same-key"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", first.Code, first.Body.String())
	}
	conflict := requestJSONKBase(handler, http.MethodPost, path, "consumer-a", `{"subject":"different","scope":"Population evidence comparison","idempotency_key":"same-key"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	var payload struct {
		Audit EvidenceAudit `json:"audit"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := StartEvidenceAudit(store, payload.Audit.AuditID, "trace-http-retry", testAgentPackageTime().Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := FailEvidenceAudit(store, payload.Audit.AuditID, "model_outcome_unknown", "manual retry required", testAgentPackageTime().Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	now = testAgentPackageTime().Add(7 * time.Hour)
	retryPath := "/api/agent-audits/" + payload.Audit.AuditID + "/retry"
	retry := httptest.NewRequest(http.MethodPost, retryPath, nil)
	retry.Header.Set("Authorization", "Bearer consumer-a")
	retry.Header.Set("Idempotency-Key", "retry-http-1")
	retryResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryResponse, retry)
	if retryResponse.Code != http.StatusAccepted || !strings.Contains(retryResponse.Body.String(), `"retry_of":"`+payload.Audit.AuditID+`"`) {
		t.Fatalf("retry status=%d body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	now = now.Add(20 * time.Minute)
	replayed := httptest.NewRequest(http.MethodPost, retryPath, nil)
	replayed.Header.Set("Authorization", "Bearer consumer-a")
	replayed.Header.Set("Idempotency-Key", "retry-http-1")
	replayedResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayedResponse, replayed)
	if replayedResponse.Code != http.StatusAccepted || !strings.Contains(replayedResponse.Body.String(), `"created":false`) {
		t.Fatalf("retry replay status=%d body=%s", replayedResponse.Code, replayedResponse.Body.String())
	}
	second := httptest.NewRequest(http.MethodPost, retryPath, nil)
	second.Header.Set("Authorization", "Bearer consumer-a")
	second.Header.Set("Idempotency-Key", "retry-http-2")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("second active retry status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
}

func TestKBaseHTTPHandlerProofroomPreviewIsReadOnlyAndDeliveryIsExplicit(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	var calls atomic.Int32
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return proofroomJSONResponse(
				http.StatusOK,
				`{"receipt_id":"proofroom-http-1","status":"accepted"}`,
			), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", ProofroomDelivery: service,
	})
	path := "/api/agent-audits/" + audit.AuditID + "/proofroom"
	preview := requestKBase(handler, http.MethodGet, path, "consumer-a")
	if preview.Code != http.StatusOK ||
		!strings.Contains(preview.Body.String(), `"payload_hash":"sha256:`) ||
		!strings.Contains(preview.Body.String(), `"adjudication_authority":"proofroom"`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatalf("preview called remote %d times", calls.Load())
	}
	if _, err := os.Stat(store.EvidenceAuditProofroomDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote receipt state: %v", err)
	}

	missingKey := requestKBase(handler, http.MethodPost, path, "consumer-a")
	if missingKey.Code != http.StatusBadRequest ||
		!strings.Contains(missingKey.Body.String(), `"code":"proofroom_idempotency_key_required"`) {
		t.Fatalf("missing key status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("Authorization", "Bearer consumer-a")
	request.Header.Set("Idempotency-Key", "proofroom-http-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		!strings.Contains(response.Body.String(), `"created":true`) ||
		!strings.Contains(response.Body.String(), `"remote_receipt_id":"proofroom-http-1"`) {
		t.Fatalf("delivery status=%d body=%s", response.Code, response.Body.String())
	}
	replay := httptest.NewRequest(http.MethodPost, path, nil)
	replay.Header.Set("Authorization", "Bearer consumer-a")
	replay.Header.Set("Idempotency-Key", "proofroom-http-key")
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK ||
		!strings.Contains(replayResponse.Body.String(), `"created":false`) ||
		calls.Load() != 1 {
		t.Fatalf("replay status=%d calls=%d body=%s",
			replayResponse.Code, calls.Load(), replayResponse.Body.String())
	}
}

func TestKBaseHTTPHandlerProofroomPrivacyBlockedDoesNotSend(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomReportTest(
		t, t.TempDir(), validEvidenceAuditInput(), func(report *EvidenceAudit) {
			report.Proofroom.Title = "token"
		},
	)
	var calls atomic.Int32
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return proofroomJSONResponse(http.StatusOK, `{"receipt_id":"bad","status":"accepted"}`), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", ProofroomDelivery: service,
	})
	path := "/api/agent-audits/" + audit.AuditID + "/proofroom"
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("Authorization", "Bearer consumer-a")
		request.Header.Set("Idempotency-Key", "privacy-key")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(response.Body.String(), `"code":"privacy_blocked"`) {
			t.Fatalf("%s status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("privacy-blocked projection reached remote: %d", calls.Load())
	}
}

func TestKBaseHTTPHandlerProofroomDeliveryErrorsAreStable(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	path := "/api/agent-audits/" + audit.AuditID + "/proofroom"
	unconfigured := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a",
	})
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("Authorization", "Bearer consumer-a")
	request.Header.Set("Idempotency-Key", "proofroom-key")
	response := httptest.NewRecorder()
	unconfigured.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"proofroom_unconfigured"`) {
		t.Fatalf("unconfigured status=%d body=%s", response.Code, response.Body.String())
	}

	for _, test := range []struct {
		name       string
		client     ProofroomHTTPClient
		wantStatus int
		wantCode   string
	}{
		{
			name: "remote rejection",
			client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
				return proofroomJSONResponse(http.StatusUnprocessableEntity, `{"error":"rejected"}`), nil
			}),
			wantStatus: http.StatusBadGateway,
			wantCode:   "proofroom_remote_rejected",
		},
		{
			name: "unknown transport outcome",
			client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
			wantStatus: http.StatusBadGateway,
			wantCode:   "proofroom_outcome_unknown",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			testStore, testAudit := completedEvidenceAuditForProofroomTest(t)
			service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
				Endpoint: "https://proofroom.example.test/deliver",
				Token:    "remote-secret", Client: test.client,
			})
			if err != nil {
				t.Fatal(err)
			}
			handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
				Store: testStore, AuthToken: "consumer-a", ProofroomDelivery: service,
			})
			testRequest := httptest.NewRequest(
				http.MethodPost,
				"/api/agent-audits/"+testAudit.AuditID+"/proofroom",
				nil,
			)
			testRequest.Header.Set("Authorization", "Bearer consumer-a")
			testRequest.Header.Set("Idempotency-Key", "proofroom-error-key")
			testResponse := httptest.NewRecorder()
			handler.ServeHTTP(testResponse, testRequest)
			if testResponse.Code != test.wantStatus ||
				!strings.Contains(testResponse.Body.String(), `"code":"`+test.wantCode+`"`) ||
				strings.Contains(testResponse.Body.String(), "remote-secret") {
				t.Fatalf("status=%d body=%s", testResponse.Code, testResponse.Body.String())
			}
			if test.name == "remote rejection" &&
				!strings.Contains(testResponse.Body.String(), `"remote_status":422`) {
				t.Fatalf("remote status is not visible: %s", testResponse.Body.String())
			}
			replay := httptest.NewRecorder()
			handler.ServeHTTP(replay, testRequest.Clone(context.Background()))
			if replay.Code != test.wantStatus ||
				!strings.Contains(replay.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
			}
			if test.name == "remote rejection" &&
				!strings.Contains(replay.Body.String(), `"remote_status":422`) {
				t.Fatalf("replayed remote status is not visible: %s", replay.Body.String())
			}
		})
	}

}

func TestKBaseHTTPHandlerProofroomUnknownRequiresExplicitCoordination(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	var calls atomic.Int32
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return nil, context.DeadlineExceeded
			}
			return proofroomJSONResponse(
				http.StatusOK, `{"receipt_id":"coordinated","status":"accepted"}`,
			), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", ProofroomDelivery: service,
	})
	path := "/api/agent-audits/" + audit.AuditID + "/proofroom"
	send := func(resolution string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer consumer-a")
		request.Header.Set("Idempotency-Key", "coordinate-key")
		if resolution != "" {
			request.Header.Set("Proofroom-Delivery-Resolution", resolution)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if first := send(""); first.Code != http.StatusBadGateway ||
		!strings.Contains(first.Body.String(), `"code":"proofroom_outcome_unknown"`) {
		t.Fatalf("unknown status=%d body=%s", first.Code, first.Body.String())
	}
	if replay := send(""); replay.Code != http.StatusBadGateway || calls.Load() != 1 {
		t.Fatalf("automatic replay status=%d calls=%d body=%s",
			replay.Code, calls.Load(), replay.Body.String())
	}
	if coordinated := send(ProofroomCoordinationConfirmedNotDelivered); coordinated.Code != http.StatusOK ||
		!strings.Contains(coordinated.Body.String(), `"coordinated":true`) {
		t.Fatalf("coordination status=%d body=%s", coordinated.Code, coordinated.Body.String())
	}
	if delivered := send(""); delivered.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("post-coordinate status=%d calls=%d body=%s",
			delivered.Code, calls.Load(), delivered.Body.String())
	}
}

func TestKBaseHTTPHandlerRejectsForgedAndExpiredRetryGrants(t *testing.T) {
	now := testAgentPackageTime()
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "consumer-a",
		AuditRetrySigningKey: []byte("test-retry-signing-key-32-bytes!!"),
		AuditNow:             func() time.Time { return now },
	}).(*kbaseHTTPHandler)
	request := httptest.NewRequest(http.MethodPost, "/api/agent-audits/audit-1/retry", nil)
	request.Header.Set("Authorization", "Bearer consumer-a")
	valid := handler.issueEvidenceAuditRetryAuthorization(request, "audit-1", "retry-1", now)
	if err := handler.validateEvidenceAuditRetryAuthorization(valid, now); err != nil {
		t.Fatalf("valid authorization rejected: %v", err)
	}
	forged := valid
	forged.Actor = evidenceAuditOpaqueIdentity("different-actor")
	if err := handler.validateEvidenceAuditRetryAuthorization(forged, now); err == nil {
		t.Fatal("forged authorization accepted")
	}
	expired := valid
	expired.ExpiresAt = now.Add(-time.Second)
	expired.Signature = handler.evidenceAuditRetryMAC(
		"grant", expired.AuditID, expired.Actor, expired.Issuer, expired.Scope,
		expired.ExpiresAt.Format(time.RFC3339Nano), expired.Nonce,
	)
	if err := handler.validateEvidenceAuditRetryAuthorization(expired, now); err == nil {
		t.Fatal("expired authorization accepted")
	}
}

func TestKBaseHTTPHandlerEvidenceAuditErrorsAreStableAndDoNotLeakStorageDetails(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := os.MkdirAll(store.EvidenceAuditDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	privateDetail := store.EvidenceAuditManifestPath() + " bearer super-secret-token"
	if err := os.WriteFile(store.EvidenceAuditManifestPath(), []byte("{"+privateDetail), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.EvidenceAuditManifestPath()+".bak", []byte("{broken-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := make([]EvidenceAuditHTTPLogEvent, 0, 1)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a",
		AuditLogger: func(event EvidenceAuditHTTPLogEvent) { logs = append(logs, event) },
	})
	response := requestKBase(handler, http.MethodGet, "/api/agent-audits", "consumer-a")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, response.Body.String())
	}
	if payload.Code != "audit_store_unavailable" || payload.Error != "evidence audit storage is unavailable" {
		t.Fatalf("stable error payload = %+v", payload)
	}
	if strings.Contains(response.Body.String(), store.Root()) ||
		strings.Contains(response.Body.String(), "super-secret-token") ||
		strings.Contains(response.Body.String(), "invalid character") {
		t.Fatalf("response leaked internal detail: %s", response.Body.String())
	}
	if len(logs) != 1 || logs[0].Code != payload.Code || logs[0].Operation != "list_audits" {
		t.Fatalf("audit logs = %+v", logs)
	}
	if !strings.Contains(logs[0].Cause, "invalid character") {
		t.Fatalf("logger did not receive complete storage diagnostic: %+v", logs[0])
	}
	if strings.Contains(logs[0].Cause, "super-secret-token") {
		t.Fatalf("logger leaked token: %+v", logs[0])
	}
}

func TestKBaseHTTPHandlerEvidenceAuditQueueErrorUsesStableResponse(t *testing.T) {
	store, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
	queue := &recordingEvidenceAuditEnqueuer{err: fmt.Errorf("%w: /private/path token=secret", ErrEvidenceAuditQueueFull)}
	logs := make([]EvidenceAuditHTTPLogEvent, 0, 1)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "consumer-a", AuditCoordinator: queue,
		AuditLogger: func(event EvidenceAuditHTTPLogEvent) { logs = append(logs, event) },
	})
	path := "/api/agent-packages/" + pkg.PackageID + "/audits?version=" + pkg.Version
	response := requestJSONKBase(
		handler, http.MethodPost, path, "consumer-a",
		`{"subject":"Trial claim","scope":"Population evidence comparison","idempotency_key":"queue-error"}`,
	)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"audit_queue_full"`) ||
		strings.Contains(response.Body.String(), "/private/path") {
		t.Fatalf("queue error status=%d body=%s", response.Code, response.Body.String())
	}
	if len(logs) != 1 || logs[0].Code != "audit_queue_full" ||
		strings.Contains(logs[0].Cause, "secret") {
		t.Fatalf("queue logs = %+v", logs)
	}
}

func TestKBaseHTTPHandlerEvidenceAuditPackageMissingUsesStableResponse(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "consumer-a",
		AuditCoordinator: &recordingEvidenceAuditEnqueuer{},
	})
	response := requestJSONKBase(
		handler, http.MethodPost,
		"/api/agent-packages/missing-package/audits?version=2.0.0",
		"consumer-a",
		`{"subject":"Trial claim","scope":"Population evidence comparison","idempotency_key":"missing-package"}`,
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "audit_package_not_found" ||
		payload["error"] != "agent package not found" {
		t.Fatalf("stable package error = %+v", payload)
	}
}

func TestKBaseHTTPHandlerAllEvidenceAuditMethodErrorsUseStableResponse(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "consumer-a",
		AuditCoordinator: &recordingEvidenceAuditEnqueuer{},
	})
	for _, path := range []string{
		"/api/agent-audits",
		"/api/agent-audits/audit-1",
		"/api/agent-audits/audit-1/retry",
		"/api/agent-audits/audit-1/proofroom",
		"/api/agent-packages/pkg/audits?version=2.0.0",
	} {
		response := requestKBase(handler, http.MethodDelete, path, "consumer-a")
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d body=%s", path, response.Code, response.Body.String())
		}
		var payload map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s invalid JSON: %v", path, err)
		}
		if payload["code"] != "audit_method_not_allowed" ||
			payload["error"] != "method not allowed" {
			t.Fatalf("%s unstable method error = %+v", path, payload)
		}
	}
}

func TestKBaseHTTPHandlerEvidenceAuditAuthorizationErrorsUseStableResponse(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "consumer-a",
		AuditCoordinator: &recordingEvidenceAuditEnqueuer{},
	})
	for _, token := range []string{"", "wrong-token"} {
		response := requestKBase(handler, http.MethodGet, "/api/agent-audits", token)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token=%q status=%d body=%s", token, response.Code, response.Body.String())
		}
		var payload map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["code"] != "audit_unauthorized" ||
			payload["error"] != "unauthorized" {
			t.Fatalf("token=%q unstable auth error = %+v", token, payload)
		}
	}
}

func TestSanitizeEvidenceAuditHTTPLogCauseRedactsCommonCredentialForms(t *testing.T) {
	secrets := []string{
		"query-api-key", "json-apikey", "client-secret", "plain-secret",
		"password-value", "passwd-value", "session-value", "csrf-value",
		"access-value", "refresh-value", "bearer-value", "basic-value",
	}
	cause := `request failed?api_key=query-api-key ` +
		`{"ApiKey":"json-apikey","client_secret":"client-secret","SECRET":"plain-secret",` +
		`"password":"password-value","passwd":"passwd-value","session":"session-value",` +
		`"csrf":"csrf-value","access_token":"access-value","refresh_token":"refresh-value"} ` +
		`Authorization: Bearer bearer-value Proxy-Authorization: Basic basic-value`
	sanitized := sanitizeEvidenceAuditHTTPLogCause(cause)
	for _, secret := range secrets {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("sanitized log leaked %q: %s", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "[redacted]") {
		t.Fatalf("sanitized log omitted redaction marker: %s", sanitized)
	}
}

func TestKBaseHTTPHandlerBookChatAllowsPost(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/book-chat", "secret-token", `{}`)
	if resp.Code == http.StatusMethodNotAllowed {
		t.Fatalf("book chat POST returned 405; HTTP API should expose TokenPlan analysis: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerBookChatMissingBookDoesNotExposeFilesystemPath(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	root := t.TempDir()
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(filepath.Join(root, "book_knowledge")),
		AuthToken: "secret-token",
	})

	resp := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/book-chat",
		"secret-token",
		`{"book_id":"missing-prompts","mode":"chat","question":"ping"}`,
	)
	body := resp.Body.String()
	if resp.Code != http.StatusNotFound {
		t.Errorf("missing book chat status = %d, want 404, body=%s", resp.Code, body)
	}
	for _, leak := range []string{root, "manifest.json", "book_knowledge"} {
		if strings.Contains(body, leak) {
			t.Errorf("missing book chat response leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "book not found") {
		t.Errorf("missing book chat response should be actionable: %s", body)
	}
}

func TestKBaseHTTPHandlerContextChatAllowsCourseArticleAnalysis(t *testing.T) {
	t.Setenv("DEDAO_TOKENPLAN_API_KEY", "sk-test-token")
	t.Setenv("DEDAO_TOKENPLAN_BASE_URL", "https://token-plan.example.test/compatible-mode/v1")
	client := &fakeBookKnowledgeLLMClient{answer: "临时文章分析"}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:      NewBookKnowledgeStore(t.TempDir()),
		AuthToken:  "secret-token",
		ChatClient: client,
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/context-chat", "secret-token", `{"title":"课程文章","source_type":"dedao_course_article","question":"总结","content":"正文内容","model":"Qwen-3.7-Max"}`)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"answer":"临时文章分析"`) || !strings.Contains(resp.Body.String(), `"model":"qwen3.7-max"`) {
		t.Fatalf("context chat status=%d body=%s", resp.Code, resp.Body.String())
	}
	if len(client.messages) != 2 || !strings.Contains(client.messages[1].Content, "课程文章") || !strings.Contains(client.messages[1].Content, "正文内容") {
		t.Fatalf("context chat messages = %#v", client.messages)
	}

	invalid := requestJSONKBase(handler, http.MethodPost, "/api/context-chat", "secret-token", `{"question":"总结"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid context chat status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestKBaseHTTPHandlerBookAnalysisGet(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if err := store.SaveAnalysisManifest(BookAnalysisManifest{
		Version: "1", BookID: "source-article-1", ContentHash: "hash-1",
		Status: BookAnalysisPending, UpdatedAt: "2026-07-12T12:00:00Z",
	}); err != nil {
		t.Fatalf("SaveAnalysisManifest returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestKBase(handler, http.MethodGet, "/api/books/source-article-1/analysis", "secret-token")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"pending"`) {
		t.Fatalf("analysis GET status=%d body=%s", resp.Code, resp.Body.String())
	}
	missing := requestKBase(handler, http.MethodGet, "/api/books/missing/analysis", "secret-token")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing analysis status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestKBaseHTTPHandlerBookAnalysisPost(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	var got BookAnalysisGenerateRequest
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, _ *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			got = request
			return &BookAnalysisManifest{Version: "1", BookID: request.BookID, Status: BookAnalysisReady, Model: request.Model, Answer: "analysis"}, nil
		},
	})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/books/source-article-1/analysis", "secret-token", `{"model":"Qwen-3.7-Max","max_context_chars":8000}`)
	if resp.Code != http.StatusOK || got.BookID != "source-article-1" || got.Model != "Qwen-3.7-Max" || got.MaxContextChars != 8000 {
		t.Fatalf("analysis POST status=%d request=%#v body=%s", resp.Code, got, resp.Body.String())
	}
	invalid := requestJSONKBase(handler, http.MethodPost, "/api/books/source-article-1/analysis", "secret-token", `{`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid analysis POST status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgePipeline(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "needs-analysis", "hash-analysis")
	savePipelinePackage(t, store, "needs-quality", "hash-quality")
	savePipelineAnalysis(t, store, "needs-quality", "hash-quality")
	generatorCalls := 0
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		AnalysisGenerator: func(_ context.Context, current *BookKnowledgeStore, request BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			generatorCalls++
			savePipelineAnalysis(t, current, request.BookID, "hash-analysis")
			return current.LoadAnalysisManifest(request.BookID)
		},
	})

	dashboard := requestKBase(handler, http.MethodGet, "/api/knowledge/pipeline?limit=10", "secret-token")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), `"needs_analysis":1`) || !strings.Contains(dashboard.Body.String(), `"next_action":"needs_quality"`) {
		t.Fatalf("pipeline status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}

	dryRun := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/pipeline/run", "secret-token", `{"dry_run":true,"limit":5}`)
	if dryRun.Code != http.StatusOK || !strings.Contains(dryRun.Body.String(), `"dry_run":true`) || !strings.Contains(dryRun.Body.String(), `"eligible":2`) {
		t.Fatalf("pipeline dry run status=%d body=%s", dryRun.Code, dryRun.Body.String())
	}
	if generatorCalls != 0 {
		t.Fatalf("dry run called generator %d times", generatorCalls)
	}

	run := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/pipeline/run", "secret-token", `{"limit":5}`)
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"analyzed":1`) || !strings.Contains(run.Body.String(), `"qualified":2`) {
		t.Fatalf("pipeline run status=%d body=%s", run.Code, run.Body.String())
	}
	if generatorCalls != 1 {
		t.Fatalf("run called generator %d times, want 1", generatorCalls)
	}
}

func TestKBaseHTTPHandlerKnowledgeReadiness(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.ContentHash = "readiness-hash"
	pkg.Book.SourceHTML = "sensitive-local-path/downloaded.html"
	pkg.Book.SourceAccount = "sensitive-local-path/account"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/readiness", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	response := requestKBase(handler, http.MethodGet, "/api/knowledge/readiness?limit=10&book_id=42", "secret-token")
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"schema_version":"knowledge_readiness.v1"`) ||
		!strings.Contains(response.Body.String(), `"book_id":"42"`) ||
		!strings.Contains(response.Body.String(), `"next_action":"needs_analysis"`) {
		t.Fatalf("readiness status=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"sensitive-local-path", "downloaded.html", `"source_account":`, `"prompt"`, `"answer"`} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("readiness leaked %q: %s", forbidden, response.Body.String())
		}
	}
	invalidLimit := requestKBase(handler, http.MethodGet, "/api/knowledge/readiness?limit=501", "secret-token")
	if invalidLimit.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d body=%s", invalidLimit.Code, invalidLimit.Body.String())
	}
	wrongMethod := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/readiness", "secret-token", `{}`)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeAssembly(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := knowledgeAssemblyTestRelease(
		"release-assembly",
		"book-assembly",
		"2026-07-26T10:00:00Z",
		"干预能改善结局",
		"private publisher value",
		"wechat_mp_article",
	)
	release.Book.SourceHTML = "local/private/source.html"
	release.Analysis.Summary = "private summary sentinel"
	saveKnowledgeAssemblyRelease(t, store, release)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/assembly", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	response := requestKBase(
		handler,
		http.MethodGet,
		"/api/knowledge/assembly?limit=1&query="+url.QueryEscape("改善结局"),
		"secret-token",
	)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"schema_version":"knowledge_release_assembly.v1"`) ||
		!strings.Contains(response.Body.String(), `"release_id":"release-assembly"`) ||
		!strings.Contains(response.Body.String(), `"claim_id":"release-assembly-claim"`) {
		t.Fatalf("assembly status=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{
		"private publisher value",
		"private summary sentinel",
		"local/private",
		`"source_account":`,
		`"prompt"`,
		`"answer"`,
	} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("assembly leaked %q: %s", forbidden, response.Body.String())
		}
	}
	for _, path := range []string{
		"/api/knowledge/assembly?limit=501",
		"/api/knowledge/assembly?limit=invalid",
		"/api/knowledge/assembly?query=" + url.QueryEscape(strings.Repeat("界", 257)),
	} {
		invalid := requestKBase(handler, http.MethodGet, path, "secret-token")
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid request %q status=%d body=%s", path, invalid.Code, invalid.Body.String())
		}
	}
	wrongMethod := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/assembly", "secret-token", `{}`)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeAssemblyRedactsInternalErrors(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	release := knowledgeAssemblyTestRelease(
		"release-missing",
		"book-missing",
		"2026-07-26T10:00:00Z",
		"缺失文件结论",
		"Publisher",
		"wechat_mp_article",
	)
	saveKnowledgeAssemblyRelease(t, store, release)
	if err := os.Remove(store.KnowledgeReleasePath(release.ReleaseID)); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	response := requestKBase(handler, http.MethodGet, "/api/knowledge/assembly", "secret-token")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.String() != "{\"error\":\"knowledge assembly unavailable\"}\n" ||
		strings.Contains(response.Body.String(), store.Root()) {
		t.Fatalf("internal error was not redacted: %s", response.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeOperationsConsole(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	saveHealthReadinessBook(t, store, "book-health", "hash-health")
	saveHealthAnalysis(t, store, "book-health", "hash-health")
	saveHealthQuality(t, store, "book-health", "hash-health", BookQualityPass, BookUsageEvidenceOnly)
	saveFeedRelease(t, store, sampleHealthEvidenceRelease())
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/operations", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	resp := requestKBase(handler, http.MethodGet, "/api/knowledge/operations?limit=10", "secret-token")
	if resp.Code != http.StatusOK ||
		!strings.Contains(resp.Body.String(), `"schema_version":"knowledge_operations.v1"`) ||
		!strings.Contains(resp.Body.String(), `"health_published":1`) ||
		!strings.Contains(resp.Body.String(), `"claim_count":2`) {
		t.Fatalf("operations status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "规律运动可能帮助") {
		t.Fatalf("operations response exposed claim statement: %s", resp.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodPost, "/api/knowledge/operations", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeOperationsReplayRejectsUnsafeActions(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	savePipelinePackage(t, store, "ready", "hash-ready")
	savePipelineAnalysis(t, store, "ready", "hash-ready")
	savePipelineQuality(t, store, "ready", "hash-ready", BookQualityPass)
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	resp := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/operations/replay", "secret-token", `{"book_id":"ready","action":"publish","confirm":true}`)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "not allowed") {
		t.Fatalf("unsafe replay status=%d body=%s", resp.Code, resp.Body.String())
	}
	planned := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/operations/replay", "secret-token", `{"book_id":"ready","action":"evaluate_quality"}`)
	if planned.Code != http.StatusOK || !strings.Contains(planned.Body.String(), `"status":"planned"`) || strings.Contains(planned.Body.String(), `"mutated":true`) {
		t.Fatalf("planned replay status=%d body=%s", planned.Code, planned.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodGet, "/api/knowledge/operations/replay", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeQualityAndRelease(t *testing.T) {
	store := qualityTestStore(t)
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	quality := requestKBase(handler, http.MethodGet, "/api/books/42/quality", "secret-token")
	if quality.Code != http.StatusOK || !strings.Contains(quality.Body.String(), `"decision":"pass"`) {
		t.Fatalf("quality status=%d body=%s", quality.Code, quality.Body.String())
	}
	published := requestJSONKBase(handler, http.MethodPost, "/api/books/42/publish", "secret-token", `{}`)
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"release_id":"release-`) {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	var release KnowledgeRelease
	if err := json.Unmarshal(published.Body.Bytes(), &release); err != nil {
		t.Fatalf("decode release: %v", err)
	}

	list := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?limit=10", "secret-token")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), release.ReleaseID) || !strings.Contains(list.Body.String(), `"next_cursor":"`+release.ReleaseID+`"`) {
		t.Fatalf("release list status=%d body=%s", list.Code, list.Body.String())
	}
	after := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?after="+url.QueryEscape(release.ReleaseID), "secret-token")
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"releases":[]`) {
		t.Fatalf("release cursor status=%d body=%s", after.Code, after.Body.String())
	}
	detail := requestKBase(handler, http.MethodGet, "/api/knowledge/releases/"+url.PathEscape(release.ReleaseID), "secret-token")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"usage_policy":"standard"`) {
		t.Fatalf("release detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodDelete, "/api/books/42/quality", "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("quality DELETE status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReleaseRejectsQuarantinedAnalysis(t *testing.T) {
	store := qualityTestStore(t)
	manifest, _ := store.LoadAnalysisManifest("42")
	manifest.Payload.Claims[0].CitationIDs = nil
	if err := store.SaveAnalysisManifest(*manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateBookAnalysisQuality(store, "42"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	resp := requestJSONKBase(handler, http.MethodPost, "/api/books/42/publish", "secret-token", `{}`)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "quality decision") {
		t.Fatalf("publish quarantined status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeFeedback(t *testing.T) {
	store, release := feedbackTestStore(t)
	analysisCalls := 0
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token",
		ReverificationNow:      func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) },
		ReverificationCooldown: 5 * time.Minute,
		AnalysisGenerator: func(context.Context, *BookKnowledgeStore, BookAnalysisGenerateRequest) (*BookAnalysisManifest, error) {
			analysisCalls++
			return nil, fmt.Errorf("must not run synchronously")
		},
	})
	path := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/feedback"
	empty := requestKBase(handler, http.MethodGet, path, "secret-token")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"disposition":"healthy"`) || strings.Contains(empty.Body.String(), "consumer") {
		t.Fatalf("empty feedback assessment status=%d body=%s", empty.Code, empty.Body.String())
	}
	payload := `{"event_id":"event-1","consumer":"health-assistant","outcome":"used","claim_ids":["claim-1"],"reason_code":"used_for_answer"}`
	resp := requestJSONKBase(handler, http.MethodPost, path, "secret-token", payload)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"feedback_id":"feedback-`) || !strings.Contains(resp.Body.String(), `"used":1`) || !strings.Contains(resp.Body.String(), `"assessment":{"release_id":`) {
		t.Fatalf("feedback status=%d body=%s", resp.Code, resp.Body.String())
	}
	reverify := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1b","consumer":"health-assistant","outcome":"stale","reason_code":"stale_source"}`)
	if reverify.Code != http.StatusOK || !strings.Contains(reverify.Body.String(), `"disposition":"reverify_required"`) || !strings.Contains(reverify.Body.String(), `"trigger_outcomes":["stale"]`) || !strings.Contains(reverify.Body.String(), `"reverification":{"version":"1"`) || !strings.Contains(reverify.Body.String(), `"status":"queued"`) {
		t.Fatalf("reverify feedback status=%d body=%s", reverify.Code, reverify.Body.String())
	}
	if analysisCalls != 0 {
		t.Fatalf("feedback invoked analysis generator %d times", analysisCalls)
	}
	replayed := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1b","consumer":"health-assistant","outcome":"stale","reason_code":"stale_source"}`)
	if replayed.Code != http.StatusOK {
		t.Fatalf("replayed feedback status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	tasks, err := store.ListKnowledgeReverifications(release.ReleaseID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("replayed feedback tasks=%#v err=%v", tasks, err)
	}
	statusPath := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/reverification"
	status := requestKBase(handler, http.MethodGet, statusPath, "secret-token")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"tasks":[`) || !strings.Contains(status.Body.String(), `"status":"queued"`) || strings.Contains(status.Body.String(), "health-assistant") || strings.Contains(status.Body.String(), "event-1b") {
		t.Fatalf("reverification status=%d body=%s", status.Code, status.Body.String())
	}
	method := requestJSONKBase(handler, http.MethodPost, statusPath, "secret-token", `{}`)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("reverification POST status=%d body=%s", method.Code, method.Body.String())
	}
	read := requestKBase(handler, http.MethodGet, path, "secret-token")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"reverify_required":true`) || strings.Contains(read.Body.String(), "event-1") || strings.Contains(read.Body.String(), "health-assistant") {
		t.Fatalf("feedback assessment status=%d body=%s", read.Code, read.Body.String())
	}
	sensitive := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-2","consumer":"health-assistant","outcome":"used","user_id":"private-user"}`)
	if sensitive.Code != http.StatusBadRequest || !strings.Contains(sensitive.Body.String(), "invalid JSON body") {
		t.Fatalf("sensitive feedback status=%d body=%s", sensitive.Code, sensitive.Body.String())
	}
	freeText := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-2b","consumer":"health-assistant","outcome":"used","reason":"private free text"}`)
	if freeText.Code != http.StatusBadRequest || !strings.Contains(freeText.Body.String(), "invalid JSON body") {
		t.Fatalf("free-text feedback status=%d body=%s", freeText.Code, freeText.Body.String())
	}
	mismatch := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-1","consumer":"health-assistant","outcome":"rejected","claim_ids":["claim-1"]}`)
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "idempotency") {
		t.Fatalf("mismatched feedback status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}
	invalidClaim := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{"event_id":"event-3","consumer":"health-assistant","outcome":"conflict","claim_ids":["missing"]}`)
	if invalidClaim.Code != http.StatusBadRequest || !strings.Contains(invalidClaim.Body.String(), "claim_id") {
		t.Fatalf("invalid claim status=%d body=%s", invalidClaim.Code, invalidClaim.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReverificationRetry(t *testing.T) {
	store, release := feedbackTestStore(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	task, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextKnowledgeReverification(now, 15*time.Minute)
	if err != nil || !ok {
		t.Fatalf("claimed = %#v, ok=%v, err=%v", claimed, ok, err)
	}
	if _, err := store.FailKnowledgeReverification(task.TaskID, claimed.AssessmentAt, claimed.AssessmentFingerprint, KnowledgeReverificationErrorAnalysisFailed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: store, AuthToken: "secret-token", ReverificationNow: func() time.Time { return now.Add(2 * time.Minute) },
	})
	path := "/api/knowledge/releases/" + url.PathEscape(release.ReleaseID) + "/reverification/retry"
	unauthorized := requestJSONKBase(handler, http.MethodPost, path, "", `{}`)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized retry status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	wrongMethod := requestKBase(handler, http.MethodGet, path, "secret-token")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("retry GET status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
	}
	retried := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{}`)
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"status":"queued"`) || !strings.Contains(retried.Body.String(), `"attempts":0`) {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	conflict := requestJSONKBase(handler, http.MethodPost, path, "secret-token", `{}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "failed") {
		t.Fatalf("retry conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	missing := requestJSONKBase(handler, http.MethodPost, "/api/knowledge/releases/missing/reverification/retry", "secret-token", `{}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing retry status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReleasesFiltersBookBeforeLimit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, release := range []KnowledgeRelease{
		{Version: knowledgeReleaseVersion, ReleaseID: "release-other", BookID: "other", CreatedAt: "2026-07-14T12:00:00Z"},
		{Version: knowledgeReleaseVersion, ReleaseID: "release-target", BookID: "target", CreatedAt: "2026-07-14T12:01:00Z"},
	} {
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	response := requestKBase(handler, http.MethodGet, "/api/knowledge/releases?book_id=target&limit=1", "secret-token")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"release_id":"release-target"`) || strings.Contains(response.Body.String(), `"release-other"`) {
		t.Fatalf("book-filtered releases status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReleasesListsLatestPerBookNewestFirst(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	for _, release := range []KnowledgeRelease{
		{
			Version:   knowledgeReleaseVersion,
			ReleaseID: "release-book-a-old",
			BookID:    "book-a",
			CreatedAt: "2026-07-14T12:00:00Z",
		},
		{
			Version:   knowledgeReleaseVersion,
			ReleaseID: "release-book-a-new",
			BookID:    "book-a",
			CreatedAt: "2026-07-14T12:02:00Z",
		},
		{
			Version:   knowledgeReleaseVersion,
			ReleaseID: "release-book-b-newest",
			BookID:    "book-b",
			CreatedAt: "2026-07-14T12:03:00Z",
		},
	} {
		if err := store.saveKnowledgeRelease(release); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})

	first := requestKBase(
		handler,
		http.MethodGet,
		"/api/knowledge/releases?latest=true&limit=1",
		"secret-token",
	)
	if first.Code != http.StatusOK ||
		!strings.Contains(first.Body.String(), `"release_id":"release-book-b-newest"`) ||
		strings.Contains(first.Body.String(), `"release-book-a-old"`) {
		t.Fatalf("latest release first page status=%d body=%s", first.Code, first.Body.String())
	}
	second := requestKBase(
		handler,
		http.MethodGet,
		"/api/knowledge/releases?latest=true&limit=1&after=release-book-b-newest",
		"secret-token",
	)
	if second.Code != http.StatusOK ||
		!strings.Contains(second.Body.String(), `"release_id":"release-book-a-new"`) ||
		strings.Contains(second.Body.String(), `"release-book-a-old"`) {
		t.Fatalf("latest release second page status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestKBaseHTTPHandlerKnowledgeReviewCockpit(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(root)
	savePipelinePackage(t, store, "review-book", "hash-review")
	savePipelineAnalysis(t, store, "review-book", "hash-review")
	savePipelineQuality(t, store, "review-book", "hash-review", BookQualityPass)
	release := KnowledgeRelease{
		SchemaVersion: KnowledgeReleaseSchemaVersion,
		Version:       knowledgeReleaseVersion,
		ReleaseID:     "release-review",
		BookID:        "review-book",
		ContentHash:   "hash-review",
		UsagePolicy:   BookUsageStandard,
		Book:          BookKnowledgeBook{BookID: "review-book", Title: "Review Book", ContentHash: "hash-review"},
		Analysis:      &BookAnalysisPayload{Summary: "summary", Claims: []BookAnalysisClaim{{ID: "claim-1", Statement: "claim", CitationIDs: []string{"citation-1"}, Confidence: 0.8, RiskLevel: "low"}}},
		Quality:       BookQualityReport{Decision: BookQualityPass, UsagePolicy: BookUsageStandard},
		Citations:     []BookKnowledgeCitation{{CitationID: "citation-1", BookID: "review-book"}},
		CreatedAt:     "2026-07-14T12:00:00Z",
	}
	if err := store.saveKnowledgeRelease(release); err != nil {
		t.Fatalf("save release: %v", err)
	}
	assessment := saveReverificationFeedback(t, store, release.ReleaseID, "event-stale", KnowledgeFeedbackStale)
	if _, err := store.EnqueueKnowledgeReverification(release.ReleaseID, *assessment, time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC), 0); err != nil {
		t.Fatalf("enqueue reverification: %v", err)
	}
	catalog, err := NewKnowledgeCatalogStore(root, nil)
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	if _, err := catalog.SaveDeliveryReceipt(DeliveryReceipt{
		SchemaVersion:  DeliveryReceiptSchemaVersion,
		Consumer:       "health-consumer",
		ReleaseID:      release.ReleaseID,
		IdempotencyKey: "health:release-review:1",
		Disposition:    "imported",
	}, nil); err != nil {
		t.Fatalf("save receipt: %v", err)
	}
	if err := catalog.RecordKnowledgeGap(KnowledgeGapInput{Consumer: "health-consumer", Domain: "health", Fingerprint: "gap-hash", Kind: "zero_hit"}); err != nil {
		t.Fatalf("record gap: %v", err)
	}
	if err := catalog.Close(); err != nil {
		t.Fatalf("close catalog: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: store, AuthToken: "secret-token"})
	unauthorized := requestKBase(handler, http.MethodGet, "/api/knowledge/review", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	resp := requestKBase(handler, http.MethodGet, "/api/knowledge/review?limit=20", "secret-token")
	body := resp.Body.String()
	if resp.Code != http.StatusOK ||
		!strings.Contains(body, `"schema_version":"knowledge_review.v1"`) ||
		!strings.Contains(body, `"release_id":"release-review"`) ||
		!strings.Contains(body, `"latest_reverification_status":"queued"`) ||
		!strings.Contains(body, `"attention_reasons":["reverification_queued"]`) ||
		!strings.Contains(body, `"receipt_counts":{"imported":1}`) ||
		!strings.Contains(body, `"pipeline_stage":"published"`) ||
		!strings.Contains(body, `"published_releases":1`) ||
		!strings.Contains(body, `"rebuild_actions":{"noop":1}`) ||
		!strings.Contains(body, `"rebuild_plan":{"schema_version":"knowledge_rebuild_plan.v1"`) ||
		!strings.Contains(body, `"fingerprint":"gap-hash"`) {
		t.Fatalf("review status=%d body=%s", resp.Code, body)
	}
	invalid := requestKBase(handler, http.MethodGet, "/api/knowledge/review?limit=201", "secret-token")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceAgentAuthenticationIsolation(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})
	heartbeat := `{"agent_id":"agent-a","version":"1.0.0","capabilities":["sync_content"],"wcplus_healthy":true}`

	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "admin-secret", heartbeat)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("admin token on agent route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "invalid-agent-token", heartbeat)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("invalid agent token status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "invalid-agent-token") {
		t.Fatalf("agent auth response leaked token: %s", resp.Body.String())
	}
	resp = requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"agent_id":"agent-a"`) {
		t.Fatalf("agent heartbeat status = %d, body=%s", resp.Code, resp.Body.String())
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	evolutionClient, err := NewEvolutionWorkerClient(EvolutionWorkerClientConfig{
		RemoteURL: server.URL, Token: "agent-secret", WorkerID: "agent-evolution-worker-production",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evolutionClient.Heartbeat(context.Background(), EvolutionCapabilityAgent, "1.0.1", "0123456789abcdef"); err != nil {
		t.Fatalf("production revision heartbeat: %v", err)
	}
	evolutionAgent, err := sourceSync.GetSourceAgent("agent-evolution-worker-production")
	if err != nil {
		t.Fatal(err)
	}
	health := evolutionAgent.CapabilityHealth[string(EvolutionCapabilityAgent)]
	if health.Version != "1.0.1" || health.Revision != "0123456789abcdef" || health.Code != "" {
		t.Fatalf("persisted production health=%#v", health)
	}

	resp = requestKBase(handler, http.MethodGet, "/api/books", "agent-secret")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("agent token on admin route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestKBase(handler, http.MethodGet, "/api/books", "admin-secret")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin token on admin route status = %d, body=%s", resp.Code, resp.Body.String())
	}
	browserReq := httptest.NewRequest(http.MethodGet, "/browser/session-token", nil)
	browserReq.Header.Set("Authorization", "Bearer agent-secret")
	browserReq.Header.Set("X-KBase-Browser-Session", "1")
	browserResp := httptest.NewRecorder()
	handler.ServeHTTP(browserResp, browserReq)
	if browserResp.Code != http.StatusGone || strings.Contains(browserResp.Body.String(), "admin-secret") {
		t.Fatalf("agent token exchanged for browser token: status=%d body=%s", browserResp.Code, browserResp.Body.String())
	}

	unconfigured := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:      NewBookKnowledgeStore(t.TempDir()),
		AuthToken:  "admin-secret",
		SourceSync: sourceSync,
	})
	resp = requestJSONKBase(unconfigured, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured agent auth status = %d, body=%s", resp.Code, resp.Body.String())
	}

	sharedToken := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(t.TempDir()),
		AuthToken:        "shared-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "shared-secret",
	})
	resp = requestJSONKBase(sharedToken, http.MethodPost, "/api/source-agent/heartbeat", "shared-secret", heartbeat)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("shared admin/agent token status = %d, body=%s", resp.Code, resp.Body.String())
	}
	resp = requestKBase(sharedToken, http.MethodGet, "/api/books", "shared-secret")
	if resp.Code != http.StatusOK {
		t.Fatalf("shared-token defense disabled admin API: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPResearchWorkerAuthenticationAndValidation(t *testing.T) {
	root := t.TempDir()
	researchStore, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = researchStore.Close() })
	run := createResearchRunForTest(t, researchStore, "research-worker-http")
	job, _, err := researchStore.CreateWorkerJob(ResearchWorkerJobInput{
		RunID: run.RunID, TargetAgentID: "chatlog-agent-a", Tool: ResearchWorkerToolFetchChatMessage,
		Arguments: []byte(`{"message_ref":"message-1","conversation_ref":"conversation-1","time":"2026-08-13"}`), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret",
		SourceAgentToken: "worker-secret", ResearchStore: researchStore,
	})
	claimBody := `{"agent_id":"chatlog-agent-a","lease_seconds":60}`
	for _, token := range []string{"", "admin-secret"} {
		response := requestJSONKBase(handler, http.MethodPost, "/api/research-worker/jobs/claim", token, claimBody)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token=%q status=%d body=%s", token, response.Code, response.Body.String())
		}
	}
	signatureRequest := httptest.NewRequest(http.MethodPost, "/api/research-worker/jobs/claim", strings.NewReader(claimBody))
	signatureRequest.Header.Set("Content-Type", "application/json")
	signatureRequest.Header.Set("X-Research-Signature", "not-an-auth-mechanism")
	signatureResponse := httptest.NewRecorder()
	handler.ServeHTTP(signatureResponse, signatureRequest)
	if signatureResponse.Code != http.StatusUnauthorized {
		t.Fatalf("signature-only status=%d body=%s", signatureResponse.Code, signatureResponse.Body.String())
	}

	unknown := requestJSONKBase(handler, http.MethodPost, "/api/research-worker/jobs/claim", "worker-secret",
		`{"agent_id":"chatlog-agent-a","lease_seconds":60,"unknown":"private"}`)
	if unknown.Code != http.StatusBadRequest || strings.Contains(unknown.Body.String(), "private") {
		t.Fatalf("unknown status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	overLimit := requestJSONKBase(handler, http.MethodPost, "/api/research-worker/jobs/claim", "worker-secret",
		`{"agent_id":"chatlog-agent-a","lease_seconds":3601}`)
	if overLimit.Code != http.StatusBadRequest {
		t.Fatalf("over-limit status=%d body=%s", overLimit.Code, overLimit.Body.String())
	}
	tooLarge := requestJSONKBase(handler, http.MethodPost, "/api/research-worker/jobs/claim", "worker-secret",
		`{"agent_id":"`+strings.Repeat("x", int(defaultResearchWorkerHTTPMaxBodyBytes))+`"}`)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status=%d body=%s", tooLarge.Code, tooLarge.Body.String())
	}

	claimed := requestJSONKBase(handler, http.MethodPost, "/api/research-worker/jobs/claim", "worker-secret", claimBody)
	if claimed.Code != http.StatusOK || !strings.Contains(claimed.Body.String(), `"job_id":"`+job.JobID+`"`) {
		t.Fatalf("claimed status=%d body=%s", claimed.Code, claimed.Body.String())
	}
	foreignComplete := requestJSONKBase(handler, http.MethodPost,
		"/api/research-worker/jobs/"+url.PathEscape(job.JobID)+"/complete", "worker-secret",
		`{"agent_id":"chatlog-agent-b","request_hash":"`+job.RequestHash+`","result":{"items":[]}}`)
	if foreignComplete.Code != http.StatusForbidden {
		t.Fatalf("foreign completion status=%d body=%s", foreignComplete.Code, foreignComplete.Body.String())
	}
	invalidTime := requestJSONKBase(handler, http.MethodPost,
		"/api/research-worker/jobs/"+url.PathEscape(job.JobID)+"/complete", "worker-secret",
		`{"agent_id":"chatlog-agent-a","request_hash":"`+job.RequestHash+`","result":{"items":[{"source_type":"chatlog_message","source_role":"direct_advice","occurred_at":"not-a-time","content":"bounded","locator":{"worker_id":"worker-fixture","conversation_ref":"conversation","message_ref":"message"},"privacy":"private","selected":true}]}}`)
	if invalidTime.Code != http.StatusBadRequest {
		t.Fatalf("invalid time status=%d body=%s", invalidTime.Code, invalidTime.Body.String())
	}

	disabled := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(t.TempDir()), AuthToken: "admin-secret", SourceAgentToken: "worker-secret",
	})
	unavailable := requestJSONKBase(disabled, http.MethodPost, "/api/research-worker/jobs/claim", "worker-secret", claimBody)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status=%d body=%s", unavailable.Code, unavailable.Body.String())
	}
}

func TestKBaseHTTPResearchRunLifecycleBearerCompatibilityAndRedaction(t *testing.T) {
	root := t.TempDir()
	researchStore, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = researchStore.Close() })
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", ResearchStore: researchStore,
		ResearchQuickBudget: ResearchBudget{MaxIterations: 2, MaxEvidenceItems: 20, MaxQuotedChars: 4000, MaxModelCalls: 2, MaxCostUSD: 1},
		ResearchDeepBudget:  ResearchBudget{MaxIterations: 8, MaxEvidenceItems: 200, MaxQuotedChars: 40000, MaxModelCalls: 12, MaxCostUSD: 8},
	})

	createRequest := httptest.NewRequest(http.MethodPost, "/api/research/runs", strings.NewReader(
		`{"mode":"auto","question":"比较去年与现在的建议","requested_sources":["knowledge","chatlog"],"subject_ids":["subject-private"]}`,
	))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", "Bearer admin-secret")
	createRequest.Header.Set("Idempotency-Key", "research-http-one")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var payload struct {
		Created bool `json:"created"`
		Run     struct {
			RunID  string `json:"run_id"`
			Mode   string `json:"mode"`
			Status string `json:"status"`
		} `json:"run"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Created || payload.Run.RunID == "" || payload.Run.Mode != ResearchModeDeep || payload.Run.Status != string(ResearchPlanning) {
		t.Fatalf("create payload=%#v", payload)
	}
	for _, private := range []string{"subject-private", `"subject_ids"`, `"lease_owner"`, `"summary"`, `"actor"`} {
		if strings.Contains(created.Body.String(), private) {
			t.Fatalf("create response exposed %q: %s", private, created.Body.String())
		}
	}

	replayRequest := httptest.NewRequest(http.MethodPost, "/api/research/runs", strings.NewReader(
		`{"mode":"auto","question":"比较去年与现在的建议","requested_sources":["knowledge","chatlog"],"subject_ids":["subject-private"]}`,
	))
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Authorization", "Bearer admin-secret")
	replayRequest.Header.Set("Idempotency-Key", "research-http-one")
	replayed := httptest.NewRecorder()
	handler.ServeHTTP(replayed, replayRequest)
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"created":false`) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}

	detail := requestKBase(handler, http.MethodGet, "/api/research/runs/"+url.PathEscape(payload.Run.RunID), "admin-secret")
	if detail.Code != http.StatusAccepted || !strings.Contains(detail.Body.String(), `"run_id":"`+payload.Run.RunID+`"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	events := requestKBase(handler, http.MethodGet, "/api/research/runs/"+url.PathEscape(payload.Run.RunID)+"/events?after=0", "admin-secret")
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"code":"run_created"`) || strings.Contains(events.Body.String(), `"actor"`) || strings.Contains(events.Body.String(), `"summary"`) {
		t.Fatalf("events status=%d body=%s", events.Code, events.Body.String())
	}

	if _, err := researchStore.db.Exec(`INSERT INTO research_identity_bindings
		(binding_id, run_id, identity_id, source_type, source_identity_hash, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "binding-one", payload.Run.RunID, "person-one", "chatlog", "private-hash", 0.7, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	confirmed := requestJSONKBase(handler, http.MethodPost,
		"/api/research/runs/"+url.PathEscape(payload.Run.RunID)+"/identity-bindings/binding-one/confirm",
		"admin-secret", `{}`)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"confirmed":true`) || strings.Contains(confirmed.Body.String(), "private-hash") {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	canceled := requestJSONKBase(handler, http.MethodPost, "/api/research/runs/"+url.PathEscape(payload.Run.RunID)+"/cancel", "admin-secret", `{}`)
	if canceled.Code != http.StatusAccepted || !strings.Contains(canceled.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel status=%d body=%s", canceled.Code, canceled.Body.String())
	}
	terminal := requestKBase(handler, http.MethodGet, "/api/research/runs/"+url.PathEscape(payload.Run.RunID), "admin-secret")
	if terminal.Code != http.StatusOK {
		t.Fatalf("terminal detail status=%d body=%s", terminal.Code, terminal.Body.String())
	}
}

func TestKBaseHTTPResearchRunCookieCSRFAndOwnership(t *testing.T) {
	clock := &browserSessionTestClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 903)
	root := t.TempDir()
	researchStore, err := OpenResearchStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = researchStore.Close() })
	concrete := handler.(*kbaseHTTPHandler)
	concrete.researchStore = researchStore
	if err := migrateResearchHTTP(researchStore.db); err != nil {
		t.Fatal(err)
	}
	concrete.researchQuickBudget = ResearchBudget{MaxIterations: 2, MaxEvidenceItems: 20, MaxQuotedChars: 4000, MaxModelCalls: 2, MaxCostUSD: 1}
	concrete.researchDeepBudget = ResearchBudget{MaxIterations: 8, MaxEvidenceItems: 200, MaxQuotedChars: 40000, MaxModelCalls: 12, MaxCostUSD: 8}
	first := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Research Browser One")
	second := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Research Browser Two")
	csrf, _ := loadKBaseBrowserSessionCSRF(t, handler, first.Token)
	body := `{"mode":"quick","question":"只检索当前知识库"}`

	missingCSRF := newKBaseBrowserCookieRequest(http.MethodPost, "/api/research/runs", first.Token, body)
	missingCSRF.Header.Set("Idempotency-Key", "cookie-run")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	create := newKBaseBrowserCookieRequest(http.MethodPost, "/api/research/runs", first.Token, body)
	create.Header.Set("Idempotency-Key", "cookie-run")
	addKBaseBrowserSessionSecurityHeaders(create, csrf)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("cookie create status=%d body=%s", created.Code, created.Body.String())
	}
	var payload struct {
		Run struct {
			RunID string `json:"run_id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	foreign := requestKBaseWithBrowserCookie(handler, http.MethodGet, "/api/research/runs/"+url.PathEscape(payload.Run.RunID), second.Token, "")
	if foreign.Code != http.StatusNotFound || foreign.Body.String() != "{\"error\":\"research_run_not_found\"}\n" {
		t.Fatalf("foreign detail status=%d body=%s", foreign.Code, foreign.Body.String())
	}
}

func TestKBaseHTTPResearchRunValidationAndMethods(t *testing.T) {
	root := t.TempDir()
	researchStore, err := OpenResearchStore(root, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = researchStore.Close() })
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", ResearchStore: researchStore})
	for _, test := range []struct {
		name, method, path, body, key string
		want                          int
	}{
		{name: "missing idempotency", method: http.MethodPost, path: "/api/research/runs", body: `{"question":"one"}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPost, path: "/api/research/runs", body: `{"question":"one","private":"secret"}`, key: "unknown", want: http.StatusBadRequest},
		{name: "oversized", method: http.MethodPost, path: "/api/research/runs", body: `{"question":"` + strings.Repeat("x", int(defaultResearchRunHTTPMaxBodyBytes)) + `"}`, key: "large", want: http.StatusRequestEntityTooLarge},
		{name: "collection method", method: http.MethodDelete, path: "/api/research/runs", want: http.StatusMethodNotAllowed},
		{name: "bad cursor", method: http.MethodGet, path: "/api/research/runs/missing/events?after=private", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer admin-secret")
			if test.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if test.key != "" {
				req.Header.Set("Idempotency-Key", test.key)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s want=%d", response.Code, response.Body.String(), test.want)
			}
			for _, private := range []string{"secret", "private", researchStore.dbPath} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("error leaked %q: %s", private, response.Body.String())
				}
			}
		})
	}
}

func TestKBaseHTTPHandlerSourceAgentControl(t *testing.T) {
	handler, sourceSync, _, _ := newKBaseSourceAgentCommandHTTPFixture(t)

	detail := requestKBase(handler, http.MethodGet, "/api/source-agents/agent-a", "admin-secret")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"agent_id":"agent-a"`) ||
		!strings.Contains(detail.Body.String(), `"desired_state":"active"`) {
		t.Fatalf("agent detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	for _, desired := range []string{SourceAgentDesiredPaused, SourceAgentDesiredActive} {
		response := requestJSONKBase(
			handler, http.MethodPost, "/api/source-agents/agent-a/desired-state", "admin-secret",
			`{"desired_state":"`+desired+`"}`,
		)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"desired_state":"`+desired+`"`) {
			t.Fatalf("set desired state %q status=%d body=%s", desired, response.Code, response.Body.String())
		}
	}
	agent, err := sourceSync.GetSourceAgent(" agent-a ")
	if err != nil || agent.DesiredState != SourceAgentDesiredActive {
		t.Fatalf("GetSourceAgent() = %#v, %v", agent, err)
	}

	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "unknown field", method: http.MethodPost, path: "/api/source-agents/agent-a/desired-state", body: `{"desired_state":"paused","extra":"private"}`, want: http.StatusBadRequest},
		{name: "trailing JSON", method: http.MethodPost, path: "/api/source-agents/agent-a/desired-state", body: `{"desired_state":"paused"}{"extra":true}`, want: http.StatusBadRequest},
		{name: "invalid state", method: http.MethodPost, path: "/api/source-agents/agent-a/desired-state", body: `{"desired_state":"reboot"}`, want: http.StatusBadRequest},
		{name: "unknown agent", method: http.MethodGet, path: "/api/source-agents/missing-agent", want: http.StatusNotFound},
		{name: "invalid escaped agent", method: http.MethodGet, path: "/api/source-agents/%2E%2E", want: http.StatusBadRequest},
		{name: "detail method", method: http.MethodDelete, path: "/api/source-agents/agent-a", want: http.StatusMethodNotAllowed},
		{name: "unknown action", method: http.MethodPost, path: "/api/source-agents/agent-a/reboot", body: `{}`, want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response *httptest.ResponseRecorder
			if test.body == "" {
				response = requestKBase(handler, test.method, test.path, "admin-secret")
			} else {
				response = requestJSONKBase(handler, test.method, test.path, "admin-secret", test.body)
			}
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), test.want)
			}
			for _, secret := range []string{"private", "reboot", "../", sourceSync.DBPath()} {
				if secret != "" && strings.Contains(response.Body.String(), secret) {
					t.Fatalf("error response leaked %q: %s", secret, response.Body.String())
				}
			}
		})
	}
}

func TestKBaseHTTPHandlerSourceAgentCommands(t *testing.T) {
	t.Run("restricted restart end to end", func(t *testing.T) {
		handler, sourceSync, clock, browserSessions := newKBaseSourceAgentCommandHTTPFixture(t)
		if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID: "agent-a", WorkerType: "book-job-worker", Platform: "darwin", Architecture: "arm64",
			Version: "1.0.0", ProtocolVersion: "2026-08-01",
			Capabilities: []string{"book_jobs", "diagnose", "controlled_restart"},
		}); err != nil {
			t.Fatal(err)
		}
		expiresAt := clock.Now().Add(time.Hour).Format(time.RFC3339Nano)
		body := `{"type":"restart","idempotency_key":"restart-http","expires_at":"` + expiresAt + `"}`
		bearerRestart := requestJSONKBase(handler, http.MethodPost, "/api/source-agents/agent-a/commands", "admin-secret", body)
		if bearerRestart.Code != http.StatusForbidden || bearerRestart.Body.String() != "{\"error\":\"browser management session required\"}\n" {
			t.Fatalf("Bearer restart status=%d body=%s", bearerRestart.Code, bearerRestart.Body.String())
		}
		credentials, err := createBrowserSessionForTest(browserSessions, BrowserSessionCreate{DeviceLabel: "Restart Browser"})
		if err != nil {
			t.Fatal(err)
		}
		missingCSRF := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, body)
		missingCSRFResponse := httptest.NewRecorder()
		handler.ServeHTTP(missingCSRFResponse, missingCSRF)
		if missingCSRFResponse.Code != http.StatusForbidden {
			t.Fatalf("Cookie restart without CSRF status=%d body=%s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
		}
		createdRequest := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, body)
		addKBaseBrowserSessionSecurityHeaders(createdRequest, credentials.CSRFToken)
		created := httptest.NewRecorder()
		handler.ServeHTTP(created, createdRequest)
		var createdPayload struct {
			Command SourceAgentCommand `json:"command"`
		}
		if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &createdPayload) != nil ||
			createdPayload.Command.Type != SourceAgentCommandRestart {
			t.Fatalf("create restart status=%d body=%s", created.Code, created.Body.String())
		}
		claimed := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", `{"agent_id":"agent-a"}`)
		if claimed.Code != http.StatusOK || !strings.Contains(claimed.Body.String(), `"id":"`+createdPayload.Command.ID+`"`) ||
			!strings.Contains(claimed.Body.String(), `"state":"claimed"`) {
			t.Fatalf("claim restart status=%d body=%s", claimed.Code, claimed.Body.String())
		}
		completed := requestJSONKBase(
			handler, http.MethodPost,
			"/api/source-agent/commands/"+url.PathEscape(createdPayload.Command.ID)+"/complete", "agent-secret",
			`{"agent_id":"agent-a","state":"succeeded","code":"restart_complete"}`,
		)
		if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), `"result_code":"restart_complete"`) {
			t.Fatalf("complete restart status=%d body=%s", completed.Code, completed.Body.String())
		}
		withPayloadRequest := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token,
			`{"type":"restart","idempotency_key":"restart-payload","payload":{},"expires_at":"`+expiresAt+`"}`,
		)
		addKBaseBrowserSessionSecurityHeaders(withPayloadRequest, credentials.CSRFToken)
		withPayload := httptest.NewRecorder()
		handler.ServeHTTP(withPayload, withPayloadRequest)
		if withPayload.Code != http.StatusBadRequest {
			t.Fatalf("restart payload status=%d body=%s", withPayload.Code, withPayload.Body.String())
		}
		withoutCapabilityRequest := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/source-agents/agent-b/commands", credentials.Token,
			`{"type":"restart","idempotency_key":"restart-no-capability","expires_at":"`+expiresAt+`"}`,
		)
		addKBaseBrowserSessionSecurityHeaders(withoutCapabilityRequest, credentials.CSRFToken)
		withoutCapability := httptest.NewRecorder()
		handler.ServeHTTP(withoutCapability, withoutCapabilityRequest)
		if withoutCapability.Code != http.StatusConflict {
			t.Fatalf("restart without capability status=%d body=%s", withoutCapability.Code, withoutCapability.Body.String())
		}
	})

	t.Run("management command API", func(t *testing.T) {
		handler, _, clock, browserSessions := newKBaseSourceAgentCommandHTTPFixture(t)
		expiresAt := clock.Now().Add(time.Hour).Format(time.RFC3339Nano)
		diagnoseBody := `{"type":"diagnose","idempotency_key":"diag-1","expires_at":"` + expiresAt + `"}`
		diagnose := requestJSONKBase(handler, http.MethodPost, "/api/source-agents/agent-a/commands", "admin-secret", diagnoseBody)
		if diagnose.Code != http.StatusCreated || !strings.Contains(diagnose.Body.String(), `"type":"diagnose"`) ||
			!strings.Contains(diagnose.Body.String(), `"target_agent_id":"agent-a"`) {
			t.Fatalf("diagnose status=%d body=%s", diagnose.Code, diagnose.Body.String())
		}

		upgradeBody := `{"type":"upgrade","idempotency_key":"upgrade-1","payload":{"artifact_id":"artifact-2","expected_current_version":"1.0.0"},"expires_at":"` + expiresAt + `"}`
		bearerUpgrade := requestJSONKBase(handler, http.MethodPost, "/api/source-agents/agent-a/commands", "admin-secret", upgradeBody)
		if bearerUpgrade.Code != http.StatusForbidden || bearerUpgrade.Body.String() != "{\"error\":\"browser management session required\"}\n" {
			t.Fatalf("Bearer upgrade status=%d body=%s", bearerUpgrade.Code, bearerUpgrade.Body.String())
		}

		credentials, err := createBrowserSessionForTest(browserSessions, BrowserSessionCreate{DeviceLabel: "Upgrade Browser"})
		if err != nil {
			t.Fatal(err)
		}
		missingCSRF := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, upgradeBody)
		missingCSRFResponse := httptest.NewRecorder()
		handler.ServeHTTP(missingCSRFResponse, missingCSRF)
		if missingCSRFResponse.Code != http.StatusForbidden {
			t.Fatalf("Cookie upgrade without CSRF status=%d body=%s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
		}

		clock.Advance(time.Second)
		validUpgrade := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, upgradeBody)
		addKBaseBrowserSessionSecurityHeaders(validUpgrade, credentials.CSRFToken)
		validUpgradeResponse := httptest.NewRecorder()
		handler.ServeHTTP(validUpgradeResponse, validUpgrade)
		if validUpgradeResponse.Code != http.StatusCreated || !strings.Contains(validUpgradeResponse.Body.String(), `"artifact_id":"artifact-2"`) {
			t.Fatalf("Cookie upgrade status=%d body=%s", validUpgradeResponse.Code, validUpgradeResponse.Body.String())
		}

		list := requestKBase(handler, http.MethodGet, "/api/source-agents/agent-a/commands?limit=1", "admin-secret")
		var listPayload struct {
			Commands []SourceAgentCommand `json:"commands"`
		}
		if list.Code != http.StatusOK || json.Unmarshal(list.Body.Bytes(), &listPayload) != nil ||
			len(listPayload.Commands) != 1 || listPayload.Commands[0].Type != SourceAgentCommandUpgrade {
			t.Fatalf("command list status=%d body=%s", list.Code, list.Body.String())
		}

		for _, test := range []struct {
			name       string
			agentID    string
			body       string
			cookieAuth bool
			want       int
			forbidden  []string
		}{
			{name: "unknown field", agentID: "agent-a", body: `{"type":"diagnose","idempotency_key":"bad-field","expires_at":"` + expiresAt + `","secret_field":"private-value"}`, want: http.StatusBadRequest, forbidden: []string{"secret_field", "private-value"}},
			{name: "target spoof", agentID: "agent-a", body: `{"type":"diagnose","idempotency_key":"spoof","target_agent_id":"agent-b","expires_at":"` + expiresAt + `"}`, want: http.StatusBadRequest, forbidden: []string{"target_agent_id", "agent-b"}},
			{name: "trailing JSON", agentID: "agent-a", body: diagnoseBody + `{"spec_json":"private"}`, want: http.StatusBadRequest, forbidden: []string{"spec_json", "private"}},
			{name: "unknown type", agentID: "agent-a", body: `{"type":"shell","idempotency_key":"unknown-type","expires_at":"` + expiresAt + `"}`, want: http.StatusBadRequest, forbidden: []string{"shell"}},
			{name: "unknown agent", agentID: "missing-agent", body: diagnoseBody, want: http.StatusNotFound, forbidden: []string{"missing-agent"}},
			{name: "stale version", agentID: "agent-b", cookieAuth: true, body: `{"type":"upgrade","idempotency_key":"stale","payload":{"artifact_id":"private-artifact","expected_current_version":"0.9.0"},"expires_at":"` + expiresAt + `"}`, want: http.StatusConflict, forbidden: []string{"private-artifact", "0.9.0", "1.0.0", "expected", "actual", "spec"}},
			{name: "duplicate active upgrade", agentID: "agent-a", cookieAuth: true, body: `{"type":"upgrade","idempotency_key":"upgrade-2","payload":{"artifact_id":"artifact-3","expected_current_version":"1.0.0"},"expires_at":"` + expiresAt + `"}`, want: http.StatusConflict, forbidden: []string{"artifact-3", "expected", "spec"}},
			{name: "idempotency conflict", agentID: "agent-a", body: `{"type":"diagnose","idempotency_key":"diag-1","expires_at":"` + clock.Now().Add(2*time.Hour).Format(time.RFC3339Nano) + `"}`, want: http.StatusConflict, forbidden: []string{"diag-1", "expires_at"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				path := "/api/source-agents/" + test.agentID + "/commands"
				var response *httptest.ResponseRecorder
				if test.cookieAuth {
					request := newKBaseBrowserCookieRequest(http.MethodPost, path, credentials.Token, test.body)
					addKBaseBrowserSessionSecurityHeaders(request, credentials.CSRFToken)
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, request)
				} else {
					response = requestJSONKBase(handler, http.MethodPost, path, "admin-secret", test.body)
				}
				if response.Code != test.want {
					t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), test.want)
				}
				for _, forbidden := range append(test.forbidden, "/Users/", `C:\\Users\\`) {
					if strings.Contains(response.Body.String(), forbidden) {
						t.Fatalf("error response leaked %q: %s", forbidden, response.Body.String())
					}
				}
			})
		}

		invalidID := requestKBase(handler, http.MethodGet, "/api/source-agents/%2E%2E/commands", "admin-secret")
		if invalidID.Code != http.StatusBadRequest {
			t.Fatalf("invalid escaped agent status=%d body=%s", invalidID.Code, invalidID.Body.String())
		}
		wrongMethod := requestKBase(handler, http.MethodDelete, "/api/source-agents/agent-a/commands", "admin-secret")
		if wrongMethod.Code != http.StatusMethodNotAllowed {
			t.Fatalf("wrong command method status=%d body=%s", wrongMethod.Code, wrongMethod.Body.String())
		}
	})

	t.Run("worker command API", func(t *testing.T) {
		handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
		diagnose := mustCreateSourceAgentDiagnoseCommand(t, sourceSync, clock, "agent-a", "worker-diagnose", time.Hour)
		clock.Advance(time.Second)
		upgrade := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "worker-upgrade")

		claimNext := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", `{"agent_id":"agent-a"}`)
		if claimNext.Code != http.StatusOK || !strings.Contains(claimNext.Body.String(), `"id":"`+diagnose.ID+`"`) ||
			!strings.Contains(claimNext.Body.String(), `"state":"claimed"`) {
			t.Fatalf("claim next status=%d body=%s", claimNext.Code, claimNext.Body.String())
		}
		claimByIDBody := `{"agent_id":"agent-a","command_id":"` + diagnose.ID + `"}`
		claimByID := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", claimByIDBody)
		if claimByID.Code != http.StatusOK || !strings.Contains(claimByID.Body.String(), `"id":"`+diagnose.ID+`"`) {
			t.Fatalf("idempotent claim by id status=%d body=%s", claimByID.Code, claimByID.Body.String())
		}

		wrongTarget := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", `{"agent_id":"agent-b","command_id":"`+diagnose.ID+`"}`)
		if wrongTarget.Code != http.StatusForbidden || strings.Contains(wrongTarget.Body.String(), diagnose.ID) || strings.Contains(wrongTarget.Body.String(), "agent-a") {
			t.Fatalf("wrong target status=%d body=%s", wrongTarget.Code, wrongTarget.Body.String())
		}
		empty := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", `{"agent_id":"agent-b"}`)
		if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"command":null}` {
			t.Fatalf("empty queue status=%d body=%s", empty.Code, empty.Body.String())
		}
		legacyResultCode := requestJSONKBase(
			handler,
			http.MethodPost,
			"/api/source-agent/commands/"+url.PathEscape(diagnose.ID)+"/complete",
			"agent-secret",
			`{"agent_id":"agent-a","state":"succeeded","result_code":"diagnostic_complete"}`,
		)
		if legacyResultCode.Code != http.StatusBadRequest {
			t.Fatalf("legacy result_code status=%d body=%s", legacyResultCode.Code, legacyResultCode.Body.String())
		}

		claimUpgrade := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "agent-secret", `{"agent_id":"agent-a","command_id":"`+upgrade.ID+`"}`)
		if claimUpgrade.Code != http.StatusOK {
			t.Fatalf("claim upgrade status=%d body=%s", claimUpgrade.Code, claimUpgrade.Body.String())
		}
		recoverOwned := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/recover", "agent-secret", `{"agent_id":"agent-a"}`)
		if recoverOwned.Code != http.StatusOK || !strings.Contains(recoverOwned.Body.String(), `"id":"`+upgrade.ID+`"`) {
			t.Fatalf("recover owned status=%d body=%s", recoverOwned.Code, recoverOwned.Body.String())
		}
		resumeExact := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/recover", "agent-secret", `{"agent_id":"agent-a","command_id":"`+upgrade.ID+`"}`)
		if resumeExact.Code != http.StatusOK || !strings.Contains(resumeExact.Body.String(), `"state":"claimed"`) {
			t.Fatalf("resume exact status=%d body=%s", resumeExact.Code, resumeExact.Body.String())
		}
		foreignResume := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/recover", "agent-secret", `{"agent_id":"agent-b","command_id":"`+upgrade.ID+`"}`)
		if foreignResume.Code != http.StatusForbidden || strings.Contains(foreignResume.Body.String(), upgrade.ID) {
			t.Fatalf("foreign resume status=%d body=%s", foreignResume.Code, foreignResume.Body.String())
		}
		commandPath := "/api/source-agent/commands/" + url.PathEscape(upgrade.ID)
		progress := requestJSONKBase(handler, http.MethodPost, commandPath+"/progress", "agent-secret", `{"agent_id":"agent-a","state":"downloading","message":"downloading"}`)
		if progress.Code != http.StatusOK || !strings.Contains(progress.Body.String(), `"state":"downloading"`) {
			t.Fatalf("download progress status=%d body=%s", progress.Code, progress.Body.String())
		}
		badComplete := requestJSONKBase(handler, http.MethodPost, commandPath+"/complete", "agent-secret", `{"agent_id":"agent-a","state":"downloading"}`)
		if badComplete.Code != http.StatusBadRequest {
			t.Fatalf("nonterminal complete status=%d body=%s", badComplete.Code, badComplete.Body.String())
		}
		for _, state := range []string{
			SourceAgentCommandVerified,
			SourceAgentCommandInstalling,
			SourceAgentCommandRestarting,
			SourceAgentCommandVerifying,
		} {
			response := requestJSONKBase(handler, http.MethodPost, commandPath+"/progress", "agent-secret", `{"agent_id":"agent-a","state":"`+state+`"}`)
			if response.Code != http.StatusOK {
				t.Fatalf("progress %s status=%d body=%s", state, response.Code, response.Body.String())
			}
		}
		completionBody := `{"agent_id":"agent-a","state":"succeeded","code":"upgrade_complete","message":"installed","actual_version":"2.0.0"}`
		complete := requestJSONKBase(handler, http.MethodPost, commandPath+"/complete", "agent-secret", completionBody)
		if complete.Code != http.StatusOK || !strings.Contains(complete.Body.String(), `"state":"succeeded"`) {
			t.Fatalf("complete status=%d body=%s", complete.Code, complete.Body.String())
		}
		duplicate := requestJSONKBase(handler, http.MethodPost, commandPath+"/complete", "agent-secret", completionBody)
		if duplicate.Code != http.StatusOK {
			t.Fatalf("duplicate complete status=%d body=%s", duplicate.Code, duplicate.Body.String())
		}
		badProgress := requestJSONKBase(handler, http.MethodPost, commandPath+"/progress", "agent-secret", completionBody)
		if badProgress.Code != http.StatusBadRequest {
			t.Fatalf("terminal progress status=%d body=%s", badProgress.Code, badProgress.Body.String())
		}
		terminalResume := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/recover", "agent-secret", `{"agent_id":"agent-a","command_id":"`+upgrade.ID+`"}`)
		if terminalResume.Code != http.StatusOK || !strings.Contains(terminalResume.Body.String(), `"state":"succeeded"`) {
			t.Fatalf("terminal resume status=%d body=%s", terminalResume.Code, terminalResume.Body.String())
		}
		noActiveUpgrade := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/recover", "agent-secret", `{"agent_id":"agent-a"}`)
		if noActiveUpgrade.Code != http.StatusOK || strings.TrimSpace(noActiveUpgrade.Body.String()) != `{"command":null}` {
			t.Fatalf("terminal command recovered as active: status=%d body=%s", noActiveUpgrade.Code, noActiveUpgrade.Body.String())
		}

		for _, test := range []struct {
			name string
			path string
			body string
		}{
			{name: "claim unknown field", path: "/api/source-agent/commands/claim", body: `{"agent_id":"agent-a","target_agent_id":"agent-b"}`},
			{name: "claim trailing", path: "/api/source-agent/commands/claim", body: `{"agent_id":"agent-a"}{"secret":"private"}`},
			{name: "recover unknown field", path: "/api/source-agent/commands/recover", body: `{"agent_id":"agent-a","owner":"private"}`},
			{name: "report unknown field", path: commandPath + "/complete", body: `{"agent_id":"agent-a","state":"succeeded","code":"upgrade_complete","actual_version":"2.0.0","spec_json":"private"}`},
		} {
			t.Run(test.name, func(t *testing.T) {
				response := requestJSONKBase(handler, http.MethodPost, test.path, "agent-secret", test.body)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				for _, forbidden := range []string{"target_agent_id", "agent-b", "spec_json", "private"} {
					if strings.Contains(response.Body.String(), forbidden) {
						t.Fatalf("strict JSON error leaked %q: %s", forbidden, response.Body.String())
					}
				}
			})
		}

		unknown := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/missing-command/complete", "agent-secret", `{"agent_id":"agent-a","state":"failed","code":"upgrade_failed"}`)
		if unknown.Code != http.StatusNotFound {
			t.Fatalf("unknown command status=%d body=%s", unknown.Code, unknown.Body.String())
		}
		adminOnWorker := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/commands/claim", "admin-secret", `{"agent_id":"agent-a"}`)
		if adminOnWorker.Code != http.StatusUnauthorized {
			t.Fatalf("management Bearer on worker route status=%d body=%s", adminOnWorker.Code, adminOnWorker.Body.String())
		}
		workerOnManagement := requestKBase(handler, http.MethodGet, "/api/source-agents/agent-a", "agent-secret")
		if workerOnManagement.Code != http.StatusUnauthorized {
			t.Fatalf("worker Bearer on management route status=%d body=%s", workerOnManagement.Code, workerOnManagement.Body.String())
		}
	})
}

func TestKBaseHTTPHandlerSourceAgentArtifactMetadata(t *testing.T) {
	handler, _, _, browserSessions := newKBaseSourceAgentCommandHTTPFixture(t)

	response := requestKBase(handler, http.MethodGet, "/api/source-agent-artifacts?limit=2", "admin-secret")
	if response.Code != http.StatusOK {
		t.Fatalf("Bearer metadata status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Artifacts []SourceAgentArtifactPublic `json:"artifacts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Artifacts) != 2 || payload.Artifacts[0].ID >= payload.Artifacts[1].ID {
		t.Fatalf("artifact metadata = %#v", payload.Artifacts)
	}
	for _, forbidden := range []string{"storage_key", "artifacts/", "catalog.json"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("metadata leaked %q: %s", forbidden, response.Body.String())
		}
	}

	credentials, err := createBrowserSessionForTest(browserSessions, BrowserSessionCreate{DeviceLabel: "Artifact Browser"})
	if err != nil {
		t.Fatal(err)
	}
	cookieRequest := newKBaseBrowserCookieRequest(http.MethodGet, "/api/source-agent-artifacts?limit=1", credentials.Token, "")
	cookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(cookieResponse, cookieRequest)
	if cookieResponse.Code != http.StatusOK {
		t.Fatalf("Cookie metadata status=%d body=%s", cookieResponse.Code, cookieResponse.Body.String())
	}

	for _, test := range []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{name: "anonymous", path: "/api/source-agent-artifacts", want: http.StatusUnauthorized},
		{name: "worker token", path: "/api/source-agent-artifacts", token: "agent-secret", want: http.StatusUnauthorized},
		{name: "duplicate limit", path: "/api/source-agent-artifacts?limit=1&limit=2", token: "admin-secret", want: http.StatusBadRequest},
		{name: "unknown query", path: "/api/source-agent-artifacts?root=private", token: "admin-secret", want: http.StatusBadRequest},
		{name: "negative limit", path: "/api/source-agent-artifacts?limit=-1", token: "admin-secret", want: http.StatusBadRequest},
		{name: "wrong method", path: "/api/source-agent-artifacts", token: "admin-secret", want: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			method := http.MethodGet
			if test.name == "wrong method" {
				method = http.MethodPost
			}
			got := requestKBase(handler, method, test.path, test.token)
			if got.Code != test.want {
				t.Fatalf("status=%d body=%s, want %d", got.Code, got.Body.String(), test.want)
			}
			for _, forbidden := range []string{"private", "root", "storage_key", "catalog.json"} {
				if strings.Contains(got.Body.String(), forbidden) {
					t.Fatalf("error leaked %q: %s", forbidden, got.Body.String())
				}
			}
		})
	}
}

func TestKBaseHTTPHandlerSourceAgentArtifactDownloadIsCommandBound(t *testing.T) {
	handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
	command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "artifact-download")
	claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, "agent-a", "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID)
	response := requestKBase(handler, http.MethodGet, path, "agent-secret")
	if response.Code != http.StatusOK || response.Body.String() != "artifact-worker-bytes" {
		t.Fatalf("download status=%d body=%q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len("artifact-worker-bytes")) {
		t.Fatalf("Content-Length=%q", got)
	}
	if got := response.Header().Get("X-Source-Agent-Artifact-SHA256"); got != sha256HexForTest([]byte("artifact-worker-bytes")) {
		t.Fatalf("artifact SHA header=%q", got)
	}

	diagnose := mustCreateSourceAgentDiagnoseCommand(t, sourceSync, clock, "agent-a", "artifact-diagnose", time.Hour)
	if _, err := sourceSync.ClaimSourceAgentCommand(diagnose.ID, "agent-a", "agent-a"); err != nil {
		t.Fatal(err)
	}
	queued := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-b", "artifact-worker", "artifact-queued")
	for _, test := range []struct {
		name      string
		path      string
		token     string
		want      int
		forbidden []string
	}{
		{name: "anonymous", path: path, want: http.StatusUnauthorized},
		{name: "admin token", path: path, token: "admin-secret", want: http.StatusUnauthorized},
		{name: "wrong target", path: strings.Replace(path, "agent_id=agent-a", "agent_id=agent-b", 1), token: "agent-secret", want: http.StatusForbidden, forbidden: []string{claimed.ID, "agent-a"}},
		{name: "wrong artifact", path: strings.Replace(path, "artifacts/artifact-worker", "artifacts/artifact-2", 1), token: "agent-secret", want: http.StatusForbidden, forbidden: []string{claimed.ID, "artifact-worker"}},
		{name: "queued", path: "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-b&command_id=" + url.QueryEscape(queued.ID), token: "agent-secret", want: http.StatusConflict, forbidden: []string{queued.ID}},
		{name: "diagnose", path: "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(diagnose.ID), token: "agent-secret", want: http.StatusForbidden, forbidden: []string{diagnose.ID}},
		{name: "duplicate agent", path: path + "&agent_id=agent-a", token: "agent-secret", want: http.StatusBadRequest},
		{name: "unknown query", path: path + "&storage_key=private", token: "agent-secret", want: http.StatusBadRequest, forbidden: []string{"private", "storage_key"}},
		{name: "traversal artifact", path: "/api/source-agent/artifacts/%2E%2E/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID), token: "agent-secret", want: http.StatusBadRequest},
		{name: "noncanonical artifact", path: "/api/source-agent/artifacts/%20artifact-worker%20/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID), token: "agent-secret", want: http.StatusBadRequest},
		{name: "noncanonical agent", path: strings.Replace(path, "agent_id=agent-a", "agent_id=%20agent-a%20", 1), token: "agent-secret", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := requestKBase(handler, http.MethodGet, test.path, test.token)
			if got.Code != test.want {
				t.Fatalf("status=%d body=%s, want %d", got.Code, got.Body.String(), test.want)
			}
			for _, forbidden := range append(test.forbidden, "catalog.json", "artifacts/") {
				if strings.Contains(got.Body.String(), forbidden) {
					t.Fatalf("error leaked %q: %s", forbidden, got.Body.String())
				}
			}
		})
	}

	completed := completeSourceAgentUpgradeCommand(t, sourceSync, claimed.ID, "agent-a", "agent-a", "2.0.0")
	terminalPath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(completed.ID)
	terminal := requestKBase(handler, http.MethodGet, terminalPath, "agent-secret")
	if terminal.Code != http.StatusConflict || strings.Contains(terminal.Body.String(), completed.ID) {
		t.Fatalf("terminal download status=%d body=%s", terminal.Code, terminal.Body.String())
	}

	if _, err := sourceSync.ClaimSourceAgentCommand(queued.ID, "agent-b", "agent-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID: "agent-b", WorkerType: "wechat-worker", Platform: "darwin", Architecture: "amd64",
		Version: "1.0.0", ProtocolVersion: "2026-08-01",
	}); err != nil {
		t.Fatal(err)
	}
	incompatiblePath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-b&command_id=" + url.QueryEscape(queued.ID)
	incompatible := requestKBase(handler, http.MethodGet, incompatiblePath, "agent-secret")
	if incompatible.Code != http.StatusForbidden || strings.Contains(incompatible.Body.String(), queued.ID) {
		t.Fatalf("incompatible registry target status=%d body=%s", incompatible.Code, incompatible.Body.String())
	}

	expiring := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "artifact-expired")
	if _, err := sourceSync.ClaimSourceAgentCommand(expiring.ID, "agent-a", "agent-a"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour)
	expiredPath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(expiring.ID)
	expired := requestKBase(handler, http.MethodGet, expiredPath, "agent-secret")
	if expired.Code != http.StatusConflict || strings.Contains(expired.Body.String(), expiring.ID) {
		t.Fatalf("expired download status=%d body=%s", expired.Code, expired.Body.String())
	}

	stale := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "artifact-stale-version")
	if _, err := sourceSync.ClaimSourceAgentCommand(stale.ID, "agent-a", "agent-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID: "agent-a", WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "1.1.0", ProtocolVersion: "2026-08-01",
	}); err != nil {
		t.Fatal(err)
	}
	stalePath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(stale.ID)
	staleResponse := requestKBase(handler, http.MethodGet, stalePath, "agent-secret")
	if staleResponse.Code != http.StatusConflict || strings.Contains(staleResponse.Body.String(), stale.ID) || strings.Contains(staleResponse.Body.String(), "1.0.0") {
		t.Fatalf("stale-version download status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
}

func TestSourceAgentArtifactHandoffDownloadIncludesCommandBoundCatalogSnapshot(t *testing.T) {
	handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
	command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "artifact-handoff-snapshot")
	claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, "agent-a", "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID)
	response := requestKBase(handler, http.MethodGet, path, "agent-secret")
	if response.Code != http.StatusOK || response.Body.String() != "artifact-worker-bytes" {
		t.Fatalf("download status=%d body=%q", response.Code, response.Body.String())
	}

	wantHeaders := map[string]string{
		"X-Source-Agent-Command-ID":                claimed.ID,
		"X-Source-Agent-Artifact-ID":               "artifact-worker",
		"X-Source-Agent-Artifact-Version":          "2.0.0",
		"X-Source-Agent-Artifact-Worker-Type":      "wechat-worker",
		"X-Source-Agent-Artifact-Platform":         "darwin",
		"X-Source-Agent-Artifact-Architecture":     "arm64",
		"X-Source-Agent-Artifact-Protocol-Version": "2026-08-01",
		"X-Source-Agent-Artifact-Revision":         sourceAgentArtifactTestRevision,
		"X-Source-Agent-Artifact-Channel":          "staging",
		"X-Source-Agent-Artifact-Size":             strconv.Itoa(len("artifact-worker-bytes")),
		"X-Source-Agent-Artifact-SHA256":           sha256HexForTest([]byte("artifact-worker-bytes")),
	}
	for name, want := range wantHeaders {
		t.Run(name, func(t *testing.T) {
			values := response.Header().Values(name)
			if len(values) != 1 || values[0] != want {
				t.Fatalf("%s=%q, want one value %q", name, values, want)
			}
		})
	}
}

func TestSourceAgentArtifactHandoffCatalogReloadCannotMixSnapshotMetadataAndBytes(t *testing.T) {
	handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
	concrete := handler.(*kbaseHTTPHandler)
	command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-a", "artifact-worker", "artifact-snapshot-reload")
	claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, "agent-a", "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	requestPath := "/api/source-agent/artifacts/artifact-worker/download?agent_id=agent-a&command_id=" + url.QueryEscape(claimed.ID)
	release := make(chan struct{})
	writer := &snapshotBindingSourceAgentArtifactWriter{
		header: make(http.Header), entered: make(chan struct{}), release: release,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		request.Header.Set("Authorization", "Bearer agent-secret")
		handler.ServeHTTP(writer, request)
	}()
	select {
	case <-writer.entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("download did not bind its snapshot before response")
	}

	artifacts, err := concrete.sourceArtifacts.load()
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	replacementBytes := []byte("replacement artifact bytes")
	found := false
	for index := range artifacts {
		if artifacts[index].ID != "artifact-worker" {
			continue
		}
		found = true
		artifacts[index].Version = "3.0.0"
		artifacts[index].Revision = strings.Repeat("b", 40)
		artifacts[index].Channel = "production"
		artifacts[index].Size = int64(len(replacementBytes))
		artifacts[index].SHA256 = sha256HexForTest(replacementBytes)
		artifactPath := filepath.Join(concrete.sourceArtifacts.root, filepath.FromSlash(artifacts[index].StorageKey))
		if err := os.WriteFile(artifactPath, replacementBytes, 0o600); err != nil {
			close(release)
			t.Fatal(err)
		}
	}
	if !found {
		close(release)
		t.Fatal("artifact-worker was not found")
	}
	writeSourceAgentArtifactCatalog(t, concrete.sourceArtifacts.root, artifacts)
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot download did not finish")
	}

	if writer.status != http.StatusOK || writer.body.String() != "artifact-worker-bytes" {
		t.Fatalf("status=%d body=%q", writer.status, writer.body.String())
	}
	for name, want := range map[string]string{
		sourceAgentHeaderArtifactVersion:  "2.0.0",
		sourceAgentHeaderArtifactRevision: sourceAgentArtifactTestRevision,
		sourceAgentHeaderArtifactChannel:  "staging",
		sourceAgentHeaderArtifactSize:     strconv.Itoa(len("artifact-worker-bytes")),
		sourceAgentHeaderArtifactSHA256:   sha256HexForTest([]byte("artifact-worker-bytes")),
	} {
		if values := writer.header.Values(name); len(values) != 1 || values[0] != want {
			t.Fatalf("%s=%q want one value %q", name, values, want)
		}
	}
}

type snapshotBindingSourceAgentArtifactWriter struct {
	header      http.Header
	status      int
	body        bytes.Buffer
	entered     chan struct{}
	release     <-chan struct{}
	wroteHeader sync.Once
}

func (w *snapshotBindingSourceAgentArtifactWriter) Header() http.Header { return w.header }

func (w *snapshotBindingSourceAgentArtifactWriter) WriteHeader(status int) {
	w.status = status
	w.wroteHeader.Do(func() { close(w.entered) })
}

func (w *snapshotBindingSourceAgentArtifactWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	<-w.release
	return w.body.Write(data)
}

func TestSourceAgentUpdateGuardIsWorkerAuthenticatedCommandBoundAndSnapshotExact(t *testing.T) {
	type guardFixture struct {
		handler      http.Handler
		sourceSync   *SourceSyncStore
		clock        *sourceSyncTestClock
		command      SourceAgentCommand
		artifactRoot string
	}
	newFixture := func(t *testing.T, state string, ttl time.Duration) guardFixture {
		t.Helper()
		handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
		command, err := sourceSync.CreateSourceAgentCommand(SourceAgentCommandCreate{
			TargetAgentID: "agent-a", Type: SourceAgentCommandUpgrade, IdempotencyKey: "artifact-guard-" + state,
			Payload:   json.RawMessage(`{"artifact_id":"artifact-worker","expected_current_version":"1.0.0"}`),
			ExpiresAt: clock.Now().Add(ttl).Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, "agent-a", "agent-a")
		if err != nil {
			t.Fatal(err)
		}
		commandPath := "/api/source-agent/commands/" + url.PathEscape(claimed.ID) + "/progress"
		for _, next := range []string{SourceAgentCommandDownloading, SourceAgentCommandVerified, SourceAgentCommandInstalling, SourceAgentCommandRestarting} {
			got := requestJSONKBase(handler, http.MethodPost, commandPath, "agent-secret", `{"agent_id":"agent-a","state":"`+next+`"}`)
			if got.Code != http.StatusOK {
				t.Fatalf("progress %s status=%d body=%s", next, got.Code, got.Body.String())
			}
			if next == state {
				break
			}
		}
		stored, err := sourceSync.GetSourceAgentCommand(claimed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return guardFixture{
			handler: handler, sourceSync: sourceSync, clock: clock, command: stored,
			artifactRoot: sourceAgentArtifactRootFromHandlerForTest(t, handler),
		}
	}
	requestGuard := func(t *testing.T, fixture guardFixture, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		path := "/api/source-agent/commands/" + url.PathEscape(fixture.command.ID) + "/guard"
		return requestJSONKBase(fixture.handler, http.MethodPost, path, token, body)
	}
	validFields := func() map[string]any {
		return map[string]any{
			"agent_id": "agent-a", "artifact_id": "artifact-worker",
			"current_version": "1.0.0", "target_version": "2.0.0",
			"revision": sourceAgentArtifactTestRevision, "channel": "staging",
			"size": int64(len("artifact-worker-bytes")), "sha256": sha256HexForTest([]byte("artifact-worker-bytes")),
			"worker_type": "wechat-worker", "platform": "darwin", "architecture": "arm64",
			"protocol_version": "2026-08-01",
		}
	}
	bodyFor := func(t *testing.T, fields map[string]any) string {
		t.Helper()
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	emptyLease := ""
	malformedLease := "not-a-time"
	nonCanonicalLease := "2026-08-01T13:00:00.000000000Z"
	offsetLease := "2026-08-01T09:00:00-04:00"
	expiredLease := "2026-08-01T11:59:59Z"

	t.Run("allows exact owned installing command with no run and sufficient TTL", func(t *testing.T) {
		fixture := newFixture(t, SourceAgentCommandInstalling, time.Hour)
		response := requestGuard(t, fixture, "agent-secret", bodyFor(t, validFields()))
		if response.Code != http.StatusNoContent {
			t.Fatalf("guard status=%d body=%s", response.Code, response.Body.String())
		}
	})

	for _, test := range []struct {
		name      string
		state     string
		token     string
		ttl       time.Duration
		mutate    func(map[string]any)
		disable   bool
		activeRun bool
		lease     *string
		want      int
	}{
		{name: "requires worker authentication", state: SourceAgentCommandInstalling, ttl: time.Hour, want: http.StatusUnauthorized},
		{name: "requires claimed owner", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["agent_id"] = "agent-b" }, want: http.StatusForbidden},
		{name: "requires installing state", state: SourceAgentCommandVerified, token: "agent-secret", ttl: time.Hour, want: http.StatusConflict},
		{name: "rejects restarting state", state: SourceAgentCommandRestarting, token: "agent-secret", ttl: time.Hour, want: http.StatusConflict},
		{name: "requires no active source run", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, want: http.StatusConflict},
		{name: "allows a valid expired source lease", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, lease: &expiredLease, want: http.StatusNoContent},
		{name: "fails closed on empty source lease", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, lease: &emptyLease, want: http.StatusServiceUnavailable},
		{name: "fails closed on malformed source lease", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, lease: &malformedLease, want: http.StatusServiceUnavailable},
		{name: "fails closed on noncanonical source lease", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, lease: &nonCanonicalLease, want: http.StatusServiceUnavailable},
		{name: "fails closed on non-UTC source lease", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, activeRun: true, lease: &offsetLease, want: http.StatusServiceUnavailable},
		{name: "requires allowed rollout", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, disable: true, want: http.StatusConflict},
		{name: "requires restart ready reconcile safety TTL", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Minute, want: http.StatusConflict},
		{name: "requires artifact ID", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["artifact_id"] = "artifact-2" }, want: http.StatusConflict},
		{name: "requires current version", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["current_version"] = "1.0.1" }, want: http.StatusConflict},
		{name: "requires target version", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["target_version"] = "2.0.1" }, want: http.StatusConflict},
		{name: "requires revision", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["revision"] = strings.Repeat("b", 40) }, want: http.StatusConflict},
		{name: "requires channel", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["channel"] = "production" }, want: http.StatusConflict},
		{name: "requires size", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["size"] = int64(len("artifact-worker-bytes") + 1) }, want: http.StatusConflict},
		{name: "requires SHA-256", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["sha256"] = strings.Repeat("0", 64) }, want: http.StatusConflict},
		{name: "requires worker type", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["worker_type"] = "wcplus-worker" }, want: http.StatusConflict},
		{name: "requires platform", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["platform"] = "linux" }, want: http.StatusConflict},
		{name: "requires architecture", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["architecture"] = "amd64" }, want: http.StatusConflict},
		{name: "requires protocol", state: SourceAgentCommandInstalling, token: "agent-secret", ttl: time.Hour, mutate: func(fields map[string]any) { fields["protocol_version"] = "2026-07-01" }, want: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t, test.state, test.ttl)
			if test.disable {
				setSourceAgentArtifactRolloutForTest(t, fixture.artifactRoot, "artifact-worker", false)
			}
			if test.activeRun {
				subscription, err := fixture.sourceSync.CreateSubscription(SourceSubscriptionInput{
					SourceType: "wechat_mp_article", SourceAccountKey: "guard-active-run",
					SourceAccount: "Guard Active Run", AgentID: "agent-a",
					Operation: "sync_articles", Enabled: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				run, err := fixture.sourceSync.CreateRun(subscription.ID, "")
				if err != nil {
					t.Fatal(err)
				}
				leased, err := fixture.sourceSync.LeaseNextRun("agent-a", []string{"sync_articles"}, time.Minute)
				if err != nil || leased == nil || leased.ID != run.ID {
					t.Fatalf("leased=%#v err=%v", leased, err)
				}
				if _, err := fixture.sourceSync.StartRun(run.ID, "agent-a"); err != nil {
					t.Fatal(err)
				}
				if test.lease != nil {
					if _, err := fixture.sourceSync.db.Exec(`UPDATE source_sync_runs SET lease_expires_at = ? WHERE id = ?`, *test.lease, run.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			fields := validFields()
			if test.mutate != nil {
				test.mutate(fields)
			}
			response := requestGuard(t, fixture, test.token, bodyFor(t, fields))
			if response.Code != test.want {
				t.Fatalf("guard status=%d body=%s, want %d", response.Code, response.Body.String(), test.want)
			}
		})
	}
}

func TestKBaseHTTPHandlerSourceAgentArtifactDownloadBoundsConcurrentResponses(t *testing.T) {
	handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
	concrete := handler.(*kbaseHTTPHandler)
	snapshotTempDir := t.TempDir()
	concrete.sourceArtifacts.snapshotTempDir = snapshotTempDir
	if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
		AgentID: "agent-c", WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
		Version: "1.0.0", ProtocolVersion: "2026-08-01",
	}); err != nil {
		t.Fatal(err)
	}
	agentIDs := []string{"agent-a", "agent-b", "agent-c"}
	paths := make([]string, 0, 3)
	for index, agentID := range agentIDs {
		command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, agentID, "artifact-worker", fmt.Sprintf("artifact-bounded-%d", index))
		claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, agentID, agentID)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "/api/source-agent/artifacts/artifact-worker/download?agent_id="+url.QueryEscape(agentID)+"&command_id="+url.QueryEscape(claimed.ID))
	}

	release := make(chan struct{})
	start := func(path string) (<-chan struct{}, context.CancelFunc, <-chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		done := make(chan struct{})
		writer := &blockingSourceAgentArtifactResponseWriter{
			header:  make(http.Header),
			ctx:     ctx,
			entered: entered,
			release: release,
		}
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer agent-secret")
		go func() {
			defer close(done)
			handler.ServeHTTP(writer, request)
		}()
		return entered, cancel, done
	}

	firstEntered, cancelFirst, firstDone := start(paths[0])
	secondEntered, cancelSecond, secondDone := start(paths[1])
	defer cancelFirst()
	defer cancelSecond()
	for index, entered := range []<-chan struct{}{firstEntered, secondEntered} {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatalf("download %d did not enter response", index+1)
		}
	}

	thirdEntered, cancelThird, thirdDone := start(paths[2])
	unexpectedThird := false
	select {
	case <-thirdEntered:
		unexpectedThird = true
	case <-time.After(time.Second):
	}
	cancelThird()
	select {
	case <-thirdDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("canceled third download did not return promptly")
	}

	retryEntered, cancelRetry, retryDone := start(paths[2])
	defer cancelRetry()
	unexpectedRetry := false
	select {
	case <-retryEntered:
		unexpectedRetry = true
	case <-time.After(time.Second):
	}
	cancelFirst()
	select {
	case <-retryEntered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("active download cancellation did not release a snapshot slot")
	}
	cancelRetry()
	select {
	case <-retryDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("canceled active retry did not return promptly")
	}
	close(release)
	for index, done := range []<-chan struct{}{firstDone, secondDone} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("download %d did not finish after release", index+1)
		}
	}
	if unexpectedThird {
		t.Fatal("third concurrent download entered the response before a slot was released")
	}
	if unexpectedRetry {
		t.Fatal("retried third download entered the response before active cancellation released a slot")
	}
	if entries, err := os.ReadDir(snapshotTempDir); err != nil || len(entries) != 0 {
		t.Fatalf("snapshot temp entries after cancellation = %#v, %v", entries, err)
	}
}

func TestKBaseHTTPHandlerSourceAgentArtifactDownloadRevalidatesAfterQueue(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *kbaseHTTPHandler, *SourceSyncStore)
	}{
		{
			name: "rollout disabled",
			mutate: func(t *testing.T, handler *kbaseHTTPHandler, _ *SourceSyncStore) {
				setSourceAgentArtifactRolloutForTest(t, handler.sourceArtifacts.root, "artifact-worker", false)
			},
		},
		{
			name: "registry version changed",
			mutate: func(t *testing.T, _ *kbaseHTTPHandler, sourceSync *SourceSyncStore) {
				if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
					AgentID: "agent-c", WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
					Version: "1.1.0", ProtocolVersion: "2026-08-01",
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, sourceSync, clock, _ := newKBaseSourceAgentCommandHTTPFixture(t)
			concrete := handler.(*kbaseHTTPHandler)
			if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
				AgentID: "agent-c", WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
				Version: "1.0.0", ProtocolVersion: "2026-08-01",
			}); err != nil {
				t.Fatal(err)
			}
			leaseObserved := make(chan struct{}, 3)
			concrete.sourceArtifacts.snapshotLeaseObserver = func() { leaseObserved <- struct{}{} }

			agentIDs := []string{"agent-a", "agent-b", "agent-c"}
			paths := make([]string, 0, len(agentIDs))
			for index, agentID := range agentIDs {
				command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, agentID, "artifact-worker", fmt.Sprintf("artifact-revalidate-%d", index))
				claimed, err := sourceSync.ClaimSourceAgentCommand(command.ID, agentID, agentID)
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, "/api/source-agent/artifacts/artifact-worker/download?agent_id="+url.QueryEscape(agentID)+"&command_id="+url.QueryEscape(claimed.ID))
			}

			release := make(chan struct{})
			startBlocking := func(path string) (<-chan struct{}, context.CancelFunc, <-chan struct{}) {
				ctx, cancel := context.WithCancel(context.Background())
				entered := make(chan struct{})
				done := make(chan struct{})
				writer := &blockingSourceAgentArtifactResponseWriter{
					header: make(http.Header), ctx: ctx, entered: entered, release: release,
				}
				request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
				request.Header.Set("Authorization", "Bearer agent-secret")
				go func() {
					defer close(done)
					handler.ServeHTTP(writer, request)
				}()
				return entered, cancel, done
			}
			firstEntered, cancelFirst, firstDone := startBlocking(paths[0])
			secondEntered, cancelSecond, secondDone := startBlocking(paths[1])
			for _, entered := range []<-chan struct{}{firstEntered, secondEntered} {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					cancelFirst()
					cancelSecond()
					t.Fatal("blocking download did not enter response")
				}
			}
			for index := 0; index < 2; index++ {
				select {
				case <-leaseObserved:
				case <-time.After(time.Second):
					cancelFirst()
					cancelSecond()
					t.Fatal("blocking download lease was not observed")
				}
			}

			thirdCtx, cancelThird := context.WithCancel(context.Background())
			thirdResponse := httptest.NewRecorder()
			thirdRequest := httptest.NewRequest(http.MethodGet, paths[2], nil).WithContext(thirdCtx)
			thirdRequest.Header.Set("Authorization", "Bearer agent-secret")
			thirdDone := make(chan struct{})
			go func() {
				defer close(thirdDone)
				handler.ServeHTTP(thirdResponse, thirdRequest)
			}()
			select {
			case <-leaseObserved:
			case <-time.After(time.Second):
				cancelFirst()
				cancelSecond()
				cancelThird()
				t.Fatal("queued download lease attempt was not observed")
			}

			test.mutate(t, concrete, sourceSync)
			cancelFirst()
			select {
			case <-thirdDone:
			case <-time.After(2 * time.Second):
				cancelSecond()
				cancelThird()
				t.Fatal("queued download did not finish after a slot was released")
			}
			cancelSecond()
			cancelThird()
			for _, done := range []<-chan struct{}{firstDone, secondDone} {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("blocking download did not stop after cancellation")
				}
			}
			if thirdResponse.Code != http.StatusConflict {
				t.Fatalf("queued download status=%d body=%q, want conflict", thirdResponse.Code, thirdResponse.Body.String())
			}
			for _, forbidden := range []string{"artifact-worker-bytes", "artifact-worker", concrete.sourceArtifacts.root, "catalog.json", "storage_key"} {
				if strings.Contains(thirdResponse.Body.String(), forbidden) {
					t.Fatalf("queued download error leaked %q: %s", forbidden, thirdResponse.Body.String())
				}
			}
		})
	}
}

type blockingSourceAgentArtifactResponseWriter struct {
	header      http.Header
	ctx         context.Context
	entered     chan<- struct{}
	release     <-chan struct{}
	wroteHeader sync.Once
}

func (w *blockingSourceAgentArtifactResponseWriter) Header() http.Header {
	return w.header
}

func (w *blockingSourceAgentArtifactResponseWriter) WriteHeader(_ int) {
	w.wroteHeader.Do(func() { close(w.entered) })
}

func (w *blockingSourceAgentArtifactResponseWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	select {
	case <-w.release:
		return len(data), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

func TestKBaseHTTPHandlerSourceAgentArtifactRolloutGate(t *testing.T) {
	handler, sourceSync, clock, browserSessions := newKBaseSourceAgentCommandHTTPFixture(t)
	credentials, err := createBrowserSessionForTest(browserSessions, BrowserSessionCreate{DeviceLabel: "Artifact Rollout Browser"})
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := clock.Now().Add(time.Hour).Format(time.RFC3339Nano)
	body := `{"type":"upgrade","idempotency_key":"disabled-artifact","payload":{"artifact_id":"artifact-2","expected_current_version":"1.0.0"},"expires_at":"` + expiresAt + `"}`

	artifactRoot := sourceAgentArtifactRootFromHandlerForTest(t, handler)
	setSourceAgentArtifactRolloutForTest(t, artifactRoot, "artifact-2", false)
	request := newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, body)
	addKBaseBrowserSessionSecurityHeaders(request, credentials.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "artifact-2") || strings.Contains(response.Body.String(), artifactRoot) {
		t.Fatalf("disabled create status=%d body=%s", response.Code, response.Body.String())
	}
	commands, err := sourceSync.ListSourceAgentCommands("agent-a", 0)
	if err != nil || len(commands) != 0 {
		t.Fatalf("disabled rollout created commands=%#v err=%v", commands, err)
	}

	setSourceAgentArtifactRolloutForTest(t, artifactRoot, "artifact-2", true)
	request = newKBaseBrowserCookieRequest(http.MethodPost, "/api/source-agents/agent-a/commands", credentials.Token, body)
	addKBaseBrowserSessionSecurityHeaders(request, credentials.CSRFToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("enabled create status=%d body=%s", response.Code, response.Body.String())
	}

	command := mustCreateSourceAgentUpgradeCommand(t, sourceSync, clock, "agent-b", "artifact-worker", "install-gate")
	if _, err := sourceSync.ClaimSourceAgentCommand(command.ID, "agent-b", "agent-b"); err != nil {
		t.Fatal(err)
	}
	commandPath := "/api/source-agent/commands/" + url.PathEscape(command.ID) + "/progress"
	for _, state := range []string{SourceAgentCommandDownloading, SourceAgentCommandVerified} {
		got := requestJSONKBase(handler, http.MethodPost, commandPath, "agent-secret", `{"agent_id":"agent-b","state":"`+state+`"}`)
		if got.Code != http.StatusOK {
			t.Fatalf("progress %s status=%d body=%s", state, got.Code, got.Body.String())
		}
	}
	setSourceAgentArtifactRolloutForTest(t, artifactRoot, "artifact-worker", false)
	install := requestJSONKBase(handler, http.MethodPost, commandPath, "agent-secret", `{"agent_id":"agent-b","state":"installing"}`)
	if install.Code != http.StatusConflict || strings.Contains(install.Body.String(), artifactRoot) || strings.Contains(install.Body.String(), "artifact-worker") {
		t.Fatalf("disabled install status=%d body=%s", install.Code, install.Body.String())
	}
	stored, err := sourceSync.GetSourceAgentCommand(command.ID)
	if err != nil || stored.State != SourceAgentCommandVerified {
		t.Fatalf("disabled install command=%#v err=%v", stored, err)
	}
}

func TestKBaseHTTPHandlerSerializesCapabilityHealth(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", SourceSync: sourceSync, SourceAgentToken: "agent-secret"})
	heartbeat := `{"agent_id":"agent-a","current_run_id":"job-42","current_run_stage":"building_knowledge","capability_health":{"wechat_mp":{"healthy":false,"requires_action":"login"},"wcplus":{"healthy":false}}}`
	if resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", heartbeat); resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", resp.Code, resp.Body.String())
	}
	resp := requestKBase(handler, http.MethodGet, "/api/source-agents", "admin-secret")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"capability_health":{"wcplus":{"healthy":false},"wechat_mp":{"healthy":false,"requires_action":"login"}}`) ||
		!strings.Contains(resp.Body.String(), `"current_run_id":"job-42","current_run_stage":"building_knowledge"`) {
		t.Fatalf("agents capability response status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerCreatesWeChatCollectorSubscription(t *testing.T) {
	root := t.TempDir()
	syncStore, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{Store: NewBookKnowledgeStore(root), AuthToken: "admin-secret", SourceSync: syncStore})
	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-subscriptions", "admin-secret", `{"source_type":"wechat_mp_article","source_account_key":"account-key","source_account":"Sanitized account","operation":"sync_articles","schedule":"manual","enabled":true}`)
	if resp.Code != http.StatusCreated || !strings.Contains(resp.Body.String(), `"source_type":"wechat_mp_article"`) || !strings.Contains(resp.Body.String(), `"operation":"sync_articles"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceAgentPayloadLimit(t *testing.T) {
	sourceSync, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:                   NewBookKnowledgeStore(t.TempDir()),
		AuthToken:               "admin-secret",
		SourceSync:              sourceSync,
		SourceAgentToken:        "agent-secret",
		SourceAgentMaxBodyBytes: 128,
	})
	payload := `{"agent_id":"agent-a","source_item_key":"` + strings.Repeat("x", 512) + `","idempotency_key":"idem","outcome":"new"}`
	resp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/runs/run-1/items", "agent-secret", payload)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized agent payload status = %d, body=%s", resp.Code, resp.Body.String())
	}
}

func TestKBaseHTTPHandlerSourceSyncHTTP(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})

	createResp := requestJSONKBase(handler, http.MethodPost, "/api/source-subscriptions", "admin-secret", `{
		"source_type":"wcplus_wechat_article",
		"source_account_key":"biz-med",
		"source_account":"医学参考",
		"agent_id":"agent-a",
		"schedule":"manual",
		"operation":"sync_content",
		"enabled":true
	}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create subscription status = %d, body=%s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Subscription SourceSubscription `json:"subscription"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if createPayload.Subscription.ID == "" {
		t.Fatalf("created subscription missing id: %s", createResp.Body.String())
	}

	syncPath := "/api/source-subscriptions/" + url.PathEscape(createPayload.Subscription.ID) + "/sync"
	syncResp := requestJSONKBase(handler, http.MethodPost, syncPath, "admin-secret", `{}`)
	if syncResp.Code != http.StatusCreated {
		t.Fatalf("create sync run status = %d, body=%s", syncResp.Code, syncResp.Body.String())
	}

	heartbeatResp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/heartbeat", "agent-secret", `{
		"agent_id":"agent-a",
		"version":"1.0.0",
		"capabilities":["sync_content"],
		"wcplus_healthy":true
	}`)
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body=%s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
	leaseResp := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/lease", "agent-secret", `{
		"agent_id":"agent-a",
		"capabilities":["sync_content"],
		"lease_seconds":120
	}`)
	if leaseResp.Code != http.StatusOK {
		t.Fatalf("lease status = %d, body=%s", leaseResp.Code, leaseResp.Body.String())
	}
	var leasePayload struct {
		Run *SourceSyncRun `json:"run"`
	}
	if err := json.Unmarshal(leaseResp.Body.Bytes(), &leasePayload); err != nil {
		t.Fatalf("decode lease: %v", err)
	}
	if leasePayload.Run == nil || leasePayload.Run.Status != SourceRunRunning {
		t.Fatalf("leased run = %#v, body=%s", leasePayload.Run, leaseResp.Body.String())
	}

	runPath := "/api/source-agent/runs/" + url.PathEscape(leasePayload.Run.ID)
	itemResp := requestJSONKBase(handler, http.MethodPost, runPath+"/items", "agent-secret", `{
		"agent_id":"agent-a",
		"source_type":"wcplus_wechat_article",
		"source_account_key":"biz-med",
		"source_account":"医学参考",
		"source_item_key":"article-1",
		"idempotency_key":"idem-1",
		"title":"可验证知识",
		"author":"编辑部",
		"source_url":"https://mp.weixin.qq.com/s/article-1",
		"published_at":"2026-07-09T19:30:00Z",
		"content":"# 可验证知识\\n\\n每一个知识结论都需要保留可复核的来源、上下文和更新时间，供下游系统进行交叉验证。",
		"content_format":"markdown"
	}`)
	if itemResp.Code != http.StatusCreated {
		t.Fatalf("record item status = %d, body=%s", itemResp.Code, itemResp.Body.String())
	}
	completeResp := requestJSONKBase(handler, http.MethodPost, runPath+"/complete", "agent-secret", `{"agent_id":"agent-a"}`)
	if completeResp.Code != http.StatusOK || !strings.Contains(completeResp.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("complete run status = %d, body=%s", completeResp.Code, completeResp.Body.String())
	}

	detailResp := requestKBase(handler, http.MethodGet, "/api/source-sync/runs/"+url.PathEscape(leasePayload.Run.ID), "admin-secret")
	if detailResp.Code != http.StatusOK || !strings.Contains(detailResp.Body.String(), `"new_count":1`) || !strings.Contains(detailResp.Body.String(), `"source_item_key":"article-1"`) {
		t.Fatalf("run detail status = %d, body=%s", detailResp.Code, detailResp.Body.String())
	}
	agentsResp := requestKBase(handler, http.MethodGet, "/api/source-agents", "admin-secret")
	if agentsResp.Code != http.StatusOK || !strings.Contains(agentsResp.Body.String(), `"agent_id":"agent-a"`) {
		t.Fatalf("agents status = %d, body=%s", agentsResp.Code, agentsResp.Body.String())
	}
	runsResp := requestKBase(handler, http.MethodGet, "/api/source-sync/runs", "admin-secret")
	if runsResp.Code != http.StatusOK || !strings.Contains(runsResp.Body.String(), leasePayload.Run.ID) {
		t.Fatalf("runs status = %d, body=%s", runsResp.Code, runsResp.Body.String())
	}
}

func TestKBaseHTTPHandlerPersistsFailureCheckpointCursor(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceSync.Close()
	registerSourceLeaseAgent(t, sourceSync, "agent-a")
	subscription, err := sourceSync.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wechat_mp_article",
		SourceAccountKey: "account-key",
		SourceAccount:    "Account",
		Operation:        "sync_articles",
		Cursor:           "old-cursor",
		Enabled:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := sourceSync.CreateRun(subscription.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.LeaseNextRun("agent-a", []string{"sync_articles"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceSync.StartRun(run.ID, "agent-a"); err != nil {
		t.Fatal(err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
	})
	response := requestJSONKBase(handler, http.MethodPost, "/api/source-agent/runs/"+url.PathEscape(run.ID)+"/fail", "agent-secret", `{
		"agent_id":"agent-a",
		"error":"download failed",
		"cursor":"safe-cursor"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("fail status=%d body=%s", response.Code, response.Body.String())
	}
	updated, err := sourceSync.GetSubscription(subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cursor != "safe-cursor" || updated.LastSuccessAt != "" {
		t.Fatalf("updated subscription=%#v", updated)
	}
}

func TestKBaseHTTPHandlerSetsSubscriptionEnabledWithoutReplacingCursor(t *testing.T) {
	root := t.TempDir()
	sourceSync, err := NewSourceSyncStore(root)
	if err != nil {
		t.Fatalf("new source sync store: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:      NewBookKnowledgeStore(root),
		AuthToken:  "admin-secret",
		SourceSync: sourceSync,
	})
	subscription, err := sourceSync.CreateSubscription(SourceSubscriptionInput{
		SourceType:       "wcplus_wechat_article",
		SourceAccountKey: "biz-med",
		SourceAccount:    "医学参考",
		AgentID:          "agent-a",
		Schedule:         "interval:3600",
		Cursor:           "2026-07-10T11:55:00Z|article-42",
		Operation:        "sync_content",
		Options:          map[string]any{"limit": float64(50)},
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	path := "/api/source-subscriptions/" + url.PathEscape(subscription.ID) + "/enabled"
	resp := requestJSONKBase(handler, http.MethodPost, path, "admin-secret", `{"enabled":false}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("disable subscription status = %d, body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Subscription SourceSubscription `json:"subscription"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if payload.Subscription.Enabled || payload.Subscription.Cursor != subscription.Cursor || payload.Subscription.Schedule != subscription.Schedule || payload.Subscription.Operation != subscription.Operation {
		t.Fatalf("enabled endpoint replaced subscription state: before=%#v after=%#v", subscription, payload.Subscription)
	}

	missingEnabled := requestJSONKBase(handler, http.MethodPost, path, "admin-secret", `{}`)
	if missingEnabled.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, body=%s", missingEnabled.Code, missingEnabled.Body.String())
	}
}

const (
	testKBaseAuthToken           = "kbase-api-token-must-not-leak"
	testBrowserSessionSecret     = "browser-proxy-secret-0123456789abcdef"
	testBrowserSessionOrigin     = "https://kbase.example"
	testBrowserSessionCookieTTL  = 30 * 24 * time.Hour
	testBrowserSessionAdminToken = "session-admin-token-0123456789abcdef"
)

func TestKBaseHTTPHandlerSessionAdmin(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}

	t.Run("dedicated bearer only", func(t *testing.T) {
		handler, sessionStore := newKBaseSessionAdminHTTPTestHandler(t, clock, 501)
		credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Safari / macOS"})
		if err != nil {
			t.Fatal(err)
		}
		path := "/api/admin/browser-sessions"
		for name, request := range map[string]*http.Request{
			"missing":          httptest.NewRequest(http.MethodGet, path, nil),
			"main bearer":      adminSessionRequest(http.MethodGet, path, testKBaseAuthToken),
			"source bearer":    adminSessionRequest(http.MethodGet, path, "dedicated-source-agent-token"),
			"publisher bearer": adminSessionRequest(http.MethodGet, path, "dedicated-publisher-token"),
			"malformed":        adminSessionRequest(http.MethodGet, path, "wrong"),
			"browser cookie": func() *http.Request {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: credentials.Token})
				return request
			}(),
			"duplicate": func() *http.Request {
				request := adminSessionRequest(http.MethodGet, path, testBrowserSessionAdminToken)
				request.Header.Add("Authorization", "Bearer "+testBrowserSessionAdminToken)
				return request
			}(),
		} {
			t.Run(name, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusUnauthorized ||
					response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
					t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
				}
				if response.Header().Get("Set-Cookie") != "" {
					t.Fatalf("admin rejection mutated browser Cookie: %q", response.Header().Get("Set-Cookie"))
				}
			})
		}
	})

	t.Run("configuration and methods fail closed", func(t *testing.T) {
		handler, _ := newKBaseSessionAdminHTTPTestHandler(t, clock, 502)
		for _, test := range []struct {
			method string
			path   string
			allow  string
		}{
			{http.MethodPost, "/api/admin/browser-sessions", http.MethodGet},
			{http.MethodGet, "/api/admin/browser-sessions/session_id", http.MethodDelete},
			{http.MethodGet, "/api/admin/browser-sessions/revoke-all", http.MethodPost},
		} {
			response := serveAdminSessionRequest(handler, test.method, test.path, testBrowserSessionAdminToken)
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != test.allow {
				t.Fatalf("%s %s status=%d Allow=%q body=%s",
					test.method, test.path, response.Code, response.Header().Get("Allow"), response.Body.String())
			}
		}
		preflight := adminSessionRequest(
			http.MethodOptions,
			"/api/admin/browser-sessions",
			testBrowserSessionAdminToken,
		)
		preflight.Header.Set("Origin", "http://127.0.0.1:5173")
		preflightResponse := httptest.NewRecorder()
		handler.ServeHTTP(preflightResponse, preflight)
		if preflightResponse.Code != http.StatusMethodNotAllowed ||
			preflightResponse.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("OPTIONS status=%d Allow=%q body=%s",
				preflightResponse.Code,
				preflightResponse.Header().Get("Allow"),
				preflightResponse.Body.String())
		}

		missingAdmin := NewKBaseHTTPHandler(KBaseHTTPConfig{
			Store: NewBookKnowledgeStore(t.TempDir()),
			BrowserSessions: BrowserSessionHTTPConfig{
				Store:        newBrowserSessionStoreForAdminTest(t, clock, 503),
				PublicOrigin: testBrowserSessionOrigin,
			},
		})
		response := serveAdminSessionRequest(
			missingAdmin, http.MethodGet, "/api/admin/browser-sessions", testBrowserSessionAdminToken,
		)
		if response.Code != http.StatusServiceUnavailable ||
			response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
			t.Fatalf("missing admin status=%d body=%q", response.Code, response.Body.String())
		}

		missingStore := NewKBaseHTTPHandler(KBaseHTTPConfig{
			Store: NewBookKnowledgeStore(t.TempDir()),
			BrowserSessions: BrowserSessionHTTPConfig{
				AdminToken:   testBrowserSessionAdminToken,
				PublicOrigin: testBrowserSessionOrigin,
			},
		})
		response = serveAdminSessionRequest(
			missingStore, http.MethodGet, "/api/admin/browser-sessions", testBrowserSessionAdminToken,
		)
		if response.Code != http.StatusServiceUnavailable ||
			response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
			t.Fatalf("missing store status=%d body=%q", response.Code, response.Body.String())
		}
	})

	t.Run("list exposes bounded public metadata only", func(t *testing.T) {
		handler, sessionStore := newKBaseSessionAdminHTTPTestHandler(t, clock, 504)
		credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{
			DeviceLabel: "Chrome / Linux",
			UserAgent:   "private-user-agent-must-not-leak",
		})
		if err != nil {
			t.Fatal(err)
		}
		request := adminSessionRequest(
			http.MethodGet, "/api/admin/browser-sessions", testBrowserSessionAdminToken,
		)
		request.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: credentials.Token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionNoStore(t, response)
		if response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("admin list mutated browser Cookie: %q", response.Header().Get("Set-Cookie"))
		}
		body := strings.ToLower(response.Body.String())
		for _, privateValue := range []string{
			credentials.Token,
			credentials.CSRFToken,
			"private-user-agent-must-not-leak",
			"token_hash",
			"csrf_hash",
			"user_agent",
			"cookie",
			"client_id",
			"issued_epoch",
		} {
			if strings.Contains(body, strings.ToLower(privateValue)) {
				t.Fatalf("admin list exposed private value %q: %s", privateValue, response.Body.String())
			}
		}
		var payload struct {
			Sessions []BrowserSession `json:"sessions"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Sessions) != 1 || payload.Sessions[0].ID != credentials.Session.ID {
			t.Fatalf("sessions=%#v", payload.Sessions)
		}
	})

	t.Run("revoke one is immediate and idempotent", func(t *testing.T) {
		handler, sessionStore := newKBaseSessionAdminHTTPTestHandler(t, clock, 505)
		credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Firefox / Linux"})
		if err != nil {
			t.Fatal(err)
		}
		path := "/api/admin/browser-sessions/" + url.PathEscape(credentials.Session.ID)
		for attempt := 0; attempt < 2; attempt++ {
			response := serveAdminSessionRequest(
				handler, http.MethodDelete, path, testBrowserSessionAdminToken,
			)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("attempt %d status=%d body=%q", attempt, response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionNoStore(t, response)
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("admin revoke mutated browser Cookie: %q", response.Header().Get("Set-Cookie"))
			}
		}

		response := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("revoked session next request status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("revoke all is counted idempotently and preserves machine bearer", func(t *testing.T) {
		handler, sessionStore := newKBaseSessionAdminHTTPTestHandler(t, clock, 506)
		for _, label := range []string{"Chrome / macOS", "Safari / iOS"} {
			if _, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: label}); err != nil {
				t.Fatal(err)
			}
		}
		for attempt, want := range []int64{2, 0} {
			response := serveAdminSessionRequest(
				handler,
				http.MethodPost,
				"/api/admin/browser-sessions/revoke-all",
				testBrowserSessionAdminToken,
			)
			if response.Code != http.StatusOK {
				t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
			}
			var payload map[string]int64
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 1 || payload["revoked_count"] != want {
				t.Fatalf("attempt %d payload=%#v", attempt, payload)
			}
		}
		response := requestKBase(handler, http.MethodGet, "/api/books", testKBaseAuthToken)
		if response.Code != http.StatusOK {
			t.Fatalf("machine bearer status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("invalid revoke paths are rejected", func(t *testing.T) {
		handler, _ := newKBaseSessionAdminHTTPTestHandler(t, clock, 507)
		for _, path := range []string{
			"/api/admin/browser-sessions/",
			"/api/admin/browser-sessions/session_a/nested",
			"/api/admin/browser-sessions/session_a?session_id=session_b",
			"/api/admin/browser-sessions/%2F",
			"/api/admin/browser-sessions/%00",
		} {
			response := serveAdminSessionRequest(
				handler, http.MethodDelete, path, testBrowserSessionAdminToken,
			)
			if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
				t.Fatalf("path %q status=%d body=%s", path, response.Code, response.Body.String())
			}
		}
	})

	t.Run("store failures are generic", func(t *testing.T) {
		for index, request := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/admin/browser-sessions"},
			{http.MethodDelete, "/api/admin/browser-sessions/session_missing"},
			{http.MethodPost, "/api/admin/browser-sessions/revoke-all"},
		} {
			sessionStore := newBrowserSessionStoreForAdminTest(t, clock, 508+index)
			handler := newKBaseSessionAdminHTTPTestHandlerForStore(t, sessionStore)
			if err := sessionStore.Close(); err != nil {
				t.Fatal(err)
			}
			response := serveAdminSessionRequest(
				handler, request.method, request.path, testBrowserSessionAdminToken,
			)
			if response.Code != http.StatusServiceUnavailable ||
				response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
				t.Fatalf("%s %s closed store status=%d body=%q",
					request.method, request.path, response.Code, response.Body.String())
			}
		}
	})
}

func TestKBaseHTTPHandlerBrowserSessionMethodRulesAndAuthorizationRejection(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 401)

	for _, method := range []string{
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/browser/session", nil)
			request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405; body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Allow"); got != http.MethodGet+", "+http.MethodPost {
				t.Fatalf("Allow = %q, want GET, POST", got)
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}

	for _, authorization := range []string{"", " ", "Basic forwarded", "Bearer forwarded"} {
		t.Run("authorization_"+fmt.Sprintf("%q", authorization), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
			request.Header.Set("Authorization", authorization)
			request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 0)
}

func TestKBaseHTTPHandlerBrowserSessionProxyConstantTimeBoundaryAndCookieContract(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 402)

	rejectedSecrets := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "short_prefix", values: []string{strings.TrimSuffix(testBrowserSessionSecret, "f")}},
		{name: "long_suffix", values: []string{testBrowserSessionSecret + "x"}},
		{name: "leading_space", values: []string{" " + testBrowserSessionSecret}},
		{name: "trailing_space", values: []string{testBrowserSessionSecret + " "}},
		{name: "oversized", values: []string{strings.Repeat("x", 1024)}},
		{name: "duplicate", values: []string{testBrowserSessionSecret, testBrowserSessionSecret}},
	}
	for _, testCase := range rejectedSecrets {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
			for _, value := range testCase.values {
				request.Header.Add("X-KBase-Browser-Session", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 0)

	request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
	addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
	request.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionNoStore(t, response)
	assertKBaseBrowserSessionPublicResponse(t, response, "Chrome on macOS")
	cookie := requireKBaseBrowserSessionCookie(t, response)
	if cookie.Value == "" || cookie.Value == testKBaseAuthToken || cookie.Value == testBrowserSessionSecret {
		t.Fatalf("session Cookie value is invalid or reused a configured secret")
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session Cookie flags = Secure:%t HttpOnly:%t SameSite:%v", cookie.Secure, cookie.HttpOnly, cookie.SameSite)
	}
	if cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("session Cookie scope = Path:%q Domain:%q", cookie.Path, cookie.Domain)
	}
	if cookie.MaxAge != int(testBrowserSessionCookieTTL/time.Second) {
		t.Fatalf("session Cookie MaxAge = %d, want %d", cookie.MaxAge, int(testBrowserSessionCookieTTL/time.Second))
	}
	if want := clock.Now().Add(testBrowserSessionCookieTTL); !cookie.Expires.Equal(want) {
		t.Fatalf("session Cookie Expires = %s, want %s", cookie.Expires, want)
	}
	assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
	assertKBaseBrowserSessionCount(t, sessionStore, 1)
}

func TestKBaseHTTPHandlerBrowserSessionCookieUsesConfiguredTTL(t *testing.T) {
	const configuredTTL = 2*time.Hour + 30*time.Second
	newClock := func() *browserSessionTestClock {
		return &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		}
	}

	t.Run("login", func(t *testing.T) {
		clock := newClock()
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandlerWithTTL(
			t,
			clock,
			409,
			configuredTTL,
		)
		request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
		request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
		addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("login status = %d, body=%s", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionCookieTTL(t, response, configuredTTL, clock.Now().Add(configuredTTL))
	})

	t.Run("bearer_migration", func(t *testing.T) {
		clock := newClock()
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandlerWithTTL(
			t,
			clock,
			410,
			configuredTTL,
		)
		request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
		addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("migration status = %d, body=%s", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionCookieTTL(t, response, configuredTTL, clock.Now().Add(configuredTTL))
	})

	t.Run("lifecycle_renewal", func(t *testing.T) {
		clock := newClock()
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandlerWithTTL(
			t,
			clock,
			411,
			configuredTTL,
		)
		credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Renewed Browser"})
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(5 * time.Minute)

		request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		addKBaseBrowserSessionHeadersForCredentials(request, credentials)
		request.AddCookie(&http.Cookie{
			Name:  "__Host-kbase_session",
			Value: credentials.Token,
		})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("renewal status = %d, body=%s", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionCookieTTL(t, response, configuredTTL, clock.Now().Add(configuredTTL))
	})
}

func TestKBaseHTTPHandlerBrowserSessionUnavailableIsGeneric(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:                NewBookKnowledgeStore(t.TempDir()),
		AuthToken:            testKBaseAuthToken,
		BrowserSessionSecret: testBrowserSessionSecret,
		BrowserSessions: BrowserSessionHTTPConfig{
			PublicOrigin: testBrowserSessionOrigin,
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
		t.Fatalf("unavailable response = status %d body %q", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionNoStore(t, response)
	assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
}

func TestKBaseHTTPHandlerBrowserSessionStoreConflictsAreGenericServiceUnavailable(t *testing.T) {
	t.Run("create_credential_collision", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		}
		sessionDirectory := t.TempDir()
		if err := os.Chmod(sessionDirectory, 0o750); err != nil {
			t.Fatal(err)
		}
		credentialPair := deterministicBrowserSessionBytes(412, 1)
		repeatedCredentials := append(append([]byte(nil), credentialPair...), credentialPair...)
		sessionStore, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
			Path:            filepath.Join(sessionDirectory, "browser-sessions.sqlite3"),
			Now:             clock.Now,
			Random:          bytes.NewReader(repeatedCredentials),
			TTL:             testBrowserSessionCookieTTL,
			RenewalInterval: 5 * time.Minute,
			MaxActive:       10,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := sessionStore.Close(); err != nil {
				t.Errorf("close browser session store: %v", err)
			}
		})
		if _, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Existing Browser"}); err != nil {
			t.Fatal(err)
		}
		handler := newKBaseBrowserSessionHTTPTestHandlerForStore(
			t,
			sessionStore,
			testBrowserSessionCookieTTL,
		)

		request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
		request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
		addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assertKBaseBrowserSessionGenericServiceUnavailable(t, response)
		assertKBaseBrowserSessionCount(t, sessionStore, 1)
	})

	t.Run("authenticate_renewal_collision", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 413)
		credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Renewal Browser"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sessionStore.db.Exec(`
			CREATE TRIGGER force_browser_session_renewal_conflict
			BEFORE UPDATE OF last_active_at ON browser_sessions
			BEGIN
				SELECT RAISE(ABORT, 'forced renewal conflict');
			END
		`); err != nil {
			t.Fatal(err)
		}
		clock.Advance(5 * time.Minute)

		request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		addKBaseBrowserSessionHeadersForCredentials(request, credentials)
		request.AddCookie(&http.Cookie{
			Name:  "__Host-kbase_session",
			Value: credentials.Token,
		})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assertKBaseBrowserSessionGenericServiceUnavailable(t, response)
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("conflicted renewal changed Cookie: %q", got)
		}
	})
}

func TestKBaseHTTPHandlerBrowserSessionDeviceLabelPrivacyAndBounds(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 403)
	const maxBoundedUserAgentBytes = 512
	rawUserAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Edg/126.0.0.0 " +
		strings.Repeat("x", maxBoundedUserAgentBytes) +
		" private-device-name raw-user-agent-tail"

	request := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
	addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
	request.Header.Set("User-Agent", rawUserAgent)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionPublicResponse(t, response, "Edge on Windows")
	body := response.Body.String()
	for _, privateValue := range []string{"126.0.0.0", "private-device-name", "raw-user-agent-tail", rawUserAgent} {
		if strings.Contains(body, privateValue) {
			t.Fatalf("public response exposed User-Agent detail %q: %s", privateValue, body)
		}
	}

	sessions, err := sessionStore.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].DeviceLabel != "Edge on Windows" ||
		len(sessions[0].DeviceLabel) > 64 {
		t.Fatalf("stored public device metadata = %#v", sessions)
	}
	var storedUserAgentHash []byte
	if err := sessionStore.db.QueryRow(
		`SELECT user_agent_hash FROM browser_sessions WHERE id = ?`,
		sessions[0].ID,
	).Scan(&storedUserAgentHash); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(rawUserAgent[:maxBoundedUserAgentBytes]))
	if !bytes.Equal(storedUserAgentHash, wantHash[:]) {
		t.Fatalf("stored User-Agent hash was not computed from the bounded header")
	}
}

func TestKBaseHTTPHandlerBrowserMigrationMethodAndExactOriginRules(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 404)

	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/browser/session/migrate", nil)
			request.Header.Set("Origin", testBrowserSessionOrigin)
			request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405; body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Allow"); got != http.MethodPost {
				t.Fatalf("Allow = %q, want POST", got)
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}

	for _, origin := range []string{
		"",
		"http://kbase.example",
		testBrowserSessionOrigin + "/",
		"https://KBASE.example",
		" " + testBrowserSessionOrigin,
		testBrowserSessionOrigin + " ",
		strings.Repeat("x", 4096),
	} {
		t.Run("origin_"+fmt.Sprintf("%q", origin), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden ||
				response.Body.String() != "{\"error\":\"forbidden\"}\n" {
				t.Fatalf("origin %q response = status %d body %q", origin, response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}

	duplicateOrigin := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	duplicateOrigin.Header.Add("Origin", testBrowserSessionOrigin)
	duplicateOrigin.Header.Add("Origin", testBrowserSessionOrigin)
	duplicateOrigin.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	duplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResponse, duplicateOrigin)
	if duplicateResponse.Code != http.StatusForbidden {
		t.Fatalf("duplicate Origin status = %d, want 403", duplicateResponse.Code)
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 0)
}

func TestKBaseHTTPHandlerBrowserMigrationValidCookieIsIdempotent(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 405)
	credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{
		DeviceLabel: "Existing Browser",
		UserAgent:   "existing-agent",
	})
	if err != nil {
		t.Fatal(err)
	}

	migrate := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Authorization", "Bearer intentionally-invalid-token")
		addKBaseBrowserSessionHeadersForCredentials(request, credentials)
		request.AddCookie(&http.Cookie{
			Name:  "__Host-kbase_session",
			Value: credentials.Token,
		})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	response := migrate()
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent migration status = %d, body=%s", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionResponseID(t, response, credentials.Session.ID)
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("migration refreshed Cookie inside lifecycle window: %q", got)
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 1)

	clock.Advance(5 * time.Minute)
	renewed := migrate()
	if renewed.Code != http.StatusOK {
		t.Fatalf("renewed migration status = %d, body=%s", renewed.Code, renewed.Body.String())
	}
	assertKBaseBrowserSessionResponseID(t, renewed, credentials.Session.ID)
	refreshedCookie := requireKBaseBrowserSessionCookie(t, renewed)
	if want := clock.Now().Add(testBrowserSessionCookieTTL); !refreshedCookie.Expires.Equal(want) {
		t.Fatalf("refreshed Cookie Expires = %s, want %s", refreshedCookie.Expires, want)
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 1)
}

func TestKBaseHTTPHandlerBrowserMigrationValidBearerCreatesSession(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 406)
	request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	request.Header.Set("Origin", testBrowserSessionOrigin)
	request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	addKBaseBrowserSessionClientHeaders(t, request, sessionStore, "")
	request.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) Version/17.5 Mobile/15E148 Safari/604.1",
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("migration status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionNoStore(t, response)
	assertKBaseBrowserSessionPublicResponse(t, response, "Safari on iOS")
	requireKBaseBrowserSessionCookie(t, response)
	assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
	assertKBaseBrowserSessionCount(t, sessionStore, 1)
}

func TestKBaseHTTPHandlerBrowserMigrationInvalidCredentialsAreIndistinguishable(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 407)
	revoked, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Revoked Browser"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.RevokeByToken(revoked.Token, "test"); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name          string
		authorization string
		cookieToken   string
		wantClear     bool
	}{
		{name: "missing"},
		{name: "invalid_bearer", authorization: "Bearer invalid-token"},
		{name: "unknown_cookie", cookieToken: "unknown-session-token", wantClear: true},
		{name: "revoked_cookie", cookieToken: revoked.Token, wantClear: true},
		{
			name:          "revoked_cookie_and_invalid_bearer",
			authorization: "Bearer invalid-token",
			cookieToken:   revoked.Token,
			wantClear:     true,
		},
	}
	var wantStatus int
	var wantBody string
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
			request.Header.Set("Origin", testBrowserSessionOrigin)
			addKBaseBrowserSessionHeadersForCredentials(request, revoked)
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			if testCase.cookieToken != "" {
				request.AddCookie(&http.Cookie{
					Name:  "__Host-kbase_session",
					Value: testCase.cookieToken,
				})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if index == 0 {
				wantStatus = response.Code
				wantBody = response.Body.String()
			}
			if response.Code != wantStatus || response.Body.String() != wantBody {
				t.Fatalf(
					"response = status %d body %q, want status %d body %q",
					response.Code,
					response.Body.String(),
					wantStatus,
					wantBody,
				)
			}
			if response.Code != http.StatusUnauthorized ||
				response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("generic credential response = status %d body %q", response.Code, response.Body.String())
			}
			if testCase.wantClear {
				cookie := requireKBaseBrowserSessionCookie(t, response)
				if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.Expires.Before(clock.Now()) {
					t.Fatalf("cleared Cookie = %#v", cookie)
				}
				if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode ||
					cookie.Path != "/" || cookie.Domain != "" {
					t.Fatalf("cleared Cookie flags/scope = %#v", cookie)
				}
			} else if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("credential response unexpectedly changed Cookie: %q", got)
			}
			assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
		})
	}
	assertKBaseBrowserSessionCount(t, sessionStore, 1)
}

func TestKBaseHTTPHandlerBrowserMigrationCredentialInvalidCookieBearerFallback(t *testing.T) {
	testCases := []struct {
		name          string
		prepareCookie func(
			*testing.T,
			*BrowserSessionStore,
			*browserSessionTestClock,
		) (string, BrowserClientFamily)
	}{
		{
			name: "unknown cookie",
			prepareCookie: func(
				t *testing.T,
				store *BrowserSessionStore,
				_ *browserSessionTestClock,
			) (string, BrowserClientFamily) {
				family, err := store.AcquireClientEpoch("browser_client_unknown_fallback")
				if err != nil {
					t.Fatal(err)
				}
				return "unknown-session-token", family
			},
		},
		{
			name: "revoked cookie",
			prepareCookie: func(
				t *testing.T,
				store *BrowserSessionStore,
				_ *browserSessionTestClock,
			) (string, BrowserClientFamily) {
				credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
					ClientID: "browser_client_revoked_fallback",
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := store.RevokeByToken(credentials.Token, "test"); err != nil {
					t.Fatal(err)
				}
				return credentials.Token, BrowserClientFamily{
					ClientID: credentials.Session.ClientID,
					Epoch:    credentials.Session.IssuedEpoch,
				}
			},
		},
		{
			name: "expired cookie",
			prepareCookie: func(
				t *testing.T,
				store *BrowserSessionStore,
				clock *browserSessionTestClock,
			) (string, BrowserClientFamily) {
				credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
					ClientID: "browser_client_expired_fallback",
				})
				if err != nil {
					t.Fatal(err)
				}
				clock.Advance(testBrowserSessionCookieTTL + time.Second)
				return credentials.Token, BrowserClientFamily{
					ClientID: credentials.Session.ClientID,
					Epoch:    credentials.Session.IssuedEpoch,
				}
			},
		},
		{
			name: "other family cookie",
			prepareCookie: func(
				t *testing.T,
				store *BrowserSessionStore,
				_ *browserSessionTestClock,
			) (string, BrowserClientFamily) {
				credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
					ClientID: "browser_client_mismatch_source",
				})
				if err != nil {
					t.Fatal(err)
				}
				requested, err := store.AcquireClientEpoch("browser_client_mismatch_target")
				if err != nil {
					t.Fatal(err)
				}
				return credentials.Token, requested
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clock := &browserSessionTestClock{
				now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
			}
			handler, store := newKBaseBrowserSessionHTTPTestHandler(t, clock, 700+index)
			oldToken, family := testCase.prepareCookie(t, store, clock)

			request := newKBaseBrowserCookieRequest(
				http.MethodPost,
				"/browser/session/migrate",
				oldToken,
				"",
			)
			request.Header.Set("Origin", testBrowserSessionOrigin)
			request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
			request.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
			request.Header.Set(
				browserSessionEpochHeaderName,
				strconv.FormatInt(family.Epoch, 10),
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf(
					"Bearer fallback migration = %d body=%s, want 200",
					response.Code,
					response.Body.String(),
				)
			}
			replacement := requireKBaseBrowserSessionCookie(t, response)
			if replacement.Value == "" || replacement.Value == oldToken {
				t.Fatalf("replacement Cookie = %#v, want a new credential", replacement)
			}
			session, err := store.Authenticate(replacement.Value)
			if err != nil {
				t.Fatalf("replacement Cookie authentication = %v", err)
			}
			if session.ClientID != family.ClientID || session.IssuedEpoch != family.Epoch {
				t.Fatalf(
					"replacement session family = (%q, %d), want (%q, %d)",
					session.ClientID,
					session.IssuedEpoch,
					family.ClientID,
					family.Epoch,
				)
			}
		})
	}
}

func TestKBaseHTTPHandlerBrowserMigrationCredentialInvalidCookiePrecedence(t *testing.T) {
	t.Run("invalid Bearer clears invalid Cookie", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 30, 0, 0, time.UTC),
		}
		handler, store := newKBaseBrowserSessionHTTPTestHandler(t, clock, 703)
		family, err := store.AcquireClientEpoch("browser_client_invalid_fallback")
		if err != nil {
			t.Fatal(err)
		}
		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/browser/session/migrate",
			"unknown-session-token",
			"",
		)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Authorization", "Bearer invalid-token")
		request.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
		request.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(family.Epoch, 10))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assertKBaseBrowserSessionUnauthorizedAndCleared(t, response, clock.Now())
	})

	t.Run("invalid Bearer clears other family Cookie", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 40, 0, 0, time.UTC),
		}
		handler, store := newKBaseBrowserSessionHTTPTestHandler(t, clock, 705)
		credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
			ClientID: "browser_client_mismatch_invalid_source",
		})
		if err != nil {
			t.Fatal(err)
		}
		requested, err := store.AcquireClientEpoch("browser_client_mismatch_invalid_target")
		if err != nil {
			t.Fatal(err)
		}
		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/browser/session/migrate",
			credentials.Token,
			"",
		)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Authorization", "Bearer invalid-token")
		request.Header.Set(browserSessionClientIDHeaderName, requested.ClientID)
		request.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(requested.Epoch, 10))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		assertKBaseBrowserSessionUnauthorizedAndCleared(t, response, clock.Now())
	})

	t.Run("stale epoch beats valid Bearer and Cookie", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 45, 0, 0, time.UTC),
		}
		handler, store := newKBaseBrowserSessionHTTPTestHandler(t, clock, 704)
		credentials, err := createBrowserSessionForTest(store, BrowserSessionCreate{
			ClientID: "browser_client_stale_fallback",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RevokeAll("admin"); err != nil {
			t.Fatal(err)
		}
		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/browser/session/migrate",
			credentials.Token,
			"",
		)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
		addKBaseBrowserSessionHeadersForCredentials(request, credentials)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusConflict {
			t.Fatalf("stale migration = %d body=%s, want 409", response.Code, response.Body.String())
		}
		assertKBaseBrowserClientMetadata(
			t,
			response,
			credentials.Session.ClientID,
			credentials.Session.IssuedEpoch+1,
		)
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("stale migration set Cookie: %#v", got)
		}
	})
}

func TestKBaseHTTPHandlerBrowserMigrationUnavailableDoesNotClearCookie(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 408)
	credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: "Unavailable Browser"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	request.Header.Set("Origin", testBrowserSessionOrigin)
	addKBaseBrowserSessionHeadersForCredentials(request, credentials)
	request.AddCookie(&http.Cookie{
		Name:  "__Host-kbase_session",
		Value: credentials.Token,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
		t.Fatalf("unavailable migration response = status %d body %q", response.Code, response.Body.String())
	}
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("store-unavailable migration changed Cookie: %q", got)
	}
	assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
}

func TestKBaseHTTPHandlerBrowserLegacyTokenRetired(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:                NewBookKnowledgeStore(t.TempDir()),
		AuthToken:            testKBaseAuthToken,
		BrowserSessionSecret: testBrowserSessionSecret,
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/browser/session-token", nil)
			request.Header.Set("Authorization", "Bearer forwarded-token")
			request.Header.Set("X-KBase-Browser-Session", testBrowserSessionSecret)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusGone {
				t.Fatalf("status = %d, want 410; body=%s", response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionNoStore(t, response)
			if method == http.MethodGet {
				guidance := strings.ToLower(response.Body.String())
				if !strings.Contains(guidance, "/browser/session") ||
					!strings.Contains(guidance, "migrat") {
					t.Fatalf("retirement guidance is not actionable: %s", response.Body.String())
				}
			} else if response.Body.Len() != 0 {
				t.Fatalf("HEAD response body = %q, want empty", response.Body.String())
			}
			assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
		})
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/browser/session-token", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, response.Code)
		}
		assertKBaseBrowserSessionNoStore(t, response)
		assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
	}
}

func TestKBaseHTTPHandlerCookieAuth(t *testing.T) {
	t.Run("reads general and audit APIs and renews only on interval", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 414)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Cookie Browser")

		response := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if response.Code != http.StatusOK {
			t.Fatalf("Cookie read status = %d, body=%s", response.Code, response.Body.String())
		}
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("Cookie read inside renewal interval reissued Cookie: %q", got)
		}

		auditResponse := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/agent-audits", credentials.Token, "",
		)
		if auditResponse.Code != http.StatusOK ||
			!strings.Contains(auditResponse.Body.String(), `"audits":[]`) {
			t.Fatalf("Cookie audit read status = %d, body=%s", auditResponse.Code, auditResponse.Body.String())
		}

		clock.Advance(5 * time.Minute)
		staticResponse := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/", credentials.Token, "",
		)
		if got := staticResponse.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("static request authenticated or renewed Cookie: %q", got)
		}

		renewed := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if renewed.Code != http.StatusOK {
			t.Fatalf("renewed Cookie read status = %d, body=%s", renewed.Code, renewed.Body.String())
		}
		assertKBaseBrowserSessionCookieTTL(
			t,
			renewed,
			testBrowserSessionCookieTTL,
			clock.Now().Add(testBrowserSessionCookieTTL),
		)

		coalesced := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if got := coalesced.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("coalesced Cookie read reissued Cookie: %q", got)
		}
	})

	t.Run("dedicated Bearer routes reject Cookie without renewing it", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 13, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 415)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Ordinary Browser")
		sourceSync, err := NewSourceSyncStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		concrete := handler.(*kbaseHTTPHandler)
		concrete.sourceSync = sourceSync
		concrete.sourceAgentToken = "dedicated-source-agent-token"
		concrete.agentPublisherToken = "dedicated-publisher-token"
		clock.Advance(5 * time.Minute)

		sourceResponse := requestKBaseWithBrowserCookie(
			handler,
			http.MethodPost,
			"/api/source-agent/heartbeat",
			credentials.Token,
			`{"agent_id":"browser","version":"1","capabilities":[],"wcplus_healthy":true}`,
		)
		if sourceResponse.Code != http.StatusUnauthorized {
			t.Fatalf("Cookie source-agent status = %d, body=%s", sourceResponse.Code, sourceResponse.Body.String())
		}
		if got := sourceResponse.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("source-agent route renewed browser Cookie: %q", got)
		}

		publisherResponse := requestKBaseWithBrowserCookie(
			handler,
			http.MethodPost,
			"/api/agent-packages/publish",
			credentials.Token,
			`{}`,
		)
		if publisherResponse.Code != http.StatusUnauthorized {
			t.Fatalf("Cookie publisher status = %d, body=%s", publisherResponse.Code, publisherResponse.Body.String())
		}
		if got := publisherResponse.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("publisher route renewed browser Cookie: %q", got)
		}

		generalResponse := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if generalResponse.Code != http.StatusOK {
			t.Fatalf("Cookie general read status = %d, body=%s", generalResponse.Code, generalResponse.Body.String())
		}
		requireKBaseBrowserSessionCookie(t, generalResponse)
	})

	t.Run("expired revoked and missing sessions clear Cookie with 401", func(t *testing.T) {
		t.Run("expired", func(t *testing.T) {
			clock := &browserSessionTestClock{
				now: time.Date(2026, time.July, 28, 14, 0, 0, 0, time.UTC),
			}
			handler, sessionStore := newKBaseBrowserSessionHTTPTestHandlerWithTTL(
				t, clock, 416, 10*time.Minute,
			)
			credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Expired Browser")
			clock.Advance(10 * time.Minute)

			response := requestKBaseWithBrowserCookie(
				handler, http.MethodGet, "/api/books", credentials.Token, "",
			)
			assertKBaseBrowserSessionUnauthorizedAndCleared(t, response, clock.Now())
		})

		t.Run("revoked", func(t *testing.T) {
			clock := &browserSessionTestClock{
				now: time.Date(2026, time.July, 28, 15, 0, 0, 0, time.UTC),
			}
			handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 417)
			credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Revoked Browser")
			if err := sessionStore.RevokeByToken(credentials.Token, "test"); err != nil {
				t.Fatal(err)
			}

			response := requestKBaseWithBrowserCookie(
				handler, http.MethodGet, "/api/books", credentials.Token, "",
			)
			assertKBaseBrowserSessionUnauthorizedAndCleared(t, response, clock.Now())
		})

		t.Run("missing", func(t *testing.T) {
			clock := &browserSessionTestClock{
				now: time.Date(2026, time.July, 28, 16, 0, 0, 0, time.UTC),
			}
			handler, _ := newKBaseBrowserSessionHTTPTestHandler(t, clock, 418)
			response := requestKBaseWithBrowserCookie(
				handler, http.MethodGet, "/api/books", "missing-session-token", "",
			)
			assertKBaseBrowserSessionUnauthorizedAndCleared(t, response, clock.Now())
		})
	})

	t.Run("store unavailable is generic 503 and preserves Cookie", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 17, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 419)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Unavailable Browser")
		if err := sessionStore.Close(); err != nil {
			t.Fatal(err)
		}

		response := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/books", credentials.Token, "",
		)
		if response.Code != http.StatusServiceUnavailable ||
			response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
			t.Fatalf("unavailable Cookie auth = status %d body %q", response.Code, response.Body.String())
		}
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("unavailable Cookie auth changed Cookie: %q", got)
		}
	})

	t.Run("audit auth failures keep stable envelope", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 18, 0, 0, 0, time.UTC),
		}
		handler, _ := newKBaseBrowserSessionHTTPTestHandler(t, clock, 420)
		response := requestKBaseWithBrowserCookie(
			handler, http.MethodGet, "/api/agent-audits", "missing-session-token", "",
		)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("audit Cookie auth status = %d, body=%s", response.Code, response.Body.String())
		}
		assertKBaseEvidenceAuditErrorEnvelope(t, response, "audit_unauthorized", "unauthorized")
		requireKBaseBrowserSessionCookie(t, response)
	})
}

func TestKBaseHTTPHandlerCSRF(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 19, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 421)
	credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "CSRF Browser")
	csrfToken, _ := loadKBaseBrowserSessionCSRF(t, handler, credentials.Token)

	var mutationCalls atomic.Int32
	concrete := handler.(*kbaseHTTPHandler)
	concrete.auditCoordinator = &recordingEvidenceAuditEnqueuer{}
	concrete.analysisGenerator = func(
		_ context.Context,
		_ *BookKnowledgeStore,
		request BookAnalysisGenerateRequest,
	) (*BookAnalysisManifest, error) {
		mutationCalls.Add(1)
		return &BookAnalysisManifest{
			Version: "1", BookID: request.BookID, Status: BookAnalysisReady, Answer: "analysis",
		}, nil
	}

	type headerValues struct {
		origin    []string
		fetchSite []string
		csrf      []string
	}
	validHeaders := headerValues{
		origin:    []string{testBrowserSessionOrigin},
		fetchSite: []string{"same-origin"},
		csrf:      []string{csrfToken},
	}
	testCases := []struct {
		name    string
		headers headerValues
	}{
		{name: "missing_origin", headers: headerValues{fetchSite: validHeaders.fetchSite, csrf: validHeaders.csrf}},
		{name: "duplicate_origin", headers: headerValues{origin: []string{testBrowserSessionOrigin, testBrowserSessionOrigin}, fetchSite: validHeaders.fetchSite, csrf: validHeaders.csrf}},
		{name: "wrong_origin", headers: headerValues{origin: []string{"https://other.example"}, fetchSite: validHeaders.fetchSite, csrf: validHeaders.csrf}},
		{name: "oversized_origin", headers: headerValues{origin: []string{strings.Repeat("x", 4096)}, fetchSite: validHeaders.fetchSite, csrf: validHeaders.csrf}},
		{name: "missing_fetch_site", headers: headerValues{origin: validHeaders.origin, csrf: validHeaders.csrf}},
		{name: "duplicate_fetch_site", headers: headerValues{origin: validHeaders.origin, fetchSite: []string{"same-origin", "same-origin"}, csrf: validHeaders.csrf}},
		{name: "wrong_fetch_site", headers: headerValues{origin: validHeaders.origin, fetchSite: []string{"cross-site"}, csrf: validHeaders.csrf}},
		{name: "oversized_fetch_site", headers: headerValues{origin: validHeaders.origin, fetchSite: []string{strings.Repeat("x", 512)}, csrf: validHeaders.csrf}},
		{name: "missing_csrf", headers: headerValues{origin: validHeaders.origin, fetchSite: validHeaders.fetchSite}},
		{name: "duplicate_csrf", headers: headerValues{origin: validHeaders.origin, fetchSite: validHeaders.fetchSite, csrf: []string{csrfToken, csrfToken}}},
		{name: "wrong_csrf", headers: headerValues{origin: validHeaders.origin, fetchSite: validHeaders.fetchSite, csrf: []string{"wrong-csrf-token"}}},
		{name: "oversized_csrf", headers: headerValues{origin: validHeaders.origin, fetchSite: validHeaders.fetchSite, csrf: []string{strings.Repeat("x", 1024)}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := newKBaseBrowserCookieRequest(
				http.MethodPost,
				"/api/books/source-article-1/analysis",
				credentials.Token,
				`{"model":"Qwen-3.7-Max"}`,
			)
			addKBaseHeaderValues(request, "Origin", testCase.headers.origin)
			addKBaseHeaderValues(request, "Sec-Fetch-Site", testCase.headers.fetchSite)
			addKBaseHeaderValues(request, "X-KBase-CSRF", testCase.headers.csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden ||
				response.Body.String() != "{\"error\":\"forbidden\"}\n" {
				t.Fatalf("CSRF rejection = status %d body %q", response.Code, response.Body.String())
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("CSRF rejection changed Cookie: %q", got)
			}
		})
	}
	if got := mutationCalls.Load(); got != 0 {
		t.Fatalf("rejected CSRF requests reached mutation handler %d times", got)
	}

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method+"_requires_CSRF", func(t *testing.T) {
			request := newKBaseBrowserCookieRequest(
				method, "/api/books", credentials.Token, "",
			)
			request.Header.Set("Origin", testBrowserSessionOrigin)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden ||
				response.Body.String() != "{\"error\":\"forbidden\"}\n" {
				t.Fatalf("%s without CSRF = status %d body %q", method, response.Code, response.Body.String())
			}
		})
	}

	validRequest := newKBaseBrowserCookieRequest(
		http.MethodPost,
		"/api/books/source-article-1/analysis",
		credentials.Token,
		`{"model":"Qwen-3.7-Max"}`,
	)
	addKBaseBrowserSessionSecurityHeaders(validRequest, csrfToken)
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, validRequest)
	if validResponse.Code != http.StatusOK || mutationCalls.Load() != 1 {
		t.Fatalf("valid Cookie write = status %d calls %d body=%s", validResponse.Code, mutationCalls.Load(), validResponse.Body.String())
	}

	auditForbidden := newKBaseBrowserCookieRequest(
		http.MethodPost,
		"/api/agent-packages/package-1/audits?version=1.0.0",
		credentials.Token,
		`{}`,
	)
	auditForbidden.Header.Set("Origin", testBrowserSessionOrigin)
	auditForbidden.Header.Set("Sec-Fetch-Site", "same-origin")
	auditForbiddenResponse := httptest.NewRecorder()
	handler.ServeHTTP(auditForbiddenResponse, auditForbidden)
	if auditForbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("audit CSRF status = %d, body=%s", auditForbiddenResponse.Code, auditForbiddenResponse.Body.String())
	}
	assertKBaseEvidenceAuditErrorEnvelope(t, auditForbiddenResponse, "audit_forbidden", "forbidden")

	auditValid := newKBaseBrowserCookieRequest(
		http.MethodPost,
		"/api/agent-packages/package-1/audits?version=1.0.0",
		credentials.Token,
		`{}`,
	)
	addKBaseBrowserSessionSecurityHeaders(auditValid, csrfToken)
	auditValidResponse := httptest.NewRecorder()
	handler.ServeHTTP(auditValidResponse, auditValid)
	if auditValidResponse.Code != http.StatusBadRequest {
		t.Fatalf("valid audit CSRF status = %d, body=%s", auditValidResponse.Code, auditValidResponse.Body.String())
	}
	assertKBaseEvidenceAuditErrorEnvelope(
		t, auditValidResponse, "audit_request_invalid", "idempotency_key is required",
	)

	t.Run("rejected request at renewal boundary does not renew", func(t *testing.T) {
		rejectionClock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 19, 20, 0, 0, time.UTC),
		}
		rejectionHandler, rejectionStore := newKBaseBrowserSessionHTTPTestHandler(
			t, rejectionClock, 428,
		)
		rejectionCredentials := createKBaseBrowserSessionHTTPTestCredentials(
			t, rejectionStore, "Rejected Write Browser",
		)
		createdLastActiveAt := rejectionCredentials.Session.LastActiveAt
		createdExpiresAt := rejectionCredentials.Session.ExpiresAt
		rejectionClock.Advance(5 * time.Minute)

		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/api/books/source-article-1/analysis",
			rejectionCredentials.Token,
			`{"model":"Qwen-3.7-Max"}`,
		)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		response := httptest.NewRecorder()
		rejectionHandler.ServeHTTP(response, request)

		if response.Code != http.StatusForbidden ||
			response.Body.String() != "{\"error\":\"forbidden\"}\n" {
			t.Fatalf("boundary rejection = status %d body %q", response.Code, response.Body.String())
		}
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("boundary rejection renewed Cookie: %q", got)
		}
		assertBrowserSessionStoredActivity(
			t,
			rejectionStore.db,
			rejectionCredentials.Session.ID,
			createdLastActiveAt,
			createdExpiresAt,
		)
	})

	t.Run("renewal boundary revocation replaces renewal with one clear Cookie", func(t *testing.T) {
		raceClock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 19, 30, 0, 0, time.UTC),
		}
		raceHandler, raceStore := newKBaseBrowserSessionHTTPTestHandler(t, raceClock, 426)
		raceCredentials := createKBaseBrowserSessionHTTPTestCredentials(
			t, raceStore, "Renewal Race Browser",
		)
		if _, err := raceStore.db.Exec(`
			CREATE TRIGGER revoke_after_browser_session_renewal
			AFTER UPDATE OF last_active_at ON browser_sessions
			BEGIN
				UPDATE browser_sessions
				SET revoked_at = NEW.last_active_at, revoke_reason = 'concurrent revoke'
				WHERE id = NEW.id;
			END
		`); err != nil {
			t.Fatal(err)
		}
		raceClock.Advance(5 * time.Minute)

		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/api/books/source-article-1/analysis",
			raceCredentials.Token,
			`{"model":"Qwen-3.7-Max"}`,
		)
		addKBaseBrowserSessionSecurityHeaders(request, raceCredentials.CSRFToken)
		response := httptest.NewRecorder()
		raceHandler.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized ||
			response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
			t.Fatalf("renewal race response = status %d body %q", response.Code, response.Body.String())
		}
		if got := response.Header().Values("Set-Cookie"); len(got) != 1 {
			t.Fatalf("renewal race Set-Cookie headers = %q, want one clear Cookie", got)
		}
		if strings.Contains(response.Header().Get("Set-Cookie"), raceCredentials.Token) {
			t.Fatalf("renewal race response retained renewed credential: %q", response.Header().Values("Set-Cookie"))
		}
		assertKBaseBrowserSessionClearedCookie(t, response, raceClock.Now())
		if _, err := raceStore.Authenticate(raceCredentials.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
			t.Fatalf("renewal race auth error = %v, want persisted revocation", err)
		}
	})

	t.Run("renewal boundary store failure returns 503 without changing Cookie", func(t *testing.T) {
		failureClock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 19, 40, 0, 0, time.UTC),
		}
		failureHandler, failureStore := newKBaseBrowserSessionHTTPTestHandler(
			t, failureClock, 430,
		)
		failureCredentials := createKBaseBrowserSessionHTTPTestCredentials(
			t, failureStore, "Renewal Failure Browser",
		)
		createdLastActiveAt := failureCredentials.Session.LastActiveAt
		createdExpiresAt := failureCredentials.Session.ExpiresAt
		if _, err := failureStore.db.Exec(`
			CREATE TRIGGER fail_browser_session_renewal
			BEFORE UPDATE OF last_active_at ON browser_sessions
			BEGIN
				SELECT RAISE(FAIL, 'forced renewal failure');
			END
		`); err != nil {
			t.Fatal(err)
		}
		failureClock.Advance(5 * time.Minute)

		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/api/books/source-article-1/analysis",
			failureCredentials.Token,
			`{"model":"Qwen-3.7-Max"}`,
		)
		addKBaseBrowserSessionSecurityHeaders(request, failureCredentials.CSRFToken)
		response := httptest.NewRecorder()
		failureHandler.ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable ||
			response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
			t.Fatalf("renewal failure = status %d body %q", response.Code, response.Body.String())
		}
		if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("renewal failure changed Cookie: %q", got)
		}
		assertBrowserSessionStoredActivity(
			t,
			failureStore.db,
			failureCredentials.Session.ID,
			createdLastActiveAt,
			createdExpiresAt,
		)
	})

	t.Run("audit retry actor uses Cookie session context", func(t *testing.T) {
		retryStore, pkg := evidenceAuditRunnerTestStore(t, 1, 1)
		retryClock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 20, 0, 0, 0, time.UTC),
		}
		retryHandler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, retryClock, 427)
		retryCredentials := createKBaseBrowserSessionHTTPTestCredentials(
			t, sessionStore, "Audit Retry Browser",
		)
		csrfToken, _ := loadKBaseBrowserSessionCSRF(
			t, retryHandler, retryCredentials.Token,
		)
		auditNow := testAgentPackageTime().Add(4 * time.Hour)
		concrete := retryHandler.(*kbaseHTTPHandler)
		concrete.store = retryStore
		concrete.auditCoordinator = &recordingEvidenceAuditEnqueuer{}
		concrete.auditRetrySigningKey = []byte("test-retry-signing-key-32-bytes!!")
		concrete.auditNow = func() time.Time { return auditNow }

		createRequest := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/api/agent-packages/"+pkg.PackageID+"/audits?version="+pkg.Version,
			retryCredentials.Token,
			`{"subject":"one","scope":"Population evidence comparison","idempotency_key":"cookie-create"}`,
		)
		addKBaseBrowserSessionSecurityHeaders(createRequest, csrfToken)
		createResponse := httptest.NewRecorder()
		retryHandler.ServeHTTP(createResponse, createRequest)
		if createResponse.Code != http.StatusAccepted {
			t.Fatalf("Cookie audit create = status %d body=%s", createResponse.Code, createResponse.Body.String())
		}
		var createPayload struct {
			Audit EvidenceAudit `json:"audit"`
		}
		if err := json.Unmarshal(createResponse.Body.Bytes(), &createPayload); err != nil {
			t.Fatal(err)
		}
		auditID := createPayload.Audit.AuditID
		if _, err := StartEvidenceAudit(
			retryStore, auditID, "trace-cookie-retry",
			testAgentPackageTime().Add(5*time.Hour),
		); err != nil {
			t.Fatal(err)
		}
		if _, err := FailEvidenceAudit(
			retryStore, auditID, "model_outcome_unknown", "manual retry required",
			testAgentPackageTime().Add(6*time.Hour),
		); err != nil {
			t.Fatal(err)
		}

		auditNow = testAgentPackageTime().Add(7 * time.Hour)
		retryRequest := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/api/agent-audits/"+auditID+"/retry",
			retryCredentials.Token,
			"",
		)
		retryRequest.Header.Set("Idempotency-Key", "cookie-retry-1")
		addKBaseBrowserSessionSecurityHeaders(retryRequest, csrfToken)
		retryResponse := httptest.NewRecorder()
		retryHandler.ServeHTTP(retryResponse, retryRequest)
		if retryResponse.Code != http.StatusAccepted {
			t.Fatalf("Cookie audit retry = status %d body=%s", retryResponse.Code, retryResponse.Body.String())
		}
		var retryPayload struct {
			Audit EvidenceAudit `json:"audit"`
		}
		if err := json.Unmarshal(retryResponse.Body.Bytes(), &retryPayload); err != nil {
			t.Fatal(err)
		}
		actor := evidenceAuditOpaqueIdentity(
			"session-actor\x00" + retryCredentials.Session.ID,
		)
		wantIdentity := evidenceAuditOpaqueIdentity(
			"manual-retry\x00" + auditID + "\x00" +
				actor + "\x00kbase-http\x00" + EvidenceAuditRetryScope +
				"\x00cookie-retry-1",
		)
		if retryPayload.Audit.RequestIdentity != wantIdentity {
			t.Fatalf(
				"Cookie retry request identity = %q, want session-context identity %q",
				retryPayload.Audit.RequestIdentity,
				wantIdentity,
			)
		}
	})
}

func TestKBaseHTTPHandlerBrowserSessionStatus(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 20, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 422)
	credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Status Browser")

	firstToken, firstExpiry := loadKBaseBrowserSessionCSRF(t, handler, credentials.Token)
	if err := sessionStore.ValidateCSRF(credentials.Session.ID, firstToken); err != nil {
		t.Fatalf("first status CSRF did not validate: %v", err)
	}
	secondToken, secondExpiry := loadKBaseBrowserSessionCSRF(t, handler, credentials.Token)
	if firstToken != secondToken {
		t.Fatal("same-window status changed CSRF token")
	}
	if !firstExpiry.Equal(secondExpiry) || !secondExpiry.Equal(clock.Now().Add(15*time.Minute)) {
		t.Fatalf("CSRF expiries = %s and %s, want %s", firstExpiry, secondExpiry, clock.Now().Add(15*time.Minute))
	}
	if err := sessionStore.ValidateCSRF(credentials.Session.ID, secondToken); err != nil {
		t.Fatalf("second status CSRF did not validate: %v", err)
	}

	type statusResult struct {
		response *httptest.ResponseRecorder
	}
	const tabCount = 8
	start := make(chan struct{})
	results := make(chan statusResult, tabCount)
	var wait sync.WaitGroup
	for index := 0; index < tabCount; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- statusResult{response: requestKBaseWithBrowserCookie(
				handler, http.MethodGet, "/api/browser/session", credentials.Token, "",
			)}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for result := range results {
		if result.response.Code != http.StatusOK {
			t.Fatalf(
				"concurrent status = %d body=%s",
				result.response.Code,
				result.response.Body.String(),
			)
		}
		assertKBaseBrowserClientMetadata(
			t,
			result.response,
			credentials.Session.ClientID,
			credentials.Session.IssuedEpoch,
		)
		assertKBaseBrowserClientMetadataIsTopLevelOnly(t, result.response)
		token, expiresAt := decodeKBaseBrowserSessionCSRFResponse(
			t, result.response, credentials.Token,
		)
		if token != firstToken || !expiresAt.Equal(firstExpiry) {
			t.Fatalf(
				"concurrent status CSRF = (%q, %s), want (%q, %s)",
				token,
				expiresAt,
				firstToken,
				firstExpiry,
			)
		}
	}

	clock.Advance(browserSessionCSRFTTL)
	rotatedResponse := requestKBaseWithBrowserCookie(
		handler, http.MethodGet, "/api/browser/session", credentials.Token, "",
	)
	if rotatedResponse.Code != http.StatusOK {
		t.Fatalf("post-expiry status = %d body=%s", rotatedResponse.Code, rotatedResponse.Body.String())
	}
	requireKBaseBrowserSessionCookie(t, rotatedResponse)
	rotatedToken, rotatedExpiry := decodeKBaseBrowserSessionCSRFResponse(
		t, rotatedResponse, credentials.Token,
	)
	if rotatedToken == firstToken ||
		!rotatedExpiry.Equal(clock.Now().Add(browserSessionCSRFTTL)) {
		t.Fatalf(
			"post-expiry status CSRF = (%q, %s), previous token %q",
			rotatedToken,
			rotatedExpiry,
			firstToken,
		)
	}
	stableRotatedToken, stableRotatedExpiry := loadKBaseBrowserSessionCSRF(
		t, handler, credentials.Token,
	)
	if stableRotatedToken != rotatedToken || !stableRotatedExpiry.Equal(rotatedExpiry) {
		t.Fatalf(
			"post-expiry same-window CSRF = (%q, %s), want (%q, %s)",
			stableRotatedToken,
			stableRotatedExpiry,
			rotatedToken,
			rotatedExpiry,
		)
	}

	for _, testCase := range []struct {
		name       string
		withCookie bool
	}{
		{name: "Bearer only"},
		{name: "Bearer and Cookie", withCookie: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/browser/session", nil)
			request.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
			if testCase.withCookie {
				request.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: credentials.Token})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("Bearer status endpoint = %d, body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("Bearer status endpoint changed Cookie: %q", got)
			}
		})
	}
}

func TestKBaseHTTPHandlerBrowserLogout(t *testing.T) {
	t.Run("requires Cookie CSRF revokes then clears and retry is stable", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 21, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 423)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Logout Browser")
		csrfToken, _ := loadKBaseBrowserSessionCSRF(t, handler, credentials.Token)

		forbidden := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/browser/session/logout", credentials.Token, "",
		)
		forbidden.Header.Set("Origin", testBrowserSessionOrigin)
		forbidden.Header.Set("Sec-Fetch-Site", "same-origin")
		forbiddenResponse := httptest.NewRecorder()
		handler.ServeHTTP(forbiddenResponse, forbidden)
		if forbiddenResponse.Code != http.StatusForbidden {
			t.Fatalf("logout without CSRF = %d, body=%s", forbiddenResponse.Code, forbiddenResponse.Body.String())
		}
		if got := forbiddenResponse.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("forbidden logout cleared Cookie: %q", got)
		}
		if _, err := sessionStore.AuthenticateAndRenew(credentials.Token); err != nil {
			t.Fatalf("forbidden logout revoked session: %v", err)
		}

		logout := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/browser/session/logout", credentials.Token, "",
		)
		addKBaseBrowserSessionSecurityHeaders(logout, csrfToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, logout)
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("logout response = status %d body %q", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionNoStore(t, response)
		assertKBaseBrowserSessionClearedCookie(t, response, clock.Now())
		if _, err := sessionStore.AuthenticateAndRenew(credentials.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
			t.Fatalf("logout auth error = %v, want revoked before Cookie clear", err)
		}

		retry := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/browser/session/logout", credentials.Token, "",
		)
		addKBaseBrowserSessionSecurityHeaders(retry, csrfToken)
		retryResponse := httptest.NewRecorder()
		handler.ServeHTTP(retryResponse, retry)
		assertKBaseBrowserSessionUnauthorizedAndCleared(t, retryResponse, clock.Now())
	})

	t.Run("concurrent requests have idempotent terminal state", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 22, 0, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 424)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Concurrent Logout Browser")
		csrfToken, _ := loadKBaseBrowserSessionCSRF(t, handler, credentials.Token)

		const requestCount = 8
		start := make(chan struct{})
		responses := make(chan *httptest.ResponseRecorder, requestCount)
		var wait sync.WaitGroup
		for index := 0; index < requestCount; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				request := newKBaseBrowserCookieRequest(
					http.MethodPost, "/api/browser/session/logout", credentials.Token, "",
				)
				addKBaseBrowserSessionSecurityHeaders(request, csrfToken)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				responses <- response
			}()
		}
		close(start)
		wait.Wait()
		close(responses)

		successes := 0
		for response := range responses {
			switch response.Code {
			case http.StatusNoContent:
				successes++
			case http.StatusUnauthorized:
				if response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
					t.Fatalf("concurrent unauthorized body = %q", response.Body.String())
				}
			default:
				t.Fatalf("concurrent logout status = %d, body=%s", response.Code, response.Body.String())
			}
			assertKBaseBrowserSessionClearedCookie(t, response, clock.Now())
		}
		if successes == 0 {
			t.Fatal("concurrent logout had no successful revocation")
		}
		if _, err := sessionStore.AuthenticateAndRenew(credentials.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
			t.Fatalf("concurrent logout terminal auth error = %v, want revoked", err)
		}
	})

	t.Run("renewal boundary logout revokes without renewing activity", func(t *testing.T) {
		clock := &browserSessionTestClock{
			now: time.Date(2026, time.July, 28, 22, 30, 0, 0, time.UTC),
		}
		handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 429)
		credentials := createKBaseBrowserSessionHTTPTestCredentials(
			t, sessionStore, "Boundary Logout Browser",
		)
		createdLastActiveAt := credentials.Session.LastActiveAt
		createdExpiresAt := credentials.Session.ExpiresAt
		clock.Advance(5 * time.Minute)

		request := newKBaseBrowserCookieRequest(
			http.MethodPost, "/api/browser/session/logout", credentials.Token, "",
		)
		addKBaseBrowserSessionSecurityHeaders(request, credentials.CSRFToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Fatalf("boundary logout = status %d body=%s", response.Code, response.Body.String())
		}
		assertKBaseBrowserSessionClearedCookie(t, response, clock.Now())
		assertBrowserSessionStoredActivity(
			t,
			sessionStore.db,
			credentials.Session.ID,
			createdLastActiveAt,
			createdExpiresAt,
		)
	})
}

func TestKBaseHTTPHandlerBrowserSessionClientEpochPreconditions(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 22, 45, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 611)
	const clientID = "browser_client_http_01"

	acquire := httptest.NewRequest(http.MethodGet, "/browser/session", nil)
	acquire.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
	acquire.Header.Set(browserSessionClientIDHeaderName, clientID)
	acquireResponse := httptest.NewRecorder()
	handler.ServeHTTP(acquireResponse, acquire)
	if acquireResponse.Code != http.StatusOK {
		t.Fatalf("client epoch GET = %d body=%s", acquireResponse.Code, acquireResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, acquireResponse, clientID, 1)
	if got := acquireResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("client epoch GET set Cookie: %#v", got)
	}

	missing := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	missing.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("login without client precondition = %d, want 428", missingResponse.Code)
	}
	if got := missingResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("login without client precondition set Cookie: %#v", got)
	}

	login := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	login.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
	login.Header.Set(browserSessionClientIDHeaderName, clientID)
	login.Header.Set(browserSessionEpochHeaderName, "1")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("epoch login = %d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, loginResponse, clientID, 1)
	assertKBaseBrowserClientMetadataIsTopLevelOnly(t, loginResponse)
	requireKBaseBrowserSessionCookie(t, loginResponse)

	if _, err := sessionStore.RevokeAll("admin"); err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	stale.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
	stale.Header.Set(browserSessionClientIDHeaderName, clientID)
	stale.Header.Set(browserSessionEpochHeaderName, "1")
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale epoch login = %d body=%s, want 409", staleResponse.Code, staleResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, staleResponse, clientID, 2)
	if got := staleResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("stale epoch login set Cookie: %#v", got)
	}

	fresh := httptest.NewRequest(http.MethodPost, "/browser/session", nil)
	fresh.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
	fresh.Header.Set(browserSessionClientIDHeaderName, clientID)
	fresh.Header.Set(browserSessionEpochHeaderName, "2")
	freshResponse := httptest.NewRecorder()
	handler.ServeHTTP(freshResponse, fresh)
	if freshResponse.Code != http.StatusOK {
		t.Fatalf("fresh epoch login = %d body=%s", freshResponse.Code, freshResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, freshResponse, clientID, 2)
	requireKBaseBrowserSessionCookie(t, freshResponse)
}

func TestKBaseHTTPHandlerBrowserSessionClientEpochValidation(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 22, 50, 0, 0, time.UTC),
	}
	handler, _ := newKBaseBrowserSessionHTTPTestHandler(t, clock, 612)

	tests := []struct {
		name       string
		method     string
		clientIDs  []string
		epochs     []string
		wantStatus int
	}{
		{name: "missing client", method: http.MethodGet, wantStatus: http.StatusPreconditionRequired},
		{name: "duplicate client", method: http.MethodGet, clientIDs: []string{"browser_client_valid_01", "browser_client_valid_01"}, wantStatus: http.StatusBadRequest},
		{name: "short client", method: http.MethodGet, clientIDs: []string{"too-short"}, wantStatus: http.StatusBadRequest},
		{name: "oversized client", method: http.MethodGet, clientIDs: []string{strings.Repeat("a", maxBrowserSessionClientIDBytes+1)}, wantStatus: http.StatusBadRequest},
		{name: "non ascii client", method: http.MethodGet, clientIDs: []string{"browser_client_浏览器_01"}, wantStatus: http.StatusBadRequest},
		{name: "invalid client punctuation", method: http.MethodGet, clientIDs: []string{"browser.client.valid.01"}, wantStatus: http.StatusBadRequest},
		{name: "epoch on GET", method: http.MethodGet, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"1"}, wantStatus: http.StatusBadRequest},
		{name: "missing epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, wantStatus: http.StatusPreconditionRequired},
		{name: "duplicate epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"1", "1"}, wantStatus: http.StatusBadRequest},
		{name: "zero epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"0"}, wantStatus: http.StatusBadRequest},
		{name: "signed epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"+1"}, wantStatus: http.StatusBadRequest},
		{name: "leading zero epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"01"}, wantStatus: http.StatusBadRequest},
		{name: "non ascii epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{"１"}, wantStatus: http.StatusBadRequest},
		{name: "oversized epoch", method: http.MethodPost, clientIDs: []string{"browser_client_valid_01"}, epochs: []string{strings.Repeat("9", maxBrowserSessionEpochBytes+1)}, wantStatus: http.StatusBadRequest},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/browser/session", nil)
			request.Header.Set(browserSessionProxyHeaderName, testBrowserSessionSecret)
			for _, value := range testCase.clientIDs {
				request.Header.Add(browserSessionClientIDHeaderName, value)
			}
			for _, value := range testCase.epochs {
				request.Header.Add(browserSessionEpochHeaderName, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), testCase.wantStatus)
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("invalid precondition set Cookie: %#v", got)
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}
}

func TestKBaseHTTPHandlerBrowserSessionUninitializedClientIsPreconditionRequired(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 22, 52, 0, 0, time.UTC),
	}
	handler, _ := newKBaseBrowserSessionHTTPTestHandler(t, clock, 617)
	const clientID = "browser_client_http_uninitialized"

	testCases := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{
			name: "login",
			path: "/browser/session",
			headers: map[string]string{
				browserSessionProxyHeaderName: testBrowserSessionSecret,
			},
		},
		{
			name: "Bearer migration",
			path: "/browser/session/migrate",
			headers: map[string]string{
				"Origin":        testBrowserSessionOrigin,
				"Authorization": "Bearer " + testKBaseAuthToken,
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, testCase.path, nil)
			for name, value := range testCase.headers {
				request.Header.Set(name, value)
			}
			request.Header.Set(browserSessionClientIDHeaderName, clientID)
			request.Header.Set(browserSessionEpochHeaderName, "1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusPreconditionRequired ||
				response.Body.String() != "{\"error\":\"browser client is not initialized\"}\n" {
				t.Fatalf(
					"uninitialized response = %d body=%q, want 428",
					response.Code,
					response.Body.String(),
				)
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("uninitialized response set Cookie: %#v", got)
			}
			assertKBaseBrowserSessionNoStore(t, response)
		})
	}
}

func TestKBaseHTTPHandlerBrowserMigrationStaleEpochDoesNotSetCookie(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 22, 55, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 613)
	missing := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	missing.Header.Set("Origin", testBrowserSessionOrigin)
	missing.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("migration without client precondition = %d, want 428", missingResponse.Code)
	}
	if got := missingResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("migration without client precondition set Cookie: %#v", got)
	}

	family, err := sessionStore.AcquireClientEpoch("browser_client_migrate_01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessionStore.RevokeAll("admin"); err != nil {
		t.Fatal(err)
	}

	stale := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	stale.Header.Set("Origin", testBrowserSessionOrigin)
	stale.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	stale.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
	stale.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(family.Epoch, 10))
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale migration = %d body=%s, want 409", staleResponse.Code, staleResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, staleResponse, family.ClientID, family.Epoch+1)
	if got := staleResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("stale migration set Cookie: %#v", got)
	}

	fresh := httptest.NewRequest(http.MethodPost, "/browser/session/migrate", nil)
	fresh.Header.Set("Origin", testBrowserSessionOrigin)
	fresh.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	fresh.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
	fresh.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(family.Epoch+1, 10))
	freshResponse := httptest.NewRecorder()
	handler.ServeHTTP(freshResponse, fresh)
	if freshResponse.Code != http.StatusOK {
		t.Fatalf("fresh migration = %d body=%s", freshResponse.Code, freshResponse.Body.String())
	}
	assertKBaseBrowserClientMetadata(t, freshResponse, family.ClientID, family.Epoch+1)
	requireKBaseBrowserSessionCookie(t, freshResponse)
}

func TestKBaseHTTPHandlerBrowserMigrationExpectedAuthLinearizesWithFence(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 22, 58, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 615)
	credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{
		ClientID: "browser_client_migrate_linear",
	})
	if err != nil {
		t.Fatal(err)
	}
	fenceStore, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            sessionStore.DBPath(),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(616, 64)),
		TTL:             testBrowserSessionCookieTTL,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fenceStore.Close()
	clock.Advance(5 * time.Minute)

	insideAuth := make(chan struct{})
	releaseAuth := make(chan struct{})
	sessionStore.expectedAuthBeforeCommit = func() {
		close(insideAuth)
		<-releaseAuth
	}
	migrateResponse := httptest.NewRecorder()
	migrateDone := make(chan struct{})
	go func() {
		defer close(migrateDone)
		request := newKBaseBrowserCookieRequest(
			http.MethodPost,
			"/browser/session/migrate",
			credentials.Token,
			"",
		)
		request.Header.Set("Origin", testBrowserSessionOrigin)
		addKBaseBrowserSessionHeadersForCredentials(request, credentials)
		handler.ServeHTTP(migrateResponse, request)
	}()
	<-insideAuth

	fenceStarted := make(chan struct{})
	fenceResult := make(chan error, 1)
	go func() {
		close(fenceStarted)
		_, err := fenceStore.FenceClientBySession(credentials.Session.ID, "logout")
		fenceResult <- err
	}()
	<-fenceStarted
	close(releaseAuth)
	<-migrateDone
	if migrateResponse.Code != http.StatusOK {
		t.Fatalf(
			"auth-first migration = %d body=%s, want 200",
			migrateResponse.Code,
			migrateResponse.Body.String(),
		)
	}
	requireKBaseBrowserSessionCookie(t, migrateResponse)
	if err := <-fenceResult; err != nil {
		t.Fatalf("concurrent fence error = %v", err)
	}
	if _, err := sessionStore.Authenticate(credentials.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
		t.Fatalf("auth-first response credential = %v, want revoked after fence", err)
	}
	sessionStore.expectedAuthBeforeCommit = nil

	stale := newKBaseBrowserCookieRequest(
		http.MethodPost,
		"/browser/session/migrate",
		credentials.Token,
		"",
	)
	stale.Header.Set("Origin", testBrowserSessionOrigin)
	addKBaseBrowserSessionHeadersForCredentials(stale, credentials)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf(
			"fence-first migration = %d body=%s, want 409",
			staleResponse.Code,
			staleResponse.Body.String(),
		)
	}
	if got := staleResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("fence-first migration set Cookie: %#v", got)
	}
}

func TestKBaseHTTPHandlerBrowserLogoutFencesFamilyAndAllowsNewEpoch(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 23, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 614)
	family, err := sessionStore.AcquireClientEpoch("browser_client_logout_01")
	if err != nil {
		t.Fatal(err)
	}
	first, err := sessionStore.Create(BrowserSessionCreate{
		ClientID: family.ClientID, ExpectedEpoch: family.Epoch, DeviceLabel: "First tab",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionStore.Create(BrowserSessionCreate{
		ClientID: family.ClientID, ExpectedEpoch: family.Epoch, DeviceLabel: "Second tab",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherFamily, err := sessionStore.AcquireClientEpoch("browser_client_other_01")
	if err != nil {
		t.Fatal(err)
	}
	other, err := sessionStore.Create(BrowserSessionCreate{
		ClientID: otherFamily.ClientID, ExpectedEpoch: otherFamily.Epoch, DeviceLabel: "Other device",
	})
	if err != nil {
		t.Fatal(err)
	}
	csrfToken, _, err := sessionStore.IssueCSRF(first.Token)
	if err != nil {
		t.Fatal(err)
	}

	logout := newKBaseBrowserCookieRequest(
		http.MethodPost, "/api/browser/session/logout", first.Token, "",
	)
	addKBaseBrowserSessionSecurityHeaders(logout, csrfToken)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("family logout = %d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	for _, credentials := range []BrowserSessionCredentials{first, second} {
		if _, err := sessionStore.Authenticate(credentials.Token); !errors.Is(err, ErrBrowserSessionRevoked) {
			t.Fatalf("same-family session after logout = %v, want revoked", err)
		}
	}
	if _, err := sessionStore.Authenticate(other.Token); err != nil {
		t.Fatalf("different-family session after logout = %v, want active", err)
	}

	next, err := sessionStore.ReadClientEpoch(family.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := sessionStore.Create(BrowserSessionCreate{
		ClientID: next.ClientID, ExpectedEpoch: next.Epoch, DeviceLabel: "New login",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessionStore.Authenticate(fresh.Token); err != nil {
		t.Fatalf("new-epoch session = %v, want active", err)
	}
}

func TestKBaseHTTPHandlerBearerCompatibility(t *testing.T) {
	clock := &browserSessionTestClock{
		now: time.Date(2026, time.July, 28, 23, 0, 0, 0, time.UTC),
	}
	handler, sessionStore := newKBaseBrowserSessionHTTPTestHandler(t, clock, 425)
	credentials := createKBaseBrowserSessionHTTPTestCredentials(t, sessionStore, "Bearer Compatibility Browser")
	concrete := handler.(*kbaseHTTPHandler)
	var mutationCalls atomic.Int32
	concrete.analysisGenerator = func(
		_ context.Context,
		_ *BookKnowledgeStore,
		request BookAnalysisGenerateRequest,
	) (*BookAnalysisManifest, error) {
		mutationCalls.Add(1)
		return &BookAnalysisManifest{
			Version: "1", BookID: request.BookID, Status: BookAnalysisReady, Answer: "analysis",
		}, nil
	}

	read := requestKBase(handler, http.MethodGet, "/api/books", testKBaseAuthToken)
	if read.Code != http.StatusOK {
		t.Fatalf("Bearer read status = %d, body=%s", read.Code, read.Body.String())
	}
	write := requestJSONKBase(
		handler,
		http.MethodPost,
		"/api/books/source-article-1/analysis",
		testKBaseAuthToken,
		`{"model":"Qwen-3.7-Max"}`,
	)
	if write.Code != http.StatusOK || mutationCalls.Load() != 1 {
		t.Fatalf("Bearer unsafe write = status %d calls %d body=%s", write.Code, mutationCalls.Load(), write.Body.String())
	}

	sourceSync, err := NewSourceSyncStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	concrete.sourceSync = sourceSync
	concrete.sourceAgentToken = "dedicated-source-agent-token"
	concrete.agentPublisherToken = "dedicated-publisher-token"
	clock.Advance(5 * time.Minute)

	sourceRequest := newKBaseBrowserCookieRequest(
		http.MethodPost,
		"/api/source-agent/heartbeat",
		credentials.Token,
		`{"agent_id":"agent-a","version":"1.0.0","capabilities":["sync_content"],"wcplus_healthy":true}`,
	)
	sourceRequest.Header.Set("Authorization", "Bearer dedicated-source-agent-token")
	sourceResponse := httptest.NewRecorder()
	handler.ServeHTTP(sourceResponse, sourceRequest)
	if sourceResponse.Code != http.StatusOK {
		t.Fatalf("dedicated source Bearer status = %d, body=%s", sourceResponse.Code, sourceResponse.Body.String())
	}
	if got := sourceResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("dedicated source Bearer renewed browser Cookie: %q", got)
	}

	publisherRequest := newKBaseBrowserCookieRequest(
		http.MethodPost, "/api/agent-packages/publish", credentials.Token, `{}`,
	)
	publisherRequest.Header.Set("Authorization", "Bearer dedicated-publisher-token")
	publisherResponse := httptest.NewRecorder()
	handler.ServeHTTP(publisherResponse, publisherRequest)
	if publisherResponse.Code != http.StatusBadRequest {
		t.Fatalf("dedicated publisher Bearer status = %d, body=%s", publisherResponse.Code, publisherResponse.Body.String())
	}
	if got := publisherResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("dedicated publisher Bearer renewed browser Cookie: %q", got)
	}

	t.Run("duplicate Authorization headers are rejected at every Bearer boundary", func(t *testing.T) {
		general := httptest.NewRequest(http.MethodGet, "/api/books", nil)
		general.Header.Add("Authorization", "Bearer "+testKBaseAuthToken)
		general.Header.Add("Authorization", "Bearer "+testKBaseAuthToken)
		generalResponse := httptest.NewRecorder()
		handler.ServeHTTP(generalResponse, general)
		if generalResponse.Code != http.StatusUnauthorized {
			t.Fatalf("duplicate general Bearer = %d body=%s", generalResponse.Code, generalResponse.Body.String())
		}

		audit := httptest.NewRequest(http.MethodGet, "/api/agent-audits", nil)
		audit.Header.Add("Authorization", "Bearer "+testKBaseAuthToken)
		audit.Header.Add("Authorization", "Bearer "+testKBaseAuthToken)
		auditResponse := httptest.NewRecorder()
		handler.ServeHTTP(auditResponse, audit)
		if auditResponse.Code != http.StatusUnauthorized {
			t.Fatalf("duplicate audit Bearer = %d body=%s", auditResponse.Code, auditResponse.Body.String())
		}
		assertKBaseEvidenceAuditErrorEnvelope(
			t, auditResponse, "audit_unauthorized", "unauthorized",
		)

		source := httptest.NewRequest(
			http.MethodPost,
			"/api/source-agent/heartbeat",
			strings.NewReader(`{"agent_id":"agent-a","version":"1.0.0","capabilities":[],"wcplus_healthy":true}`),
		)
		source.Header.Add("Authorization", "Bearer dedicated-source-agent-token")
		source.Header.Add("Authorization", "Bearer dedicated-source-agent-token")
		sourceResponse := httptest.NewRecorder()
		handler.ServeHTTP(sourceResponse, source)
		if sourceResponse.Code != http.StatusUnauthorized {
			t.Fatalf("duplicate source Bearer = %d body=%s", sourceResponse.Code, sourceResponse.Body.String())
		}

		publisher := httptest.NewRequest(
			http.MethodPost, "/api/agent-packages/publish", strings.NewReader(`{}`),
		)
		publisher.Header.Add("Authorization", "Bearer dedicated-publisher-token")
		publisher.Header.Add("Authorization", "Bearer dedicated-publisher-token")
		publisherResponse := httptest.NewRecorder()
		handler.ServeHTTP(publisherResponse, publisher)
		if publisherResponse.Code != http.StatusUnauthorized {
			t.Fatalf("duplicate publisher Bearer = %d body=%s", publisherResponse.Code, publisherResponse.Body.String())
		}
	})

	for _, authorization := range []string{"", "Basic invalid", "Bearer wrong-token"} {
		t.Run("explicit Authorization "+fmt.Sprintf("%q", authorization), func(t *testing.T) {
			request := newKBaseBrowserCookieRequest(
				http.MethodGet, "/api/books", credentials.Token, "",
			)
			request.Header["Authorization"] = []string{authorization}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("ambiguous auth status = %d, body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("invalid explicit Bearer changed valid Cookie: %q", got)
			}
		})
	}

	bearerWins := newKBaseBrowserCookieRequest(
		http.MethodGet, "/api/books", "invalid-cookie-token", "",
	)
	bearerWins.Header.Set("Authorization", "Bearer "+testKBaseAuthToken)
	bearerWinsResponse := httptest.NewRecorder()
	handler.ServeHTTP(bearerWinsResponse, bearerWins)
	if bearerWinsResponse.Code != http.StatusOK {
		t.Fatalf("valid Bearer with invalid Cookie status = %d, body=%s", bearerWinsResponse.Code, bearerWinsResponse.Body.String())
	}
	if got := bearerWinsResponse.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("valid Bearer inspected invalid Cookie: %q", got)
	}
}

func assertKBaseBrowserClientMetadata(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantClientID string,
	wantEpoch int64,
) {
	t.Helper()
	var body struct {
		ClientID string `json:"client_id"`
		Epoch    int64  `json:"epoch"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode browser client metadata: %v body=%s", err, response.Body.String())
	}
	if body.ClientID != wantClientID || body.Epoch != wantEpoch {
		t.Fatalf(
			"browser client metadata = (%q, %d), want (%q, %d)",
			body.ClientID,
			body.Epoch,
			wantClientID,
			wantEpoch,
		)
	}
}

func assertKBaseBrowserClientMetadataIsTopLevelOnly(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode browser session response: %v", err)
	}
	session, ok := body["session"].(map[string]any)
	if !ok {
		t.Fatalf("browser session response has no public session object: %#v", body)
	}
	for _, field := range []string{"client_id", "issued_epoch"} {
		if _, exists := session[field]; exists {
			t.Fatalf("browser session nested public metadata exposed %q", field)
		}
	}
	if _, ok := body["client_id"].(string); !ok {
		t.Fatal("browser session response missing top-level client_id")
	}
	if _, ok := body["epoch"].(float64); !ok {
		t.Fatal("browser session response missing top-level epoch")
	}
}

func addKBaseBrowserSessionClientHeaders(
	t *testing.T,
	request *http.Request,
	sessionStore *BrowserSessionStore,
	clientID string,
) BrowserClientFamily {
	t.Helper()
	if clientID == "" {
		clientID = fmt.Sprintf(
			"http_client_%016x",
			browserSessionTestClientSequence.Add(1),
		)
	}
	family, err := sessionStore.AcquireClientEpoch(clientID)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(browserSessionClientIDHeaderName, family.ClientID)
	request.Header.Set(browserSessionEpochHeaderName, strconv.FormatInt(family.Epoch, 10))
	return family
}

func addKBaseBrowserSessionHeadersForCredentials(
	request *http.Request,
	credentials BrowserSessionCredentials,
) {
	request.Header.Set(browserSessionClientIDHeaderName, credentials.Session.ClientID)
	request.Header.Set(
		browserSessionEpochHeaderName,
		strconv.FormatInt(credentials.Session.IssuedEpoch, 10),
	)
}

func createKBaseBrowserSessionHTTPTestCredentials(
	t *testing.T,
	sessionStore *BrowserSessionStore,
	deviceLabel string,
) BrowserSessionCredentials {
	t.Helper()
	credentials, err := createBrowserSessionForTest(sessionStore, BrowserSessionCreate{DeviceLabel: deviceLabel})
	if err != nil {
		t.Fatal(err)
	}
	return credentials
}

func newKBaseBrowserCookieRequest(
	method, path, token, body string,
) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.AddCookie(&http.Cookie{Name: browserSessionCookieName, Value: token})
	}
	return request
}

func requestKBaseWithBrowserCookie(
	handler http.Handler,
	method, path, token, body string,
) *httptest.ResponseRecorder {
	request := newKBaseBrowserCookieRequest(method, path, token, body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func addKBaseHeaderValues(request *http.Request, name string, values []string) {
	for _, value := range values {
		request.Header.Add(name, value)
	}
}

func addKBaseBrowserSessionSecurityHeaders(request *http.Request, csrfToken string) {
	request.Header.Set("Origin", testBrowserSessionOrigin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-KBase-CSRF", csrfToken)
}

func loadKBaseBrowserSessionCSRF(
	t *testing.T,
	handler http.Handler,
	sessionToken string,
) (string, time.Time) {
	t.Helper()
	response := requestKBaseWithBrowserCookie(
		handler, http.MethodGet, "/api/browser/session", sessionToken, "",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("browser session status = %d, body=%s", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionNoStore(t, response)
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("status inside renewal interval reissued Cookie: %q", got)
	}
	return decodeKBaseBrowserSessionCSRFResponse(t, response, sessionToken)
}

func decodeKBaseBrowserSessionCSRFResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	sessionToken string,
) (string, time.Time) {
	t.Helper()
	var payload struct {
		Session       BrowserSession `json:"session"`
		CSRFToken     string         `json:"csrf_token"`
		CSRFExpiresAt time.Time      `json:"csrf_expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode browser session status: %v; body=%s", err, response.Body.String())
	}
	if payload.Session.ID == "" || payload.CSRFToken == "" || payload.CSRFExpiresAt.IsZero() {
		t.Fatalf("browser session status payload = %#v", payload)
	}
	body := strings.ToLower(response.Body.String())
	for _, privateValue := range []string{
		sessionToken,
		`"token_hash"`,
		`"csrf_hash"`,
		`"user_agent"`,
		`"hash"`,
		`"revoke_reason"`,
	} {
		if strings.Contains(body, strings.ToLower(privateValue)) {
			t.Fatalf("browser session status exposed private value %q: %s", privateValue, response.Body.String())
		}
	}
	return payload.CSRFToken, payload.CSRFExpiresAt
}

func assertKBaseBrowserSessionUnauthorizedAndCleared(
	t *testing.T,
	response *httptest.ResponseRecorder,
	now time.Time,
) {
	t.Helper()
	if response.Code != http.StatusUnauthorized ||
		response.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("Cookie auth response = status %d body %q", response.Code, response.Body.String())
	}
	assertKBaseBrowserSessionClearedCookie(t, response, now)
}

func assertKBaseBrowserSessionClearedCookie(
	t *testing.T,
	response *httptest.ResponseRecorder,
	now time.Time,
) {
	t.Helper()
	cookie := requireKBaseBrowserSessionCookie(t, response)
	if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.Expires.Before(now) {
		t.Fatalf("cleared Cookie = %#v", cookie)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("cleared Cookie flags/scope = %#v", cookie)
	}
}

func assertKBaseEvidenceAuditErrorEnvelope(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantCode, wantError string,
) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode audit error envelope: %v; body=%s", err, response.Body.String())
	}
	if len(payload) != 2 || payload["code"] != wantCode || payload["error"] != wantError {
		t.Fatalf("audit error envelope = %#v, want code %q error %q", payload, wantCode, wantError)
	}
}

func newKBaseBrowserSessionHTTPTestHandler(
	t *testing.T,
	clock *browserSessionTestClock,
	randomSeed int,
) (http.Handler, *BrowserSessionStore) {
	t.Helper()
	return newKBaseBrowserSessionHTTPTestHandlerWithTTL(
		t,
		clock,
		randomSeed,
		testBrowserSessionCookieTTL,
	)
}

func newKBaseBrowserSessionHTTPTestHandlerWithTTL(
	t *testing.T,
	clock *browserSessionTestClock,
	randomSeed int,
	ttl time.Duration,
) (http.Handler, *BrowserSessionStore) {
	t.Helper()
	sessionDirectory := t.TempDir()
	if err := os.Chmod(sessionDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            filepath.Join(sessionDirectory, "browser-sessions.sqlite3"),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(randomSeed, 32)),
		TTL:             ttl,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sessionStore.Close(); err != nil {
			t.Errorf("close browser session store: %v", err)
		}
	})
	return newKBaseBrowserSessionHTTPTestHandlerForStore(t, sessionStore, ttl), sessionStore
}

func newKBaseBrowserSessionHTTPTestHandlerForStore(
	t *testing.T,
	sessionStore *BrowserSessionStore,
	ttl time.Duration,
) http.Handler {
	t.Helper()
	return NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:                NewBookKnowledgeStore(t.TempDir()),
		AuthToken:            testKBaseAuthToken,
		BrowserSessionSecret: testBrowserSessionSecret,
		BrowserSessions: BrowserSessionHTTPConfig{
			Store:           sessionStore,
			PublicOrigin:    testBrowserSessionOrigin,
			TTL:             ttl,
			RenewalInterval: 5 * time.Minute,
			MaxActive:       10,
		},
	})
}

func newKBaseSessionAdminHTTPTestHandler(
	t *testing.T,
	clock *browserSessionTestClock,
	randomSeed int,
) (http.Handler, *BrowserSessionStore) {
	t.Helper()
	sessionStore := newBrowserSessionStoreForAdminTest(t, clock, randomSeed)
	return newKBaseSessionAdminHTTPTestHandlerForStore(t, sessionStore), sessionStore
}

func newBrowserSessionStoreForAdminTest(
	t *testing.T,
	clock *browserSessionTestClock,
	randomSeed int,
) *BrowserSessionStore {
	t.Helper()
	sessionDirectory := t.TempDir()
	if err := os.Chmod(sessionDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            filepath.Join(sessionDirectory, "browser-sessions.sqlite3"),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(randomSeed, 32)),
		TTL:             testBrowserSessionCookieTTL,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sessionStore.Close(); err != nil {
			t.Errorf("close browser session store: %v", err)
		}
	})
	return sessionStore
}

func newKBaseSessionAdminHTTPTestHandlerForStore(
	t *testing.T,
	sessionStore *BrowserSessionStore,
) http.Handler {
	t.Helper()
	return NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:               NewBookKnowledgeStore(t.TempDir()),
		AuthToken:           testKBaseAuthToken,
		SourceAgentToken:    "dedicated-source-agent-token",
		AgentPublisherToken: "dedicated-publisher-token",
		BrowserSessions: BrowserSessionHTTPConfig{
			Store:           sessionStore,
			AdminToken:      testBrowserSessionAdminToken,
			PublicOrigin:    testBrowserSessionOrigin,
			TTL:             testBrowserSessionCookieTTL,
			RenewalInterval: 5 * time.Minute,
			MaxActive:       10,
		},
	})
}

func adminSessionRequest(method, path, token string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func serveAdminSessionRequest(
	handler http.Handler,
	method, path, token string,
) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, adminSessionRequest(method, path, token))
	return response
}

func assertKBaseBrowserSessionNoStore(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func assertKBaseBrowserSessionCookieTTL(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantTTL time.Duration,
	wantExpires time.Time,
) {
	t.Helper()
	cookie := requireKBaseBrowserSessionCookie(t, response)
	if cookie.MaxAge != int(wantTTL/time.Second) {
		t.Fatalf("session Cookie MaxAge = %d, want %d", cookie.MaxAge, int(wantTTL/time.Second))
	}
	if !cookie.Expires.Equal(wantExpires) {
		t.Fatalf("session Cookie Expires = %s, want %s", cookie.Expires, wantExpires)
	}
}

func assertKBaseBrowserSessionGenericServiceUnavailable(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if response.Code != http.StatusServiceUnavailable ||
		response.Body.String() != "{\"error\":\"service unavailable\"}\n" {
		t.Fatalf("store conflict response = status %d body %q", response.Code, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "conflict") {
		t.Fatalf("store conflict response exposed internal state: %s", response.Body.String())
	}
	assertKBaseBrowserSessionNoStore(t, response)
	assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(t, response)
}

func assertKBaseBrowserSessionPublicResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantDeviceLabel string,
) {
	t.Helper()
	var payload struct {
		Session BrowserSession `json:"session"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode browser session response: %v; body=%s", err, response.Body.String())
	}
	if payload.Session.ID == "" || payload.Session.DeviceLabel != wantDeviceLabel {
		t.Fatalf("public session metadata = %#v, want device label %q", payload.Session, wantDeviceLabel)
	}
	lowerBody := strings.ToLower(response.Body.String())
	for _, privateField := range []string{`"token"`, `"csrf`, `"user_agent"`, `"hash"`} {
		if strings.Contains(lowerBody, privateField) {
			t.Fatalf("browser session response contains private field %q: %s", privateField, response.Body.String())
		}
	}
}

func assertKBaseBrowserSessionResponseID(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantID string,
) {
	t.Helper()
	var payload struct {
		Session BrowserSession `json:"session"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode browser session response: %v; body=%s", err, response.Body.String())
	}
	if payload.Session.ID != wantID {
		t.Fatalf("session ID = %q, want %q", payload.Session.ID, wantID)
	}
}

func requireKBaseBrowserSessionCookie(
	t *testing.T,
	response *httptest.ResponseRecorder,
) *http.Cookie {
	t.Helper()
	var sessionCookies []*http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "__Host-kbase_session" {
			sessionCookies = append(sessionCookies, cookie)
		}
	}
	if len(sessionCookies) != 1 {
		t.Fatalf("session Cookies = %#v, want exactly one", sessionCookies)
	}
	return sessionCookies[0]
}

func assertKBaseBrowserSessionDoesNotLeakConfiguredSecrets(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	headers := fmt.Sprint(response.Header())
	for _, secret := range []string{testKBaseAuthToken, testBrowserSessionSecret} {
		if strings.Contains(response.Body.String(), secret) || strings.Contains(headers, secret) {
			t.Fatalf("response leaked configured secret: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
		}
	}
}

func assertKBaseBrowserSessionCount(
	t *testing.T,
	sessionStore *BrowserSessionStore,
	want int,
) {
	t.Helper()
	sessions, err := sessionStore.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != want {
		t.Fatalf("browser session count = %d, want %d", len(sessions), want)
	}
}

func TestKBaseHTTPHandlerAllowsDesktopCORSPreflight(t *testing.T) {
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/wcplus/status", nil)
	req.Header.Set("Origin", "wails://wails.localhost")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "wails://wails.localhost" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") || !strings.Contains(got, "Content-Type") {
		t.Fatalf("Access-Control-Allow-Headers = %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) || !strings.Contains(got, http.MethodPost) {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}

	untrustedReq := httptest.NewRequest(http.MethodOptions, "/api/wcplus/status", nil)
	untrustedReq.Header.Set("Origin", "https://example.invalid")
	untrustedReq.Header.Set("Access-Control-Request-Method", http.MethodGet)
	untrustedResp := httptest.NewRecorder()
	handler.ServeHTTP(untrustedResp, untrustedReq)
	if untrustedResp.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted origin received CORS header: %q", untrustedResp.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestKBaseHTTPHandlerServesSearchAndSystemKBExport(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	exportPath := filepath.Join(root, "artifacts", "system_kb_export.json")
	if err := os.MkdirAll(filepath.Dir(exportPath), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	exportPayload := map[string]any{
		"type":        "system_kb_v2_export",
		"schema_id":   "llm-wiki-v2-system-kb-export",
		"version":     "test-version",
		"source":      "dedao-kbase",
		"compiled_at": "2026-06-27T10:00:00Z",
		"stats":       map[string]any{"claim_count": 1},
		"pages":       []any{},
		"entities":    []any{},
		"claims":      []any{},
		"relations":   []any{},
	}
	data, err := json.Marshal(exportPayload)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := os.WriteFile(exportPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:              store,
		AuthToken:          "secret-token",
		SystemKBExportPath: exportPath,
	})

	searchResp := requestKBase(handler, http.MethodGet, "/api/search?q=MACD&limit=5", "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), `"results"`) || !strings.Contains(searchResp.Body.String(), `"42"`) {
		t.Fatalf("search response missing results: %s", searchResp.Body.String())
	}

	manifestResp := requestKBase(handler, http.MethodGet, "/api/system-kb/manifest", "secret-token")
	if manifestResp.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body=%s", manifestResp.Code, manifestResp.Body.String())
	}
	if !strings.Contains(manifestResp.Body.String(), `"version":"test-version"`) {
		t.Fatalf("manifest response missing version: %s", manifestResp.Body.String())
	}

	exportResp := requestKBase(handler, http.MethodGet, "/api/system-kb/export", "secret-token")
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export status = %d, body=%s", exportResp.Code, exportResp.Body.String())
	}
	if !strings.Contains(exportResp.Body.String(), `"type":"system_kb_v2_export"`) {
		t.Fatalf("export response missing payload: %s", exportResp.Body.String())
	}
}

func TestKBaseHTTPHandlerReadsBookWithLegacyReaderSuffix(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	pkg := sampleBookKnowledgePackageForExport()
	pkg.Book.BookID = "83477"
	pkg.Book.Title = "83477_测试书"
	if err := store.SavePackage(pkg); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books/83477-prompts", "secret-token")
	if resp.Code != http.StatusOK {
		t.Fatalf("legacy suffix status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"83477"`) {
		t.Fatalf("legacy suffix response did not resolve base book id: %s", resp.Body.String())
	}
}

func TestKBaseHTTPHandlerMissingBookDoesNotExposeFilesystemPath(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
	})

	resp := requestKBase(handler, http.MethodGet, "/api/books/missing-prompts", "secret-token")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing book status = %d, want 404, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, leak := range []string{root, "manifest.json", "book_knowledge"} {
		if strings.Contains(body, leak) {
			t.Fatalf("missing book response leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "book not found") {
		t.Fatalf("missing book response should be actionable: %s", body)
	}
}

func TestKBaseHTTPHandlerServesWebAssets(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	if err := store.SavePackage(sampleBookKnowledgePackageForExport()); err != nil {
		t.Fatalf("SavePackage returned error: %v", err)
	}
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), os.ModePerm); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<main class="reader-loading">reader</main>`), 0o644); err != nil {
		t.Fatalf("WriteFile index returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte(`console.log("reader")`), 0o644); err != nil {
		t.Fatalf("WriteFile asset returned error: %v", err)
	}

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		StaticDir: webDir,
	})

	indexResp := requestKBase(handler, http.MethodGet, "/", "")
	if indexResp.Code != http.StatusOK {
		t.Fatalf("index status = %d, body=%s", indexResp.Code, indexResp.Body.String())
	}
	if !strings.Contains(indexResp.Body.String(), `reader-loading`) {
		t.Fatalf("index response missing reader shell: %s", indexResp.Body.String())
	}
	if got := indexResp.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("index Cache-Control = %q, want no-store", got)
	}

	assetResp := requestKBase(handler, http.MethodGet, "/assets/app.js", "")
	if assetResp.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", assetResp.Code, assetResp.Body.String())
	}
	if !strings.Contains(assetResp.Body.String(), `console.log`) {
		t.Fatalf("asset response missing script: %s", assetResp.Body.String())
	}
	if got := assetResp.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("asset Cache-Control = %q, want no-cache", got)
	}

	readerRouteResp := requestKBase(handler, http.MethodGet, "/ebook/42", "")
	if readerRouteResp.Code != http.StatusOK {
		t.Fatalf("reader route status = %d, body=%s", readerRouteResp.Code, readerRouteResp.Body.String())
	}
	if !strings.Contains(readerRouteResp.Body.String(), `reader-loading`) {
		t.Fatalf("reader route did not fall back to index: %s", readerRouteResp.Body.String())
	}

	homeRouteResp := requestKBase(handler, http.MethodGet, "/home", "")
	if homeRouteResp.Code != http.StatusOK {
		t.Fatalf("home route status = %d, body=%s", homeRouteResp.Code, homeRouteResp.Body.String())
	}
	if !strings.Contains(homeRouteResp.Body.String(), `reader-loading`) {
		t.Fatalf("home route did not fall back to index: %s", homeRouteResp.Body.String())
	}

	courseRouteResp := requestKBase(handler, http.MethodGet, "/course", "")
	if courseRouteResp.Code != http.StatusOK {
		t.Fatalf("course route status = %d, body=%s", courseRouteResp.Code, courseRouteResp.Body.String())
	}
	if !strings.Contains(courseRouteResp.Body.String(), `reader-loading`) {
		t.Fatalf("course route did not fall back to index: %s", courseRouteResp.Body.String())
	}

	missingAssetResp := requestKBase(handler, http.MethodGet, "/assets/missing.js", "")
	if missingAssetResp.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missingAssetResp.Code)
	}

	apiResp := requestKBase(handler, http.MethodGet, "/api/books", "")
	if apiResp.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d, want 401", apiResp.Code)
	}
}

func TestKBaseHTTPHandlerImportsWeChatArticleIntoBookKnowledge(t *testing.T) {
	articleServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
  <body>
    <h1 id="activity-name">健康验证方法</h1>
    <a id="js_name">健康知识</a>
    <em id="publish_time">2026-07-06</em>
    <div id="js_content"><p>用指标和来源交叉验证结论。</p></div>
  </body>
</html>`)
	}))
	defer articleServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WeChat:    newTestWeChatSourceService(t, articleServer),
	})

	body := bytes.NewBufferString(`{"url":"` + articleServer.URL + `/s/test","book_id":"wechat-health"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/wechat/import", body)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wechat-health"`) {
		t.Fatalf("import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wechat-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Title != "健康验证方法" {
		t.Fatalf("book title = %q", pkg.Book.Title)
	}
	if len(pkg.Chunks) != 1 || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected chunks: %#v", pkg.Chunks)
	}
	if len(pkg.Citations) != 1 || pkg.Citations[0].SourceHTML != articleServer.URL+"/s/test" {
		t.Fatalf("unexpected citations: %#v", pkg.Citations)
	}
}

func TestKBaseHTTPHandlerProxiesAndImportsWCPlusArticles(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/gzh/list":
			fmt.Fprint(w, `{"success":true,"data":{"gzhs":[{"biz":"biz-1","nickname":"医学参考","article_count":2}],"total":1}}`)
		case "/api/report/gzh_articles":
			fmt.Fprint(w, `{"success":true,"data":{"gzh":{"biz":"biz-1","nickname":"医学参考"},"articles":[{"id":"wx-1","title":"验证文章","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/wx1","digest":"摘要","publish_time":"2026-07-06"}],"total":1}}`)
		case "/api/article/content":
			fmt.Fprintf(w, `{"success":true,"data":{"id":"%s","title":"验证文章 %s","nickname":"医学参考","url":"https://mp.weixin.qq.com/s/%s","content":"# 验证文章\n\n指标交叉验证。","publish_time":"2026-07-06"}}`, r.URL.Query().Get("id"), r.URL.Query().Get("id"), r.URL.Query().Get("id"))
		case "/api/task/all":
			fmt.Fprint(w, `{"success":true,"data":{"tasks":[{"task_id":"task-1","biz":"biz-1","nickname":"医学参考","status":"running"}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	listResp := requestKBase(handler, http.MethodGet, "/api/wcplus/gzh/list?offset=0&num=10", "secret-token")
	if listResp.Code != http.StatusOK {
		t.Fatalf("gzh list status = %d, body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"biz":"biz-1"`) {
		t.Fatalf("gzh list response missing account: %s", listResp.Body.String())
	}

	contentResp := requestKBase(handler, http.MethodGet, "/api/wcplus/article/content?nickname="+url.QueryEscape("医学参考")+"&id=wx-1", "secret-token")
	if contentResp.Code != http.StatusOK {
		t.Fatalf("content status = %d, body=%s", contentResp.Code, contentResp.Body.String())
	}
	if !strings.Contains(contentResp.Body.String(), `"content"`) || !strings.Contains(contentResp.Body.String(), "指标交叉验证") {
		t.Fatalf("content response missing article content: %s", contentResp.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/article", bytes.NewBufferString(`{"nickname":"医学参考","id":"wx-1","book_id":"wcplus-health"}`))
	importReq.Header.Set("Authorization", "Bearer secret-token")
	importResp := httptest.NewRecorder()
	handler.ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", importResp.Code, importResp.Body.String())
	}
	pkg, err := store.LoadPackage("wcplus-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "指标交叉验证") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}

	batchReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/account", bytes.NewBufferString(`{"biz":"biz-1","nickname":"医学参考","limit":1}`))
	batchReq.Header.Set("Authorization", "Bearer secret-token")
	batchResp := httptest.NewRecorder()
	handler.ServeHTTP(batchResp, batchReq)
	if batchResp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", batchResp.Code, batchResp.Body.String())
	}
	if !strings.Contains(batchResp.Body.String(), `"imported_count":1`) {
		t.Fatalf("batch import response missing count: %s", batchResp.Body.String())
	}

	taskResp := requestKBase(handler, http.MethodGet, "/api/wcplus/task/all", "secret-token")
	if taskResp.Code != http.StatusOK {
		t.Fatalf("task status = %d, body=%s", taskResp.Code, taskResp.Body.String())
	}
	if !strings.Contains(taskResp.Body.String(), `"task_id":"task-1"`) {
		t.Fatalf("task response missing task: %s", taskResp.Body.String())
	}
}

func TestKBaseHTTPHandlerPreviewsAndImportsWCPlusArticleByURL(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/article/content":
			if got := r.URL.Query().Get("url"); got != "https://mp.weixin.qq.com/s/url-only" {
				t.Fatalf("url = %q", got)
			}
			fmt.Fprint(w, `{"success":true,"data":{"id":"url-only","title":"URL 文章","nickname":"URL 公众号","url":"https://mp.weixin.qq.com/s/url-only","content":"# URL 文章\n\n只通过链接也能预览和导入。","publish_time":"2026-07-08"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	contentResp := requestKBase(handler, http.MethodGet, "/api/wcplus/article/content?url="+url.QueryEscape("https://mp.weixin.qq.com/s/url-only"), "secret-token")
	if contentResp.Code != http.StatusOK {
		t.Fatalf("content by URL status = %d, body=%s", contentResp.Code, contentResp.Body.String())
	}
	if !strings.Contains(contentResp.Body.String(), "只通过链接") {
		t.Fatalf("content by URL response missing body: %s", contentResp.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/article", bytes.NewBufferString(`{"url":"https://mp.weixin.qq.com/s/url-only","book_id":"wcplus-url-only"}`))
	importReq.Header.Set("Authorization", "Bearer secret-token")
	importResp := httptest.NewRecorder()
	handler.ServeHTTP(importResp, importReq)
	if importResp.Code != http.StatusOK {
		t.Fatalf("import by URL status = %d, body=%s", importResp.Code, importResp.Body.String())
	}
	pkg, err := store.LoadPackage("wcplus-url-only")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if !strings.Contains(pkg.Chunks[0].Text, "只通过链接") {
		t.Fatalf("unexpected imported URL package: %#v", pkg)
	}
}

func TestKBaseHTTPHandlerImportsRawWCPlusArticle(t *testing.T) {
	root := t.TempDir()
	store := NewBookKnowledgeStore(filepath.Join(root, "book_knowledge"))
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     store,
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/import/raw", bytes.NewBufferString(`{
		"title":"人工导入文章",
		"nickname":"医学参考",
		"url":"https://mp.weixin.qq.com/s/manual",
		"content":"# 人工导入文章\n\n用指标和来源交叉验证结论。",
		"book_id":"wcplus-manual-health"
	}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("raw import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"book_id":"wcplus-manual-health"`) {
		t.Fatalf("raw import response missing book id: %s", resp.Body.String())
	}

	pkg, err := store.LoadPackage("wcplus-manual-health")
	if err != nil {
		t.Fatalf("LoadPackage returned error: %v", err)
	}
	if pkg.Book.Extractor != "wcplus-source-adapter" || !strings.Contains(pkg.Chunks[0].Text, "交叉验证结论") {
		t.Fatalf("unexpected imported package: %#v", pkg)
	}
}

func TestKBaseHTTPHandlerProxiesAdvancedWCPlusAPIs(t *testing.T) {
	var sawQueueRun bool
	var sawBatchCreate bool
	var sawBatchDelete bool
	var sawXLSXExport bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/search/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if got := r.URL.Query().Get("q"); got != "血压" {
				t.Fatalf("search q = %q", got)
			}
			fmt.Fprint(w, `{"Results":[{"ID":"wx-1","Title":"血压验证"}],"Total":1}`)
		case "/api/gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzhs":[{"Biz":"biz-1","Nickname":"医学参考"}],"Total":1}`)
		case "/api/search_gzh/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-2","Nickname":"候选公众号"}],"Total":1}`)
		case "/api/article/search_title":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-2","Title":"标题搜索"}],"Total":1}`)
		case "/api/article/all_articles":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[{"ID":"wx-3","Title":"全库文章"}],"Total":1}`)
		case "/api/report/reading_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Rows":[{"date":"2026-07-06","read_num":42}]}`)
		case "/api/report/statistic_data":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"total_read":42}`)
		case "/api/article/gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Biz":"biz-1","Nickname":"医学参考"}`)
		case "/api/like_article/get_all":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Articles":[]}`)
		case "/api/req_data/get_gzh":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `{"Gzh":{"Biz":"biz-1","Nickname":"医学参考"}}`)
		case "/api/article/export_text":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `2`)
		case "/api/gzh/export_csv":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprint(w, `3`)
		case "/api/task/control":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode task control body: %v", err)
			}
			if payload["command"] != "run" {
				t.Fatalf("task control body = %#v", payload)
			}
			sawQueueRun = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		case "/api/batch_task/create_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchCreate = true
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"batch-1","status":"ready"}}`)
		case "/api/batch_task/delete_task":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			sawBatchDelete = true
			fmt.Fprint(w, `{"success":true,"data":{"deleted":1}}`)
		case "/api/article/all_articles/export_xlsx":
			sawXLSXExport = true
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			fmt.Fprint(w, "xlsx-bytes")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	statusResp := requestKBase(handler, http.MethodGet, "/api/wcplus/status", "secret-token")
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status status = %d, body=%s", statusResp.Code, statusResp.Body.String())
	}
	if !strings.Contains(statusResp.Body.String(), `"ok":true`) {
		t.Fatalf("status response missing ok: %s", statusResp.Body.String())
	}

	searchResp := requestKBase(handler, http.MethodGet, "/api/wcplus/search?q="+url.QueryEscape("血压"), "secret-token")
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status = %d, body=%s", searchResp.Code, searchResp.Body.String())
	}
	if !strings.Contains(searchResp.Body.String(), "血压验证") {
		t.Fatalf("search response missing result: %s", searchResp.Body.String())
	}

	for _, path := range []string{
		"/api/wcplus/gzh/search?q=test",
		"/api/wcplus/search-gzh?q=test",
		"/api/wcplus/article/search-title?q=test",
		"/api/wcplus/article/all?offset=0&num=10",
		"/api/wcplus/report/reading-data?biz=biz-1",
		"/api/wcplus/report/statistic-data?biz=biz-1",
		"/api/wcplus/article/gzh?id=wx-1",
		"/api/wcplus/like-articles?offset=0&num=10",
		"/api/wcplus/request/gzh?biz=biz-1",
		"/api/wcplus/export/text?biz=biz-1&nickname=test",
		"/api/wcplus/export/gzh-csv?biz=biz-1&nickname=test",
	} {
		resp := requestKBase(handler, http.MethodGet, path, "secret-token")
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, resp.Code, resp.Body.String())
		}
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/task/control", bytes.NewBufferString(`{"command":"run"}`))
	queueReq.Header.Set("Authorization", "Bearer secret-token")
	queueResp := httptest.NewRecorder()
	handler.ServeHTTP(queueResp, queueReq)
	if queueResp.Code != http.StatusOK || !sawQueueRun {
		t.Fatalf("queue run status = %d, body=%s", queueResp.Code, queueResp.Body.String())
	}

	batchCreateReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/create", bytes.NewBufferString(`{"nickname":"医学参考"}`))
	batchCreateReq.Header.Set("Authorization", "Bearer secret-token")
	batchCreateResp := httptest.NewRecorder()
	handler.ServeHTTP(batchCreateResp, batchCreateReq)
	if batchCreateResp.Code != http.StatusOK || !sawBatchCreate {
		t.Fatalf("batch create status = %d, body=%s", batchCreateResp.Code, batchCreateResp.Body.String())
	}

	batchDeleteReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-task/delete", bytes.NewBufferString(`{"status":"ready"}`))
	batchDeleteReq.Header.Set("Authorization", "Bearer secret-token")
	batchDeleteResp := httptest.NewRecorder()
	handler.ServeHTTP(batchDeleteResp, batchDeleteReq)
	if batchDeleteResp.Code != http.StatusOK || !sawBatchDelete {
		t.Fatalf("batch delete status = %d, body=%s", batchDeleteResp.Code, batchDeleteResp.Body.String())
	}

	xlsxReq := httptest.NewRequest(http.MethodPost, "/api/wcplus/export/all-articles-xlsx", bytes.NewBufferString(`{"range_mode":"recent","recent_num":10,"fields":["title"]}`))
	xlsxReq.Header.Set("Authorization", "Bearer secret-token")
	xlsxResp := httptest.NewRecorder()
	handler.ServeHTTP(xlsxResp, xlsxReq)
	if xlsxResp.Code != http.StatusOK || !sawXLSXExport || xlsxResp.Body.String() != "xlsx-bytes" {
		t.Fatalf("xlsx export status = %d, body=%q", xlsxResp.Code, xlsxResp.Body.String())
	}
	if got := xlsxResp.Header().Get("Content-Type"); !strings.Contains(got, "spreadsheetml") {
		t.Fatalf("xlsx content type = %q", got)
	}
}

func TestKBaseHTTPHandlerChecksEnvAndBatchImportsWCPlusNicknames(t *testing.T) {
	var created []map[string]any
	var queueStarted bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html>wcplus</html>`)
		case "/api/gzh/list":
			fmt.Fprint(w, `{"Gzhs":[],"Total":0}`)
		case "/api/search_gzh/search":
			keyword := r.URL.Query().Get("keyword")
			if keyword == "" {
				keyword = r.URL.Query().Get("q")
			}
			switch keyword {
			case "医学参考":
				fmt.Fprint(w, `{"Candidates":[{"Biz":"biz-med","Nickname":"医学参考"}],"Total":1}`)
			default:
				fmt.Fprint(w, `{"Candidates":[],"Total":0}`)
			}
		case "/api/task/new":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create task body: %v", err)
			}
			created = append(created, payload)
			fmt.Fprint(w, `{"success":true,"data":{"task_id":"task-1","status":"ready"}}`)
		case "/api/task/control":
			queueStarted = true
			fmt.Fprint(w, `{"success":true,"data":{"status":"running"}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:     NewBookKnowledgeStore(t.TempDir()),
		AuthToken: "secret-token",
		WCPlus:    NewWCPlusSourceService(WCPlusSourceConfig{BaseURL: apiServer.URL}),
	})

	envResp := requestKBase(handler, http.MethodGet, "/api/wcplus/env/check", "secret-token")
	if envResp.Code != http.StatusOK {
		t.Fatalf("env check status = %d, body=%s", envResp.Code, envResp.Body.String())
	}
	if !strings.Contains(envResp.Body.String(), `"ok":true`) || !strings.Contains(envResp.Body.String(), `"gzh_list"`) {
		t.Fatalf("env check response missing details: %s", envResp.Body.String())
	}

	body := `{"nicknames":["医学参考","不存在"],"articleListType":"amount","articleListAmount":20,"start_queue":true,"exact_match":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/wcplus/batch-import/gzh", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch import status = %d, body=%s", resp.Code, resp.Body.String())
	}
	if len(created) != 1 || created[0]["crawlerType"] != "gzh_article_link" {
		t.Fatalf("unexpected created tasks: %#v", created)
	}
	if !queueStarted {
		t.Fatalf("queue was not started")
	}
	if !strings.Contains(resp.Body.String(), `"success"`) || !strings.Contains(resp.Body.String(), `"failed"`) {
		t.Fatalf("batch import response missing lists: %s", resp.Body.String())
	}
}

func requestKBase(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func requestJSONKBase(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func newKBaseSourceAgentCommandHTTPFixture(
	t testing.TB,
) (http.Handler, *SourceSyncStore, *sourceSyncTestClock, *BrowserSessionStore) {
	t.Helper()
	root := t.TempDir()
	clock := newSourceSyncTestClock(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	sourceSync, err := newSourceSyncStore(root, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sourceSync.Close(); err != nil {
			t.Errorf("close source sync store: %v", err)
		}
	})
	for _, agentID := range []string{"agent-a", "agent-b"} {
		if _, err := sourceSync.HeartbeatAgent(SourceAgentHeartbeat{
			AgentID: agentID, WorkerType: "wechat-worker", Platform: "darwin", Architecture: "arm64",
			Version: "1.0.0", ProtocolVersion: "2026-08-01",
			Capabilities: []string{"sync_articles"},
			CapabilityHealth: map[string]SourceCapabilityHealth{
				"wechat": {Healthy: true},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	artifactRoot := t.TempDir()
	artifactFiles := map[string][]byte{
		"artifact-2":       []byte("artifact-2-bytes"),
		"artifact-3":       []byte("artifact-3-bytes"),
		"private-artifact": []byte("private-artifact-bytes"),
		"artifact-worker":  []byte("artifact-worker-bytes"),
	}
	artifacts := make([]SourceAgentArtifact, 0, len(artifactFiles))
	files := make(map[string][]byte, len(artifactFiles))
	for id, data := range artifactFiles {
		storageKey := "artifacts/" + id
		artifacts = append(artifacts, validSourceAgentArtifactForTest(id, storageKey, data))
		files[storageKey] = data
	}
	writeSourceAgentArtifactFixture(t, artifactRoot, artifacts, files)
	artifactCatalog, err := NewSourceAgentArtifactCatalog(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}

	browserDirectory := t.TempDir()
	if err := os.Chmod(browserDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	browserSessions, err := NewBrowserSessionStore(BrowserSessionStoreConfig{
		Path:            filepath.Join(browserDirectory, "browser-sessions.sqlite3"),
		Now:             clock.Now,
		Random:          bytes.NewReader(deterministicBrowserSessionBytes(880, 16)),
		TTL:             testBrowserSessionCookieTTL,
		RenewalInterval: 5 * time.Minute,
		MaxActive:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := browserSessions.Close(); err != nil {
			t.Errorf("close browser session store: %v", err)
		}
	})
	handler := NewKBaseHTTPHandler(KBaseHTTPConfig{
		Store:            NewBookKnowledgeStore(root),
		AuthToken:        "admin-secret",
		SourceSync:       sourceSync,
		SourceAgentToken: "agent-secret",
		SourceArtifacts:  artifactCatalog,
		BrowserSessions: BrowserSessionHTTPConfig{
			Store:           browserSessions,
			PublicOrigin:    testBrowserSessionOrigin,
			TTL:             testBrowserSessionCookieTTL,
			RenewalInterval: 5 * time.Minute,
			MaxActive:       10,
		},
	})
	return handler, sourceSync, clock, browserSessions
}

func sourceAgentArtifactRootFromHandlerForTest(t *testing.T, handler http.Handler) string {
	t.Helper()
	concrete, ok := handler.(*kbaseHTTPHandler)
	if !ok || concrete.sourceArtifacts == nil {
		t.Fatal("handler has no source artifact catalog")
	}
	return concrete.sourceArtifacts.root
}

func setSourceAgentArtifactRolloutForTest(t *testing.T, root, artifactID string, allowed bool) {
	t.Helper()
	catalog, err := NewSourceAgentArtifactCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := catalog.load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range artifacts {
		if artifacts[index].ID == artifactID {
			artifacts[index].AllowedForRollout = allowed
			found = true
		}
	}
	if !found {
		t.Fatalf("artifact %q not found", artifactID)
	}
	writeSourceAgentArtifactCatalog(t, root, artifacts)
}

type fakeDedaoLibrary struct{}

func (fakeDedaoLibrary) CourseList(category, order string, page, limit int) (*services.CourseList, error) {
	title := map[string]string{
		CateCourse:    "得到订阅课程",
		CateEbook:     "得到订阅电子书",
		CateAudioBook: "得到订阅读书",
	}[category]
	if title == "" {
		title = "得到订阅内容"
	}
	return &services.CourseList{
		List: []services.Course{{
			Enid:       category + "-enid",
			ID:         101,
			ClassID:    202,
			Title:      title,
			Intro:      "从得到账号读取的订阅内容",
			Author:     "得到",
			Icon:       "https://example.test/icon.png",
			Progress:   12,
			CourseNum:  30,
			PublishNum: 8,
		}},
		ISMore: 1,
		Page:   page,
	}, nil
}

func (fakeDedaoLibrary) CourseInfo(enid string) (*services.CourseInfo, error) {
	return &services.CourseInfo{
		ClassInfo: services.ClassInfo{
			Enid:                enid,
			Name:                "得到订阅课程详情",
			Intro:               "课程简介",
			LecturerName:        "得到讲师",
			CurrentArticleCount: 1,
			PhaseNum:            20,
		},
	}, nil
}

func (fakeDedaoLibrary) ArticleList(enid, chapterID string, count, maxID int) (*services.ArticleList, error) {
	return &services.ArticleList{
		List: []services.ArticleIntro{{
			ArticleBase: services.ArticleBase{
				ID:        1,
				Enid:      "article-enid",
				ClassEnid: enid,
				Title:     "第一讲文章列表",
			},
		}},
	}, nil
}

func (fakeDedaoLibrary) ArticleDetail(enid string) (*services.ArticleDetail, error) {
	return &services.ArticleDetail{
		Content: `[{"type":"header","level":1,"text":"正文标题"}]`,
	}, nil
}

func (fakeDedaoLibrary) AudioDetail(enid string) (*services.AudioInfoResp, error) {
	return &services.AudioInfoResp{AudioInfo: services.AudioInfo{
		AudioID:      "audio-alias",
		Title:        "得到听书详情",
		AudioSummary: "听书摘要",
		TopicSummary: []services.TopicSummary{{Title: "核心内容", SubTitle: "主题摘要"}},
	}}, nil
}

func (fakeDedaoLibrary) OdobArticleDetail(aliasID string) (*services.ArticleDetail, error) {
	return &services.ArticleDetail{Content: `[{"type":"header","level":1,"text":"听书文稿"}]`}, nil
}
