package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProofroomProjectionIsBoundedValidatedAndPrivacyMinimized(t *testing.T) {
	input := validEvidenceAuditInput()
	input.Subject = "patient@example.test bearer private-token"
	input.Scope = "patient 42 oncology decision"
	store, audit := completedEvidenceAuditForProofroomInputTest(t, t.TempDir(), input)

	preview, err := PreviewEvidenceAuditProofroom(store, audit.AuditID)
	if err != nil {
		t.Fatalf("PreviewEvidenceAuditProofroom() error = %v", err)
	}
	if preview.Payload.SchemaVersion != ProofroomEvidenceAuditSchemaVersion ||
		preview.Payload.Audit.AuditID != audit.AuditID ||
		preview.Payload.Audit.OutputHash != audit.OutputHash ||
		preview.Payload.TraceID != audit.TraceID ||
		preview.Payload.Package.ContentHash != audit.Package.ContentHash {
		t.Fatalf("projection identity = %#v", preview.Payload)
	}
	if preview.Payload.SubjectIdentity == "" || preview.Payload.ScopeIdentity == "" ||
		!strings.HasPrefix(preview.Payload.SubjectIdentity, "sha256:") ||
		!strings.HasPrefix(preview.Payload.ScopeIdentity, "sha256:") {
		t.Fatalf("privacy identities = subject:%q scope:%q",
			preview.Payload.SubjectIdentity, preview.Payload.ScopeIdentity)
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"patient@example.test", "private-token", "patient 42 oncology decision"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("projection leaked %q: %s", secret, raw)
		}
	}
	if len(preview.Payload.Claims) != 1 ||
		len(preview.Payload.Claims[0].Evidence) != 2 ||
		preview.Payload.Claims[0].Evidence[1].CitationID == "" ||
		preview.Payload.Claims[0].Evidence[1].FreshnessDecision == "" {
		t.Fatalf("claim projection = %#v", preview.Payload.Claims)
	}
	if preview.Payload.AdjudicationAuthority != "proofroom" ||
		preview.Payload.KBaseDecisionFinal {
		t.Fatalf("adjudication contract = %#v", preview.Payload)
	}
	if preview.PayloadHash == "" || preview.Summary == "" {
		t.Fatalf("preview metadata = %#v", preview)
	}
	if _, err := os.Stat(store.EvidenceAuditProofroomDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote delivery state: %v", err)
	}
}

func TestProofroomProjectionRejectsNonCompletedOrTamperedAudit(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	queued, _, err := CreateEvidenceAudit(
		store, validEvidenceAuditInput(), "proofroom-queued", testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewEvidenceAuditProofroom(store, queued.AuditID); !errors.Is(err, ErrProofroomAuditNotReady) {
		t.Fatalf("queued preview error = %v", err)
	}

	_, completed := completedEvidenceAuditForProofroomTest(t)
	completed.OutputHash = "sha256:" + strings.Repeat("0", 64)
	if _, err := BuildProofroomEvidenceAuditProjection(completed); !errors.Is(err, ErrProofroomAuditInvalid) {
		t.Fatalf("tampered projection error = %v", err)
	}
}

func TestProofroomPreviewUsesReadOnlySnapshot(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	queued, _, err := CreateEvidenceAudit(
		store, validEvidenceAuditInput(), "proofroom-read-only", testAgentPackageTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.EvidenceAuditManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewEvidenceAuditProofroom(store, queued.AuditID); !errors.Is(err, ErrProofroomAuditNotReady) {
		t.Fatalf("queued preview error = %v", err)
	}
	after, err := os.ReadFile(store.EvidenceAuditManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("GET-compatible preview changed the audit manifest")
	}
}

func TestProofroomProjectionPreservesReviewContractWithoutRawClaims(t *testing.T) {
	_, audit := completedEvidenceAuditForProofroomTest(t)
	audit.Proofroom.Title = "Independent adjudication"
	audit.Proofroom.ReviewItems = []string{"Verify allocation concealment", "Resolve endpoint conflict"}
	audit.ClaimAudits[0].NormalizedStatement = "Bearer secret-token api_key=private password=hunter2"
	audit.ClaimAudits[0].Limitations = []string{
		"Patient Alice +1 212-555-0199; 患者 张三 身份证 11010519491231002X",
	}
	audit.Summary.Conclusion = "Contact alice@example.test before review"
	finalized, err := FinalizeEvidenceAuditReport(audit)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := BuildProofroomEvidenceAuditProjection(finalized)
	if err != nil {
		t.Fatalf("BuildProofroomEvidenceAuditProjection() error = %v", err)
	}
	if preview.Payload.Proofroom.Title.Text == "" ||
		len(preview.Payload.Proofroom.ReviewItems) != 2 {
		t.Fatalf("proofroom review contract = %#v", preview.Payload.Proofroom)
	}
	if preview.Payload.Claims[0].SourceClaimIdentity == "" {
		t.Fatalf("source claim was not identity-only: %#v", preview.Payload.Claims[0])
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		audit.ClaimAudits[0].SourceClaim, "alice@example.test", "212-555-0199", "secret-token",
		"private", "hunter2", "张三", "11010519491231002X",
	} {
		if strings.Contains(string(raw), value) {
			t.Fatalf("projection leaked %q: %s", value, raw)
		}
	}
}

func TestProofroomDeliveryIsExplicitIdempotentAndStoresPrivateImmutableReceipt(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	var calls atomic.Int32
	client := proofroomHTTPClientFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != http.MethodPost ||
			r.Header.Get("Authorization") != "Bearer remote-secret" ||
			r.Header.Get("Idempotency-Key") == "" {
			t.Fatalf("remote request method=%s auth=%q key=%q",
				r.Method, r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"))
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, proofroomMaxPayloadBytes+1))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "remote-secret") {
			t.Fatal("payload contains remote token")
		}
		return proofroomJSONResponse(
			http.StatusOK,
			`{"receipt_id":"proofroom-receipt-1","status":"accepted"}`,
		), nil
	})

	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint:                  "http://127.0.0.1/proofroom",
		Token:                     "remote-secret",
		Client:                    client,
		AllowPrivateTestEndpoints: true,
		Now:                       func() time.Time { return testAgentPackageTime().Add(12 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := service.Deliver(context.Background(), store, audit.AuditID, "delivery-key-1")
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if !created || first.Status != ProofroomDeliveryDelivered ||
		first.RemoteReceiptID != "proofroom-receipt-1" ||
		first.ProjectionHash == "" || first.ReceiptHash == "" {
		t.Fatalf("first receipt = %#v created=%v", first, created)
	}
	replayed, created, err := service.Deliver(context.Background(), store, audit.AuditID, "delivery-key-1")
	if err != nil || created || replayed.ReceiptHash != first.ReceiptHash || calls.Load() != 1 {
		t.Fatalf("replay receipt=%#v created=%v calls=%d err=%v",
			replayed, created, calls.Load(), err)
	}
	info, err := os.Stat(store.EvidenceAuditProofroomReceiptPath(first.ReceiptHash))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode=%#o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(store.EvidenceAuditProofroomReceiptPath(first.ReceiptHash))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "remote-secret") || strings.Contains(string(raw), "delivery-key-1") {
		t.Fatalf("receipt leaked secret/idempotency key: %s", raw)
	}
}

func TestProofroomDeliverySerializesAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	firstStore, audit := completedEvidenceAuditForProofroomTestAtRoot(t, root)
	secondStore := NewBookKnowledgeStore(root)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	client := proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return proofroomJSONResponse(http.StatusOK, `{"receipt_id":"once","status":"accepted"}`), nil
	})
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/api/evidence-audits",
		Token:    "remote-secret", Client: client,
		Now: func() time.Time { return testAgentPackageTime().Add(13 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, store := range []*BookKnowledgeStore{firstStore, secondStore} {
		wg.Add(1)
		go func(store *BookKnowledgeStore) {
			defer wg.Done()
			_, _, err := service.Deliver(context.Background(), store, audit.AuditID, "same-key")
			errs <- err
		}(store)
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Deliver() error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("remote calls=%d, want 1", calls.Load())
	}
}

func TestProofroomDeliverySerializesAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	_, audit := completedEvidenceAuditForProofroomTestAtRoot(t, root)
	countPath := filepath.Join(root, "remote-call-count")
	commands := make([]*exec.Cmd, 0, 2)
	outputs := make([]bytes.Buffer, 2)
	for index := 0; index < 2; index++ {
		command := exec.Command(os.Args[0], "-test.run=^TestProofroomDeliveryCrossProcessHelper$")
		command.Env = append(os.Environ(),
			"PROOFROOM_CROSS_PROCESS_HELPER=1",
			"PROOFROOM_CROSS_PROCESS_ROOT="+root,
			"PROOFROOM_CROSS_PROCESS_AUDIT="+audit.AuditID,
			"PROOFROOM_CROSS_PROCESS_COUNT="+countPath,
		)
		command.Stdout = &outputs[index]
		command.Stderr = &outputs[index]
		commands = append(commands, command)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("cross-process helper failed: %v\n%s", err, outputs[index].String())
		}
	}
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "1\n" {
		t.Fatalf("cross-process remote calls = %q", count)
	}
}

func TestProofroomDeliveryCrossProcessHelper(t *testing.T) {
	if os.Getenv("PROOFROOM_CROSS_PROCESS_HELPER") != "1" {
		t.Skip("helper process only")
	}
	root := os.Getenv("PROOFROOM_CROSS_PROCESS_ROOT")
	auditID := os.Getenv("PROOFROOM_CROSS_PROCESS_AUDIT")
	countPath := os.Getenv("PROOFROOM_CROSS_PROCESS_COUNT")
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			file, err := os.OpenFile(countPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			if _, err := file.WriteString("1\n"); err != nil {
				_ = file.Close()
				return nil, err
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
			return proofroomJSONResponse(
				http.StatusOK, `{"receipt_id":"cross-process","status":"accepted"}`,
			), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Deliver(
		context.Background(), NewBookKnowledgeStore(root), auditID, "cross-process-key",
	); err != nil {
		t.Fatal(err)
	}
}

func TestProofroomDeliveryUnknownDoesNotAutomaticallyRepeatPOST(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	var calls atomic.Int32
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/api/evidence-audits",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, context.DeadlineExceeded
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, _, err := service.Deliver(context.Background(), store, audit.AuditID, "unknown-key")
		if !errors.Is(err, ErrProofroomDeliveryOutcomeUnknown) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unknown outcome repeated remote POST: calls=%d", calls.Load())
	}
	if _, _, err := service.Deliver(context.Background(), store, audit.AuditID, "different-key"); !errors.Is(err, ErrProofroomDeliveryOutcomeUnknown) {
		t.Fatalf("different key bypassed unknown outcome: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unknown outcome bypass repeated remote POST: calls=%d", calls.Load())
	}
	if err := CoordinateProofroomDelivery(
		store, audit.AuditID, "unknown-key", ProofroomCoordinationConfirmedNotDelivered,
		testAgentPackageTime().Add(14*time.Hour),
	); err != nil {
		t.Fatalf("CoordinateProofroomDelivery() error = %v", err)
	}
}

func TestProofroomDeliveryInvalidOrOversizedResponseRemainsUnknown(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"receipt_id":`},
		{name: "oversized body", body: strings.Repeat("x", proofroomMaxResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, audit := completedEvidenceAuditForProofroomTest(t)
			var calls atomic.Int32
			service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
				Endpoint: "https://proofroom.example.test/deliver",
				Token:    "remote-secret",
				Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return proofroomJSONResponse(http.StatusOK, test.body), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, _, err := service.Deliver(
					context.Background(), store, audit.AuditID, "invalid-response-key",
				); !errors.Is(err, ErrProofroomDeliveryOutcomeUnknown) {
					t.Fatalf("attempt %d error = %v", attempt, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("invalid response repeated POST: calls=%d", calls.Load())
			}
			if entries, err := os.ReadDir(filepath.Join(store.EvidenceAuditProofroomDir(), "receipts")); err == nil && len(entries) > 0 {
				t.Fatalf("invalid response created receipts: %#v", entries)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}

func TestProofroomDeliveryClassifiesRemoteOutcomesFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       error
	}{
		{name: "accepted", statusCode: 200, body: `{"receipt_id":"r-1","status":"accepted"}`},
		{name: "business rejected", statusCode: 200, body: `{"receipt_id":"r-2","status":"rejected"}`, want: ErrProofroomDeliveryRejected},
		{name: "business failed", statusCode: 200, body: `{"receipt_id":"r-3","status":"failed"}`, want: ErrProofroomDeliveryRejected},
		{name: "request timeout", statusCode: 408, body: `{}`, want: ErrProofroomDeliveryOutcomeUnknown},
		{name: "rate limited", statusCode: 429, body: `{}`, want: ErrProofroomDeliveryOutcomeUnknown},
		{name: "server failure", statusCode: 503, body: `{}`, want: ErrProofroomDeliveryOutcomeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, audit := completedEvidenceAuditForProofroomTest(t)
			var calls atomic.Int32
			service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
				Endpoint: "https://proofroom.example.test/api/audits",
				Token:    "remote-secret",
				Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return proofroomJSONResponse(test.statusCode, test.body), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			receipt, _, gotErr := service.Deliver(context.Background(), store, audit.AuditID, "classification-key")
			if test.want == nil {
				if gotErr != nil || receipt.Status != ProofroomDeliveryDelivered {
					t.Fatalf("receipt=%#v error=%v", receipt, gotErr)
				}
				return
			}
			if !errors.Is(gotErr, test.want) {
				t.Fatalf("error=%v, want %v", gotErr, test.want)
			}
			if _, _, secondErr := service.Deliver(context.Background(), store, audit.AuditID, "classification-key"); !errors.Is(secondErr, test.want) {
				t.Fatalf("second error=%v, want %v", secondErr, test.want)
			}
			if calls.Load() != 1 {
				t.Fatalf("remote calls=%d, want 1", calls.Load())
			}
			if errors.Is(test.want, ErrProofroomDeliveryRejected) {
				entries, readErr := os.ReadDir(filepath.Join(store.EvidenceAuditProofroomDir(), "receipts"))
				if readErr == nil && len(entries) != 0 {
					t.Fatalf("rejected outcome created receipt: %#v", entries)
				}
				if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
					t.Fatal(readErr)
				}
			}
		})
	}
}

func TestProofroomDeliveryBindsStateToEndpointIdentity(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	first, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test:443/api/audits",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return proofroomJSONResponse(503, `{}`), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.Deliver(context.Background(), store, audit.AuditID, "endpoint-key"); !errors.Is(err, ErrProofroomDeliveryOutcomeUnknown) {
		t.Fatalf("first delivery error = %v", err)
	}
	var calls atomic.Int32
	second, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test:8443/api/audits",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return proofroomJSONResponse(200, `{"receipt_id":"wrong-target","status":"accepted"}`), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := second.Deliver(context.Background(), store, audit.AuditID, "endpoint-key"); !errors.Is(err, ErrProofroomDeliveryConflict) {
		t.Fatalf("endpoint change error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("endpoint change reached remote: %d", calls.Load())
	}
	if err := CoordinateProofroomDeliveryForEndpoint(
		store, audit.AuditID, "endpoint-key", second.endpointIdentity,
		ProofroomCoordinationConfirmedNotDelivered, testAgentPackageTime(),
	); !errors.Is(err, ErrProofroomDeliveryConflict) {
		t.Fatalf("cross-endpoint coordination error = %v", err)
	}
}

func TestProofroomEndpointIdentityNormalizesPortAndBindsPath(t *testing.T) {
	first, _ := url.Parse("https://Proofroom.Example.test/api/audits")
	explicit, _ := url.Parse("https://proofroom.example.test:443/api/audits")
	differentPath, _ := url.Parse("https://proofroom.example.test:443/api/review")
	if proofroomEndpointIdentity(first) != proofroomEndpointIdentity(explicit) {
		t.Fatal("default HTTPS port was not normalized")
	}
	if proofroomEndpointIdentity(first) == proofroomEndpointIdentity(differentPath) {
		t.Fatal("endpoint identity did not bind the path")
	}
}

func TestProofroomDeliveryStateRecoversFromInterruptedPublish(t *testing.T) {
	store, audit := completedEvidenceAuditForProofroomTest(t)
	var injected atomic.Bool
	previous := proofroomStateStorageFault
	proofroomStateStorageFault = func(stage, _ string) error {
		if stage == proofroomStateFaultBackupPublished && injected.CompareAndSwap(false, true) {
			return errors.New("injected state publish failure")
		}
		return nil
	}
	t.Cleanup(func() { proofroomStateStorageFault = previous })
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/api/audits",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Deliver(context.Background(), store, audit.AuditID, "crash-key"); !errors.Is(err, ErrProofroomDeliveryOutcomeUnknown) {
		t.Fatalf("interrupted delivery error = %v", err)
	}
	statePath := store.EvidenceAuditProofroomStatePath(audit.AuditID, evidenceAuditOpaqueIdentity("crash-key"))
	state, err := loadProofroomDeliveryState(statePath)
	if err != nil {
		t.Fatalf("load recovered state: %v", err)
	}
	if state.Status != "in_flight" && state.Status != ProofroomDeliveryOutcomeUnknown {
		t.Fatalf("recovered state = %#v", state)
	}
	if info, err := os.Stat(statePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("recovered state permissions info=%v err=%v", info, err)
	}
}

func TestProofroomDeliveryStateRecoversFromSemanticallyCorruptPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries", "state.json")
	state := proofroomDeliveryState{
		Version: "1", AuditID: "audit-1", IdempotencyIdentity: proofroomSHA256([]byte("key")),
		ProjectionHash:   proofroomSHA256([]byte("projection")),
		EndpointIdentity: "https://proofroom.example.test:443/path/" + proofroomSHA256([]byte("/api")),
		Status:           "in_flight", RequestedAt: testAgentPackageTime().Format(time.RFC3339Nano),
		UpdatedAt: testAgentPackageTime().Format(time.RFC3339Nano),
	}
	if err := writeProofroomDeliveryState(path, state); err != nil {
		t.Fatal(err)
	}
	state.Status = ProofroomDeliveryOutcomeUnknown
	if err := writeProofroomDeliveryState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := loadProofroomDeliveryState(path)
	if err != nil {
		t.Fatalf("load recovered state: %v", err)
	}
	if recovered.Status != "in_flight" {
		t.Fatalf("recovered state = %#v", recovered)
	}
}

func TestProofroomDeliveryRejectsIdempotencyKeyReusedForDifferentPayload(t *testing.T) {
	root := t.TempDir()
	store, firstAudit := completedEvidenceAuditForProofroomTestAtRoot(t, root)
	secondInput := validEvidenceAuditInput()
	secondInput.Subject = "a different bounded audit subject"
	_, secondAudit := completedEvidenceAuditForProofroomInputTest(t, root, secondInput)
	var calls atomic.Int32
	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return proofroomJSONResponse(http.StatusOK, `{"receipt_id":"accepted","status":"accepted"}`), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Deliver(context.Background(), store, firstAudit.AuditID, "global-key"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Deliver(context.Background(), store, secondAudit.AuditID, "global-key"); !errors.Is(err, ErrProofroomDeliveryConflict) {
		t.Fatalf("reused key error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("reused key remote calls=%d", calls.Load())
	}
}

func TestProofroomEndpointValidationBlocksSSRFAndUnsafeRedirects(t *testing.T) {
	for _, endpoint := range []string{
		"http://proofroom.example.test/deliver",
		"https://user:pass@proofroom.example.test/deliver",
		"https://proofroom.example.test/deliver#fragment",
		"https://127.0.0.1/deliver",
		"https://10.0.0.5/deliver",
		"https://[::1]/deliver",
	} {
		if _, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
			Endpoint: endpoint, Token: "remote-secret",
		}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	for _, token := range []string{"", "line1\nline2", strings.Repeat("x", 8193)} {
		if _, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
			Endpoint: "https://proofroom.example.test/deliver", Token: token,
		}); !errors.Is(err, ErrProofroomDeliveryUnconfigured) {
			t.Fatalf("unsafe token %q error = %v", token, err)
		}
	}

	service, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			response := proofroomJSONResponse(http.StatusTemporaryRedirect, "")
			response.Header.Set("Location", "http://127.0.0.1/private")
			return response, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	store, audit := completedEvidenceAuditForProofroomTest(t)
	if _, _, err := service.Deliver(context.Background(), store, audit.AuditID, "redirect-key"); !errors.Is(err, ErrProofroomDeliveryRejected) {
		t.Fatalf("unsafe redirect error = %v", err)
	}

	var calls atomic.Int32
	resolvedPrivate, err := NewProofroomDeliveryService(ProofroomDeliveryConfig{
		Endpoint: "https://proofroom.example.test/deliver",
		Token:    "remote-secret",
		Client: proofroomHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return proofroomJSONResponse(http.StatusOK, `{"receipt_id":"unsafe","status":"accepted"}`), nil
		}),
		Resolver: proofroomResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.10")}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	privateStore, privateAudit := completedEvidenceAuditForProofroomTest(t)
	if _, _, err := resolvedPrivate.Deliver(
		context.Background(), privateStore, privateAudit.AuditID, "resolved-private",
	); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("private DNS result error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("private DNS result reached client: calls=%d", calls.Load())
	}
}

func completedEvidenceAuditForProofroomTest(t *testing.T) (*BookKnowledgeStore, EvidenceAudit) {
	t.Helper()
	return completedEvidenceAuditForProofroomTestAtRoot(t, t.TempDir())
}

func completedEvidenceAuditForProofroomTestAtRoot(t *testing.T, root string) (*BookKnowledgeStore, EvidenceAudit) {
	t.Helper()
	return completedEvidenceAuditForProofroomInputTest(t, root, validEvidenceAuditInput())
}

func completedEvidenceAuditForProofroomInputTest(
	t *testing.T,
	root string,
	input EvidenceAuditInput,
) (*BookKnowledgeStore, EvidenceAudit) {
	t.Helper()
	store := NewBookKnowledgeStore(root)
	now := testAgentPackageTime()
	identity := strings.TrimPrefix(proofroomPrivateTextIdentity("test", input.Subject), "sha256:")[:16]
	queued, _, err := CreateEvidenceAudit(
		store, input, "proofroom-audit-"+identity,
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	traceID := "trace-proofroom-" + identity
	if _, err := StartEvidenceAudit(store, queued.AuditID, traceID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	report := validCompletedEvidenceAudit()
	report.Package = input.Package
	report.EvidencePolicy = input.EvidencePolicy
	report.Model = input.Model
	report.Retrieval = input.Retrieval
	report.Releases = input.Releases
	report.Subject = input.Subject
	report.Scope = input.Scope
	report.SelectedClaims = input.SelectedClaims
	report.AuditID = queued.AuditID
	report.TraceID = traceID
	report.CreatedAt = queued.CreatedAt
	report.UpdatedAt = now.Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	report.StartedAt = now.Add(time.Minute).UTC().Format(time.RFC3339Nano)
	report.CompletedAt = now.Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	report.IdempotencyKey = queued.IdempotencyKey
	report.InputHash = queued.InputHash
	completed, err := completeEvidenceAuditForTest(t, store, report, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return store, *completed
}

type proofroomHTTPClientFunc func(*http.Request) (*http.Response, error)

func (fn proofroomHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type proofroomResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (fn proofroomResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return fn(ctx, host)
}

func proofroomJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestProofroomTestHelpersUsePrivatePaths(t *testing.T) {
	store := NewBookKnowledgeStore(t.TempDir())
	if strings.Contains(store.EvidenceAuditProofroomDir(), "..") ||
		filepath.Dir(store.EvidenceAuditProofroomDir()) != store.EvidenceAuditDir() {
		t.Fatalf("unsafe proofroom dir = %q", store.EvidenceAuditProofroomDir())
	}
}
