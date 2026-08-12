package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (h *kbaseHTTPHandler) handleEvolutionWorkerAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.evolutionEnabled || h.evolutionStore == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "evolution control plane is not configured")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch r.URL.Path {
	case "/api/evolution/workers/lease":
		var payload evolutionWorkerLeaseRequest
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		work, _, err := h.evolutionStore.LeaseNextEvolutionWork(EvolutionWorkLeaseInput{
			WorkerID: payload.WorkerID, Capabilities: payload.Capabilities,
			LeaseDuration: time.Duration(payload.LeaseSeconds) * time.Second,
		})
		if err != nil {
			h.writeEvolutionWorkerError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"work": work})
	case "/api/evolution/workers/renew":
		var payload evolutionWorkerRenewRequest
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		work, err := h.evolutionStore.RenewEvolutionLease(EvolutionWorkLeaseUpdate{
			WorkID: payload.WorkID, WorkerID: payload.WorkerID, LeaseID: payload.LeaseID,
			LeaseDuration: time.Duration(payload.LeaseSeconds) * time.Second,
		})
		if err != nil {
			h.writeEvolutionWorkerError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"work": work})
	case "/api/evolution/workers/generate":
		h.handleEvolutionWorkerGeneration(w, r)
	case "/api/evolution/workers/complete":
		var payload EvolutionWorkCompletion
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		work, _, err := h.evolutionStore.CompleteEvolutionWork(payload)
		if err != nil {
			h.writeEvolutionWorkerError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"work": work})
	case "/api/evolution/workers/fail":
		var payload evolutionWorkerFailRequest
		if !h.decodeSourceAgentJSON(w, r, &payload) {
			return
		}
		work, _, err := h.evolutionStore.FailEvolutionWork(EvolutionWorkFailure{
			WorkID: payload.WorkID, WorkerID: payload.WorkerID, LeaseID: payload.LeaseID, Attempt: payload.Attempt,
			FailureIdempotencyKey: payload.FailureIdempotencyKey, FailureCode: payload.FailureCode,
			FailureMessage: payload.FailureMessage, RetryDelay: time.Duration(payload.RetrySeconds) * time.Second,
		})
		if err != nil {
			h.writeEvolutionWorkerError(w, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"work": work})
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) handleEvolutionWorkerGeneration(w http.ResponseWriter, r *http.Request) {
	var payload evolutionWorkerIdentityRequest
	if !h.decodeSourceAgentJSON(w, r, &payload) {
		return
	}
	work, err := h.evolutionStore.LoadEvolutionWork(payload.WorkID)
	if err != nil {
		h.writeEvolutionWorkerError(w, err)
		return
	}
	if work.Attempt != payload.Attempt || validateActiveEvolutionWorkLease(work, payload.WorkerID, payload.LeaseID, h.evolutionStore.now().UTC()) != nil {
		h.writeEvolutionWorkerError(w, ErrEvolutionLeaseLost)
		return
	}
	version := "knowledge-evolution-worker.v1"
	if work.Capability == EvolutionCapabilityAgent {
		version = "agent-evolution-worker.v1"
	}
	service, err := NewEvolutionGenerationService(EvolutionGenerationConfig{
		ControlStore: h.evolutionStore, KnowledgeStore: h.store, GeneratorVersion: version,
	})
	if err != nil {
		h.writeEvolutionWorkerError(w, err)
		return
	}
	result, err := service.Generate(r.Context(), *work)
	if err != nil {
		h.writeEvolutionWorkerError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, result)
}

func (h *kbaseHTTPHandler) writeEvolutionWorkerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrEvolutionWorkNotFound), errors.Is(err, ErrEvolutionRunNotFound):
		writeHTTPError(w, http.StatusNotFound, "evolution work was not found")
	case errors.Is(err, ErrEvolutionLeaseLost), errors.Is(err, ErrEvolutionLeaseExpired), errors.Is(err, ErrEvolutionTransitionConflict), errors.Is(err, ErrEvolutionIdempotencyConflict):
		writeHTTPError(w, http.StatusConflict, "evolution work conflicts with current state")
	case errors.Is(err, ErrEvolutionCapabilityInvalid):
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution worker capability")
	default:
		var failure *EvolutionGenerationFailure
		if errors.As(err, &failure) {
			writeHTTPError(w, http.StatusUnprocessableEntity, failure.Message)
			return
		}
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution worker request")
	}
}

const (
	evolutionHTTPDefaultLimit = 50
	evolutionHTTPMaxLimit     = 200
)

type evolutionRunHTTPView struct {
	RunID                  string             `json:"run_id"`
	Attempt                int                `json:"attempt"`
	RetryOfRunID           string             `json:"retry_of_run_id"`
	RunType                EvolutionRunType   `json:"run_type"`
	PackageID              string             `json:"package_id"`
	BaselinePackageVersion string             `json:"baseline_package_version"`
	BaselineReleaseCount   int                `json:"baseline_release_count"`
	RiskLevel              string             `json:"risk_level"`
	PriorityScore          float64            `json:"priority_score"`
	Status                 EvolutionRunStatus `json:"status"`
	TriggerSignalCount     int                `json:"trigger_signal_count"`
	HasCandidate           bool               `json:"has_candidate"`
	FailureCode            string             `json:"failure_code"`
	CreatedAt              string             `json:"created_at"`
	UpdatedAt              string             `json:"updated_at"`
}

type evolutionAgentPackageHTTPView struct {
	PackageID      string `json:"package_id"`
	Version        string `json:"version"`
	LifecycleState string `json:"lifecycle_state"`
	Supersedes     string `json:"supersedes,omitempty"`
	PublishedAt    string `json:"published_at"`
	URL            string `json:"url"`
}

type evolutionAgentHTTPView struct {
	PackageID string                          `json:"package_id"`
	Current   *evolutionAgentPackageHTTPView  `json:"current"`
	History   []evolutionAgentPackageHTTPView `json:"history"`
	OpenRuns  []evolutionRunHTTPView          `json:"open_runs"`
}

type evolutionOverviewHTTPView struct {
	OpenRuns         []evolutionRunHTTPView   `json:"open_runs"`
	AgentFleet       []evolutionAgentHTTPView `json:"agent_fleet"`
	TotalOpenRuns    int                      `json:"total_open_runs"`
	AwaitingApproval int                      `json:"awaiting_approval"`
	Blocked          int                      `json:"blocked"`
	Failed           int                      `json:"failed"`
	Completed        int                      `json:"completed"`
}

type evolutionRunHTTPPage struct {
	Runs       []evolutionRunHTTPView `json:"runs"`
	NextCursor string                 `json:"next_cursor"`
}

type evolutionEventHTTPView struct {
	EventID    string             `json:"event_id"`
	RunID      string             `json:"run_id"`
	EventType  string             `json:"event_type"`
	Actor      string             `json:"actor"`
	FromStatus EvolutionRunStatus `json:"from_status"`
	ToStatus   EvolutionRunStatus `json:"to_status"`
	Code       string             `json:"code"`
	CreatedAt  string             `json:"created_at"`
}

type evolutionEventHTTPPage struct {
	Events     []evolutionEventHTTPView `json:"events"`
	NextCursor string                   `json:"next_cursor"`
}

func isEvolutionAPIPath(path string) bool {
	return path == "/api/evolution" || strings.HasPrefix(path, "/api/evolution/")
}

func (h *kbaseHTTPHandler) handleEvolutionReadAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.evolutionEnabled {
		writeHTTPError(w, http.StatusServiceUnavailable, "evolution control plane is disabled")
		return
	}
	if h.evolutionStore == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, "evolution control plane is not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	path := r.URL.EscapedPath()
	switch path {
	case "/api/evolution/overview":
		h.handleEvolutionOverview(w, r)
	case "/api/evolution/runs":
		h.handleEvolutionRuns(w, r)
	default:
		runID, events, ok, invalid := parseEvolutionRunHTTPPath(path)
		if invalid {
			writeHTTPError(w, http.StatusBadRequest, "invalid evolution run_id")
			return
		}
		if !ok {
			writeHTTPError(w, http.StatusNotFound, "not found")
			return
		}
		if events {
			h.handleEvolutionEvents(w, r, runID)
			return
		}
		h.handleEvolutionRun(w, r, runID)
	}
}

func (h *kbaseHTTPHandler) handleEvolutionOverview(w http.ResponseWriter, r *http.Request) {
	query, parsed := parseEvolutionHTTPQuery(r)
	if !parsed || !validateEvolutionHTTPQuery(query, nil) {
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution query")
		return
	}
	records, err := h.listAllAgentPackageRecords(r.Context())
	if err != nil {
		h.writeEvolutionReadError(w, err)
		return
	}
	overview, err := h.evolutionStore.EvolutionOverviewContext(r.Context(), records)
	if err != nil {
		h.writeEvolutionReadError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, newEvolutionOverviewHTTPView(overview))
}

func (h *kbaseHTTPHandler) handleEvolutionRuns(w http.ResponseWriter, r *http.Request) {
	query, parsed := parseEvolutionHTTPQuery(r)
	if !parsed || !validateEvolutionHTTPQuery(query, map[string]bool{
		"status": true, "risk": true, "type": true, "package": true, "cursor": true, "limit": true,
	}) {
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution query")
		return
	}
	limit, ok := parseEvolutionHTTPLimit(query.Get("limit"))
	if !ok {
		writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	if cursor := query.Get("cursor"); cursor != "" {
		if _, err := decodeEvolutionRunCursor(cursor); err != nil {
			writeHTTPError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
	}
	filter, err := parseEvolutionRunHTTPFilter(query.Get("status"), query.Get("risk"), query.Get("type"), query.Get("package"))
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution filter")
		return
	}
	page, err := h.evolutionStore.ListEvolutionRunsFiltered(r.Context(), filter, query.Get("cursor"), limit)
	if err != nil {
		h.writeEvolutionReadError(w, err)
		return
	}
	views := make([]evolutionRunHTTPView, 0, len(page.Runs))
	for index := range page.Runs {
		views = append(views, newEvolutionRunHTTPView(page.Runs[index]))
	}
	writeHTTPJSON(w, http.StatusOK, evolutionRunHTTPPage{Runs: views, NextCursor: page.NextCursor})
}

func (h *kbaseHTTPHandler) handleEvolutionRun(w http.ResponseWriter, r *http.Request, runID string) {
	query, parsed := parseEvolutionHTTPQuery(r)
	if !parsed || !validateEvolutionHTTPQuery(query, nil) {
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution query")
		return
	}
	run, err := h.evolutionStore.LoadRunContext(r.Context(), runID)
	if err != nil {
		h.writeEvolutionReadError(w, err)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"run": newEvolutionRunHTTPView(*run)})
}

func (h *kbaseHTTPHandler) handleEvolutionEvents(w http.ResponseWriter, r *http.Request, runID string) {
	query, parsed := parseEvolutionHTTPQuery(r)
	if !parsed || !validateEvolutionHTTPQuery(query, map[string]bool{"cursor": true, "limit": true}) {
		writeHTTPError(w, http.StatusBadRequest, "invalid evolution query")
		return
	}
	limit, ok := parseEvolutionHTTPLimit(query.Get("limit"))
	if !ok {
		writeHTTPError(w, http.StatusBadRequest, "limit must be between 1 and 200")
		return
	}
	cursor := query.Get("cursor")
	if cursor != "" && validateEvolutionIdentity("cursor", cursor) != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	events, err := h.evolutionStore.ListEventsContext(r.Context(), runID, cursor, limit+1)
	if err != nil {
		h.writeEvolutionReadError(w, err)
		return
	}
	nextCursor := ""
	if len(events) > limit {
		events = events[:limit]
		nextCursor = events[len(events)-1].EventID
	}
	views := make([]evolutionEventHTTPView, 0, len(events))
	for _, event := range events {
		views = append(views, evolutionEventHTTPView{
			EventID: event.EventID, RunID: event.RunID, EventType: event.EventType, Actor: event.Actor,
			FromStatus: event.FromStatus, ToStatus: event.ToStatus, Code: event.Code, CreatedAt: event.CreatedAt,
		})
	}
	writeHTTPJSON(w, http.StatusOK, evolutionEventHTTPPage{Events: views, NextCursor: nextCursor})
}

func parseEvolutionRunHTTPPath(escapedPath string) (runID string, events, ok, invalid bool) {
	const prefix = "/api/evolution/runs/"
	if !strings.HasPrefix(escapedPath, prefix) {
		return "", false, false, false
	}
	remainder := strings.TrimPrefix(escapedPath, prefix)
	if strings.HasSuffix(remainder, "/events") {
		events = true
		remainder = strings.TrimSuffix(remainder, "/events")
	}
	if remainder == "" || strings.Contains(remainder, "/") {
		return "", false, false, false
	}
	decoded, err := url.PathUnescape(remainder)
	if err != nil || validateEvolutionHTTPIdentity("run_id", decoded) != nil {
		return "", false, false, true
	}
	return decoded, events, true, false
}

func parseEvolutionHTTPLimit(raw string) (int, bool) {
	if raw == "" {
		return evolutionHTTPDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > evolutionHTTPMaxLimit {
		return 0, false
	}
	return limit, true
}

func validateEvolutionHTTPQuery(query url.Values, allowed map[string]bool) bool {
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || strings.TrimSpace(values[0]) != values[0] || values[0] == "" {
			return false
		}
	}
	return true
}

func parseEvolutionHTTPQuery(r *http.Request) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	return query, err == nil
}

func parseEvolutionRunHTTPFilter(statuses, risks, runTypes, packageID string) (EvolutionRunFilter, error) {
	filter := EvolutionRunFilter{PackageID: packageID}
	statusValues, err := parseEvolutionHTTPList(statuses)
	if err != nil {
		return EvolutionRunFilter{}, err
	}
	for _, status := range statusValues {
		value := EvolutionRunStatus(status)
		if !isKnownEvolutionRunStatus(value) {
			return EvolutionRunFilter{}, fmt.Errorf("unknown status")
		}
		filter.Statuses = append(filter.Statuses, value)
	}
	riskValues, err := parseEvolutionHTTPList(risks)
	if err != nil {
		return EvolutionRunFilter{}, err
	}
	for _, risk := range riskValues {
		if !isKnownEvolutionHTTPRisk(risk) {
			return EvolutionRunFilter{}, fmt.Errorf("unknown risk")
		}
		filter.RiskLevels = append(filter.RiskLevels, risk)
	}
	typeValues, err := parseEvolutionHTTPList(runTypes)
	if err != nil {
		return EvolutionRunFilter{}, err
	}
	for _, runType := range typeValues {
		value := EvolutionRunType(runType)
		if !isKnownEvolutionRunType(value) {
			return EvolutionRunFilter{}, fmt.Errorf("unknown type")
		}
		filter.RunTypes = append(filter.RunTypes, value)
	}
	if packageID != "" {
		if err := validateEvolutionHTTPIdentity("package", packageID); err != nil {
			return EvolutionRunFilter{}, err
		}
	}
	return filter, nil
}

func isKnownEvolutionHTTPRisk(risk string) bool {
	switch risk {
	case "p0", "p1", "p2", "p3",
		EvolutionSignalSeverityCritical, EvolutionSignalSeverityHigh,
		EvolutionSignalSeverityMedium, EvolutionSignalSeverityLow:
		return true
	default:
		return false
	}
}

func validateEvolutionHTTPIdentity(field, value string) error {
	if err := validateEvolutionIdentity(field, value); err != nil {
		return err
	}
	if strings.Contains(value, "/") || strings.Contains(value, "..") {
		return fmt.Errorf("%s is not a valid URL identity", field)
	}
	return nil
}

func parseEvolutionHTTPList(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > EvolutionCollectionMaxItems {
		return nil, fmt.Errorf("too many filter values")
	}
	seen := make(map[string]struct{}, len(parts))
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return nil, fmt.Errorf("invalid filter value")
		}
		if _, exists := seen[part]; exists {
			return nil, fmt.Errorf("duplicate filter value")
		}
		seen[part] = struct{}{}
		values = append(values, part)
	}
	return values, nil
}

func (h *kbaseHTTPHandler) listAllAgentPackageRecords(ctx context.Context) ([]AgentPackageRecord, error) {
	records := make([]AgentPackageRecord, 0)
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := h.store.ListAgentPackages(cursor, evolutionHTTPMaxLimit)
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
		if len(page) < evolutionHTTPMaxLimit {
			return records, nil
		}
		last := page[len(page)-1]
		next := agentPackageReference(last.PackageID, last.Version)
		if next == cursor {
			return nil, fmt.Errorf("agent package pagination did not advance")
		}
		cursor = next
	}
}

func newEvolutionOverviewHTTPView(source *EvolutionOverview) evolutionOverviewHTTPView {
	view := evolutionOverviewHTTPView{
		OpenRuns:      make([]evolutionRunHTTPView, 0, len(source.OpenRuns)),
		AgentFleet:    make([]evolutionAgentHTTPView, 0, len(source.AgentFleet)),
		TotalOpenRuns: source.TotalOpenRuns, AwaitingApproval: source.AwaitingApproval, Blocked: source.Blocked,
		Failed: source.Failed, Completed: source.Completed,
	}
	for _, run := range source.OpenRuns {
		view.OpenRuns = append(view.OpenRuns, newEvolutionRunHTTPView(run))
	}
	for _, agent := range source.AgentFleet {
		item := evolutionAgentHTTPView{
			PackageID: agent.PackageID,
			History:   make([]evolutionAgentPackageHTTPView, 0, len(agent.History)),
			OpenRuns:  make([]evolutionRunHTTPView, 0, len(agent.OpenRuns)),
		}
		if agent.Current != nil {
			current := newEvolutionAgentPackageHTTPView(*agent.Current)
			item.Current = &current
		}
		for _, record := range agent.History {
			item.History = append(item.History, newEvolutionAgentPackageHTTPView(record))
		}
		for _, run := range agent.OpenRuns {
			item.OpenRuns = append(item.OpenRuns, newEvolutionRunHTTPView(run))
		}
		view.AgentFleet = append(view.AgentFleet, item)
	}
	return view
}

func newEvolutionRunHTTPView(run EvolutionRun) evolutionRunHTTPView {
	return evolutionRunHTTPView{
		RunID: run.RunID, Attempt: run.Attempt, RetryOfRunID: run.RetryOfRunID,
		RunType: run.RunType, PackageID: run.PackageID, BaselinePackageVersion: run.BaselinePackageVersion,
		BaselineReleaseCount: len(run.BaselineReleaseIDs), RiskLevel: run.RiskLevel,
		PriorityScore: run.PriorityScore, Status: run.Status, TriggerSignalCount: len(run.TriggerSignalIDs),
		HasCandidate: run.CurrentCandidateID != "", FailureCode: run.FailureCode,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}

func newEvolutionAgentPackageHTTPView(record AgentPackageRecord) evolutionAgentPackageHTTPView {
	return evolutionAgentPackageHTTPView{
		PackageID: record.PackageID, Version: record.Version, LifecycleState: record.LifecycleState,
		Supersedes: record.Supersedes, PublishedAt: record.PublishedAt, URL: record.URL,
	}
}

func (h *kbaseHTTPHandler) writeEvolutionReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeHTTPError(w, http.StatusGatewayTimeout, "evolution request timed out")
	case errors.Is(err, ErrEvolutionRunNotFound):
		writeHTTPError(w, http.StatusNotFound, "evolution run not found")
	case errors.Is(err, ErrEvolutionRunCursorNotFound), errors.Is(err, ErrEvolutionEventCursorNotFound):
		writeHTTPError(w, http.StatusBadRequest, "invalid cursor")
	default:
		writeHTTPError(w, http.StatusInternalServerError, "evolution data unavailable")
	}
}
