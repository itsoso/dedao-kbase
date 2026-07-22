package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	ClinicalTrialsGovBaseURL                      = "https://clinicaltrials.gov"
	ClinicalTrialsGovStudySourceType              = "clinicaltrials_gov_study"
	ClinicalTrialsGovMaxStudyBytes                = 1 << 20
	ClinicalTrialsGovMaxVersionBytes              = 64 << 10
	ClinicalTrialsGovMaxRetryAfter                = time.Hour
	ClinicalTrialsGovMaxRequestTimeout            = 2 * time.Minute
	ClinicalTrialsGovMaxReportedOutcomes          = 256
	ClinicalTrialsGovMaxResultGroups              = 128
	ClinicalTrialsGovMaxResultClasses             = 256
	ClinicalTrialsGovMaxResultCategories          = 512
	ClinicalTrialsGovMaxGroupMeasurements         = 2048
	ClinicalTrialsGovMaxResultDenominators        = 128
	ClinicalTrialsGovMaxDenominatorCounts         = 2048
	ClinicalTrialsGovMaxResultAnalyses            = 128
	ClinicalTrialsGovMaxAnalysisGroups            = 128
	ClinicalTrialsGovMaxProtocolConditions        = 512
	ClinicalTrialsGovMaxProtocolArms              = 256
	ClinicalTrialsGovMaxProtocolInterventions     = 512
	ClinicalTrialsGovMaxProtocolOutcomes          = 512
	ClinicalTrialsGovMaxProtocolReferences        = 1024
	ClinicalTrialsGovMaxProtocolInterventionNames = 512
	ClinicalTrialsGovMaxProtocolArmGroupLabels    = 256
	ClinicalTrialsGovMaxProtocolOtherNames        = 256
	ClinicalTrialsGovMaxFlowGroups                = 128
	ClinicalTrialsGovMaxFlowPeriods               = 64
	ClinicalTrialsGovMaxFlowMilestones            = 512
	ClinicalTrialsGovMaxFlowAchievements          = 4096
	ClinicalTrialsGovMaxFlowDropWithdraws         = 512
	ClinicalTrialsGovMaxFlowReasons               = 4096

	clinicalTrialsGovUserAgent             = "dedao-kbase/clinical-trial-audit"
	clinicalTrialsGovDefaultRequestTimeout = 20 * time.Second
)

var errClinicalTrialsGovRedirectBlocked = errors.New("ClinicalTrials.gov redirect blocked")

type ClinicalTrialsGovErrorKind string

const (
	ClinicalTrialsGovErrorInvalidInput       ClinicalTrialsGovErrorKind = "invalid_input"
	ClinicalTrialsGovErrorNotFound           ClinicalTrialsGovErrorKind = "not_found"
	ClinicalTrialsGovErrorRateLimited        ClinicalTrialsGovErrorKind = "rate_limited"
	ClinicalTrialsGovErrorTimeout            ClinicalTrialsGovErrorKind = "timeout"
	ClinicalTrialsGovErrorCanceled           ClinicalTrialsGovErrorKind = "canceled"
	ClinicalTrialsGovErrorResponseTooLarge   ClinicalTrialsGovErrorKind = "response_too_large"
	ClinicalTrialsGovErrorMalformedJSON      ClinicalTrialsGovErrorKind = "malformed_json"
	ClinicalTrialsGovErrorIdentifierMismatch ClinicalTrialsGovErrorKind = "identifier_mismatch"
	ClinicalTrialsGovErrorSchemaInvalid      ClinicalTrialsGovErrorKind = "schema_invalid"
	ClinicalTrialsGovErrorUpstream           ClinicalTrialsGovErrorKind = "upstream"
)

type ClinicalTrialsGovError struct {
	Kind       ClinicalTrialsGovErrorKind
	StatusCode int
	RetryAfter time.Duration
	cause      error
	retryable  bool
}

func (e *ClinicalTrialsGovError) Error() string {
	switch e.Kind {
	case ClinicalTrialsGovErrorInvalidInput:
		return "ClinicalTrials.gov input is not a valid NCT identifier"
	case ClinicalTrialsGovErrorNotFound:
		return "ClinicalTrials.gov study was not found"
	case ClinicalTrialsGovErrorRateLimited:
		return "ClinicalTrials.gov rate limit was reached"
	case ClinicalTrialsGovErrorTimeout:
		return "ClinicalTrials.gov request timed out"
	case ClinicalTrialsGovErrorCanceled:
		return "ClinicalTrials.gov request was canceled"
	case ClinicalTrialsGovErrorResponseTooLarge:
		return "ClinicalTrials.gov response exceeded the size limit"
	case ClinicalTrialsGovErrorMalformedJSON:
		return "ClinicalTrials.gov returned malformed JSON"
	case ClinicalTrialsGovErrorIdentifierMismatch:
		return "ClinicalTrials.gov returned a different study identifier"
	case ClinicalTrialsGovErrorSchemaInvalid:
		return "ClinicalTrials.gov response did not match the required schema"
	default:
		return "ClinicalTrials.gov upstream request failed"
	}
}

func (e *ClinicalTrialsGovError) Unwrap() error { return e.cause }

func (e *ClinicalTrialsGovError) Retryable() bool {
	return e.retryable
}

func MapClinicalTrialsGovErrorToAuditFailure(err error) (ClinicalTrialAuditFailure, error) {
	var source *ClinicalTrialsGovError
	if !errors.As(err, &source) {
		return ClinicalTrialAuditFailure{}, fmt.Errorf("clinical trial source error is not classified")
	}
	failure := ClinicalTrialAuditFailure{}
	switch source.Kind {
	case ClinicalTrialsGovErrorInvalidInput:
		failure.Code = ClinicalTrialAuditErrorIdentifierInvalid
	case ClinicalTrialsGovErrorNotFound:
		failure.Code = ClinicalTrialAuditErrorSourceNotFound
	case ClinicalTrialsGovErrorRateLimited:
		failure.Code, failure.Retryable = ClinicalTrialAuditErrorSourceRateLimited, true
	case ClinicalTrialsGovErrorTimeout:
		failure.Code, failure.Retryable = ClinicalTrialAuditErrorSourceTimeout, true
	case ClinicalTrialsGovErrorCanceled:
		failure.Code = ClinicalTrialAuditErrorSourceCanceled
	case ClinicalTrialsGovErrorResponseTooLarge:
		failure.Code = ClinicalTrialAuditErrorSourceResponseTooLarge
	case ClinicalTrialsGovErrorMalformedJSON:
		failure.Code = ClinicalTrialAuditErrorSourceMalformedJSON
	case ClinicalTrialsGovErrorIdentifierMismatch:
		failure.Code = ClinicalTrialAuditErrorSourceIdentifierMismatch
	case ClinicalTrialsGovErrorSchemaInvalid:
		failure.Code = ClinicalTrialAuditErrorSourceSchemaInvalid
	case ClinicalTrialsGovErrorUpstream:
		if source.Retryable() {
			failure.Code, failure.Retryable = ClinicalTrialAuditErrorSourceUpstreamTransient, true
		} else {
			failure.Code = ClinicalTrialAuditErrorSourceUpstreamPermanent
		}
	default:
		return ClinicalTrialAuditFailure{}, fmt.Errorf("unsupported ClinicalTrials.gov error kind %q", source.Kind)
	}
	if err := validateClinicalTrialAuditFailure(failure.Code, failure.Retryable); err != nil {
		return ClinicalTrialAuditFailure{}, err
	}
	return failure, nil
}

type ClinicalTrialsGovDate struct {
	Value     string `json:"value,omitempty"`
	Type      string `json:"type,omitempty"`
	Precision string `json:"precision,omitempty"`
}

type ClinicalTrialsGovEnrollment struct {
	Count int64  `json:"count"`
	Type  string `json:"type,omitempty"`
}

type ClinicalTrialsGovDesign struct {
	Allocation        string   `json:"allocation,omitempty"`
	InterventionModel string   `json:"intervention_model,omitempty"`
	Masking           string   `json:"masking,omitempty"`
	WhoMasked         []string `json:"who_masked,omitempty"`
	PrimaryPurpose    string   `json:"primary_purpose,omitempty"`
}

type ClinicalTrialsGovArm struct {
	Label             string   `json:"label"`
	Type              string   `json:"type,omitempty"`
	Description       string   `json:"description,omitempty"`
	InterventionNames []string `json:"intervention_names,omitempty"`
}

type ClinicalTrialsGovIntervention struct {
	Type           string   `json:"type,omitempty"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	ArmGroupLabels []string `json:"arm_group_labels,omitempty"`
	OtherNames     []string `json:"other_names,omitempty"`
}

type ClinicalTrialsGovOutcome struct {
	Measure     string `json:"measure"`
	Description string `json:"description,omitempty"`
	TimeFrame   string `json:"time_frame,omitempty"`
}

type ClinicalTrialsGovPublicationReference struct {
	PMID     string `json:"pmid,omitempty"`
	Type     string `json:"type,omitempty"`
	Citation string `json:"citation"`
}

type ClinicalTrialsGovResultGroup struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type ClinicalTrialsGovGroupMeasurement struct {
	GroupID    string `json:"group_id"`
	Value      string `json:"value,omitempty"`
	Spread     string `json:"spread,omitempty"`
	LowerLimit string `json:"lower_limit,omitempty"`
	UpperLimit string `json:"upper_limit,omitempty"`
	Comment    string `json:"comment,omitempty"`
}

type ClinicalTrialsGovResultCategory struct {
	Title        string                              `json:"title,omitempty"`
	Measurements []ClinicalTrialsGovGroupMeasurement `json:"measurements,omitempty"`
}

type ClinicalTrialsGovResultClass struct {
	Title      string                            `json:"title,omitempty"`
	Categories []ClinicalTrialsGovResultCategory `json:"categories,omitempty"`
}

type ClinicalTrialsGovReportedOutcome struct {
	Type                  string                            `json:"type"`
	Title                 string                            `json:"title"`
	Description           string                            `json:"description,omitempty"`
	TimeFrame             string                            `json:"time_frame,omitempty"`
	UnitOfMeasure         string                            `json:"unit_of_measure,omitempty"`
	Parameter             string                            `json:"parameter,omitempty"`
	Dispersion            string                            `json:"dispersion,omitempty"`
	ReportingStatus       string                            `json:"reporting_status,omitempty"`
	PopulationDescription string                            `json:"population_description,omitempty"`
	Groups                []ClinicalTrialsGovResultGroup    `json:"groups,omitempty"`
	Classes               []ClinicalTrialsGovResultClass    `json:"classes,omitempty"`
	Denominators          []ClinicalTrialsGovDenominator    `json:"denominators,omitempty"`
	Analyses              []ClinicalTrialsGovResultAnalysis `json:"analyses,omitempty"`
}

type ClinicalTrialsGovDenominatorCount struct {
	GroupID string `json:"group_id"`
	Value   string `json:"value"`
}

type ClinicalTrialsGovDenominator struct {
	Units  string                              `json:"units"`
	Counts []ClinicalTrialsGovDenominatorCount `json:"counts,omitempty"`
}

type ClinicalTrialsGovResultAnalysis struct {
	GroupIDs           []string `json:"group_ids"`
	NonInferiorityType string   `json:"non_inferiority_type,omitempty"`
	PValue             string   `json:"p_value,omitempty"`
	StatisticalMethod  string   `json:"statistical_method,omitempty"`
	EffectParameter    string   `json:"effect_parameter,omitempty"`
	EffectEstimate     string   `json:"effect_estimate,omitempty"`
	DispersionType     string   `json:"dispersion_type,omitempty"`
	DispersionValue    string   `json:"dispersion_value,omitempty"`
	ConfidenceLevel    string   `json:"confidence_level,omitempty"`
	ConfidenceSides    string   `json:"confidence_sides,omitempty"`
	ConfidenceLower    string   `json:"confidence_lower,omitempty"`
	ConfidenceUpper    string   `json:"confidence_upper,omitempty"`
}

type ClinicalTrialsGovEvidenceCoverage struct {
	IncludedModules []string `json:"included_modules"`
	ExcludedModules []string `json:"excluded_modules"`
	Limitations     []string `json:"limitations"`
}

type ClinicalTrialsGovFlowCount struct {
	GroupID  string `json:"group_id"`
	Subjects string `json:"subjects"`
}

type ClinicalTrialsGovFlowMilestone struct {
	Type         string                       `json:"type"`
	Achievements []ClinicalTrialsGovFlowCount `json:"achievements,omitempty"`
}

type ClinicalTrialsGovFlowDropWithdraw struct {
	Type    string                       `json:"type"`
	Reasons []ClinicalTrialsGovFlowCount `json:"reasons,omitempty"`
}

type ClinicalTrialsGovFlowPeriod struct {
	Title         string                              `json:"title"`
	Milestones    []ClinicalTrialsGovFlowMilestone    `json:"milestones,omitempty"`
	DropWithdraws []ClinicalTrialsGovFlowDropWithdraw `json:"drop_withdraws,omitempty"`
}

type ClinicalTrialsGovParticipantFlow struct {
	RecruitmentDetails   string                         `json:"recruitment_details,omitempty"`
	PreAssignmentDetails string                         `json:"pre_assignment_details,omitempty"`
	TypeUnitsAnalyzed    string                         `json:"type_units_analyzed,omitempty"`
	Groups               []ClinicalTrialsGovResultGroup `json:"groups,omitempty"`
	Periods              []ClinicalTrialsGovFlowPeriod  `json:"periods,omitempty"`
}

type ClinicalTrialsGovStudy struct {
	SourceAPIVersion      string                                  `json:"source_api_version"`
	NCTID                 string                                  `json:"nct_id"`
	BriefTitle            string                                  `json:"brief_title"`
	OfficialTitle         string                                  `json:"official_title,omitempty"`
	OverallStatus         string                                  `json:"overall_status"`
	StudyType             string                                  `json:"study_type"`
	Phases                []string                                `json:"phases,omitempty"`
	Enrollment            ClinicalTrialsGovEnrollment             `json:"enrollment"`
	Design                ClinicalTrialsGovDesign                 `json:"design"`
	Conditions            []string                                `json:"conditions,omitempty"`
	Arms                  []ClinicalTrialsGovArm                  `json:"arms,omitempty"`
	Interventions         []ClinicalTrialsGovIntervention         `json:"interventions,omitempty"`
	PrimaryOutcomes       []ClinicalTrialsGovOutcome              `json:"primary_outcomes,omitempty"`
	SecondaryOutcomes     []ClinicalTrialsGovOutcome              `json:"secondary_outcomes,omitempty"`
	StartDate             ClinicalTrialsGovDate                   `json:"start_date,omitempty"`
	PrimaryCompletionDate ClinicalTrialsGovDate                   `json:"primary_completion_date,omitempty"`
	CompletionDate        ClinicalTrialsGovDate                   `json:"completion_date,omitempty"`
	ResultsFirstPosted    ClinicalTrialsGovDate                   `json:"results_first_posted,omitempty"`
	LastUpdatePosted      ClinicalTrialsGovDate                   `json:"last_update_posted"`
	HasResults            bool                                    `json:"has_results"`
	ReportedOutcomes      []ClinicalTrialsGovReportedOutcome      `json:"reported_outcomes,omitempty"`
	ParticipantFlow       ClinicalTrialsGovParticipantFlow        `json:"participant_flow,omitempty"`
	Coverage              ClinicalTrialsGovEvidenceCoverage       `json:"coverage"`
	PublicationReferences []ClinicalTrialsGovPublicationReference `json:"publication_references,omitempty"`
}

type ClinicalTrialsGovStudyResult struct {
	Study         ClinicalTrialsGovStudy            `json:"study"`
	Snapshot      ClinicalTrialSourceSnapshot       `json:"snapshot"`
	Evidence      ClinicalTrialAuditEvidencePayload `json:"evidence"`
	APIVersion    string                            `json:"api_version"`
	DataTimestamp string                            `json:"data_timestamp"`
}

func NewClinicalTrialsGovEvidencePayload(study ClinicalTrialsGovStudy) (ClinicalTrialAuditEvidencePayload, error) {
	encoded, err := json.Marshal(study)
	if err != nil {
		return ClinicalTrialAuditEvidencePayload{}, err
	}
	evidence := ClinicalTrialAuditEvidencePayload{
		SchemaVersion: ClinicalTrialAuditEvidenceSchemaVersion,
		SourceType:    ClinicalTrialsGovStudySourceType,
		ContentHash:   hashClinicalTrialValue(string(encoded)),
		Data:          json.RawMessage(encoded),
	}
	if err := validateClinicalTrialAuditEvidencePayload(evidence); err != nil {
		return ClinicalTrialAuditEvidencePayload{}, err
	}
	return evidence, nil
}

func DecodeClinicalTrialsGovEvidencePayload(evidence ClinicalTrialAuditEvidencePayload) (ClinicalTrialsGovStudy, error) {
	if err := validateClinicalTrialAuditEvidencePayload(evidence); err != nil {
		return ClinicalTrialsGovStudy{}, err
	}
	if evidence.SourceType != ClinicalTrialsGovStudySourceType {
		return ClinicalTrialsGovStudy{}, fmt.Errorf("evidence is not a ClinicalTrials.gov study")
	}
	var study ClinicalTrialsGovStudy
	decoder := json.NewDecoder(bytes.NewReader(evidence.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&study); err != nil {
		return ClinicalTrialsGovStudy{}, fmt.Errorf("decode normalized ClinicalTrials.gov evidence: %w", err)
	}
	if strings.TrimSpace(study.NCTID) == "" || strings.TrimSpace(study.SourceAPIVersion) == "" {
		return ClinicalTrialsGovStudy{}, fmt.Errorf("normalized ClinicalTrials.gov evidence is missing identity")
	}
	return study, nil
}

type clinicalTrialsGovOption func(*ClinicalTrialsGovClient)

func withClinicalTrialsGovClock(clock func() time.Time) clinicalTrialsGovOption {
	return func(client *ClinicalTrialsGovClient) {
		if clock != nil {
			client.now = clock
		}
	}
}

func withClinicalTrialsGovRequestTimeout(timeout time.Duration) clinicalTrialsGovOption {
	return func(client *ClinicalTrialsGovClient) {
		client.requestTimeout = timeout
	}
}

type ClinicalTrialsGovClient struct {
	httpClient     *http.Client
	baseURL        *url.URL
	now            func() time.Time
	requestTimeout time.Duration
}

func NewClinicalTrialsGovClient() *ClinicalTrialsGovClient {
	client, err := newClinicalTrialsGovClient(
		&http.Client{Timeout: clinicalTrialsGovDefaultRequestTimeout},
		ClinicalTrialsGovBaseURL,
		withClinicalTrialsGovRequestTimeout(clinicalTrialsGovDefaultRequestTimeout),
	)
	if err != nil {
		panic("invalid fixed ClinicalTrials.gov client configuration")
	}
	return client
}

func newClinicalTrialsGovClient(httpClient *http.Client, baseURL string, options ...clinicalTrialsGovOption) (*ClinicalTrialsGovClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid ClinicalTrials.gov base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	cloned := *httpClient
	originalRedirect := cloned.CheckRedirect
	allowedHost := parsed.Host
	allowedScheme := parsed.Scheme
	cloned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Host != allowedHost || request.URL.Scheme != allowedScheme {
			return errClinicalTrialsGovRedirectBlocked
		}
		if originalRedirect != nil {
			return originalRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("ClinicalTrials.gov redirect limit exceeded")
		}
		return nil
	}
	client := &ClinicalTrialsGovClient{
		httpClient:     &cloned,
		baseURL:        parsed,
		now:            time.Now,
		requestTimeout: clinicalTrialsGovDefaultRequestTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	if client.requestTimeout <= 0 || client.requestTimeout > ClinicalTrialsGovMaxRequestTimeout {
		return nil, fmt.Errorf("invalid ClinicalTrials.gov request timeout")
	}
	return client, nil
}

// GetStudy reads the current ClinicalTrials.gov record. The API exposes no
// documented history endpoint; immutable snapshots use dataTimestamp and
// lastUpdatePosted so later versions can be compared without speculation.
func (c *ClinicalTrialsGovClient) GetStudy(ctx context.Context, nctID string) (ClinicalTrialsGovStudyResult, error) {
	request, err := FinalizeClinicalTrialAuditRequest(ClinicalTrialAuditRequest{InputType: ClinicalTrialInputNCTID, Input: nctID})
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorInvalidInput, cause: err}
	}
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	version, err := c.getVersion(ctx)
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, err
	}
	body, err := c.get(ctx, "/api/v2/studies/"+url.PathEscape(request.NormalizedInput), ClinicalTrialsGovMaxStudyBytes)
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, err
	}
	study, err := normalizeClinicalTrialsGovStudy(body, version.APIVersion)
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, err
	}
	if study.NCTID != request.NormalizedInput {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorIdentifierMismatch}
	}
	evidence, err := NewClinicalTrialsGovEvidencePayload(study)
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	snapshot, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType:        ClinicalTrialsGovStudySourceType,
		CanonicalID:       study.NCTID,
		RetrievedAt:       c.now().UTC().Format(time.RFC3339Nano),
		UpstreamUpdatedAt: study.LastUpdatePosted.Value,
		ContentHash:       evidence.ContentHash,
		LicenseScope:      "public_metadata",
	})
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	return ClinicalTrialsGovStudyResult{
		Study:         study,
		Snapshot:      snapshot,
		Evidence:      evidence,
		APIVersion:    version.APIVersion,
		DataTimestamp: version.DataTimestamp,
	}, nil
}

type clinicalTrialsGovVersion struct {
	APIVersion    string `json:"apiVersion"`
	DataTimestamp string `json:"dataTimestamp"`
}

func (c *ClinicalTrialsGovClient) getVersion(ctx context.Context) (clinicalTrialsGovVersion, error) {
	body, err := c.get(ctx, "/api/v2/version", ClinicalTrialsGovMaxVersionBytes)
	if err != nil {
		return clinicalTrialsGovVersion{}, err
	}
	var version clinicalTrialsGovVersion
	if err := json.Unmarshal(body, &version); err != nil {
		return clinicalTrialsGovVersion{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
	}
	version.APIVersion = strings.TrimSpace(version.APIVersion)
	if version.APIVersion == "" {
		return clinicalTrialsGovVersion{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid}
	}
	dataTimestamp, err := canonicalClinicalTrialsGovTimestamp(version.DataTimestamp)
	if err != nil {
		return clinicalTrialsGovVersion{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	version.DataTimestamp = dataTimestamp
	return version, nil
}

func (c *ClinicalTrialsGovClient) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorUpstream, cause: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", clinicalTrialsGovUserAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, classifyClinicalTrialsGovTransportError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, classifyClinicalTrialsGovStatus(response)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, classifyClinicalTrialsGovTransportError(ctx, err)
	}
	if int64(len(body)) > limit {
		return nil, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorResponseTooLarge, StatusCode: response.StatusCode}
	}
	return body, nil
}

func classifyClinicalTrialsGovTransportError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorCanceled, cause: err}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorTimeout, cause: err, retryable: true}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorTimeout, cause: err, retryable: true}
	}
	if errors.Is(err, errClinicalTrialsGovRedirectBlocked) {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorUpstream, cause: err}
	}
	if errors.As(err, &networkError) && networkError.Temporary() {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorUpstream, cause: err, retryable: true}
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorUpstream, cause: err, retryable: true}
	}
	return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorUpstream, cause: err}
}

func classifyClinicalTrialsGovStatus(response *http.Response) error {
	classified := &ClinicalTrialsGovError{StatusCode: response.StatusCode}
	switch response.StatusCode {
	case http.StatusNotFound:
		classified.Kind = ClinicalTrialsGovErrorNotFound
	case http.StatusTooManyRequests:
		classified.Kind = ClinicalTrialsGovErrorRateLimited
		classified.RetryAfter = parseClinicalTrialsGovRetryAfter(response.Header.Get("Retry-After"), time.Now())
		classified.retryable = true
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		classified.Kind = ClinicalTrialsGovErrorUpstream
		classified.retryable = true
	default:
		classified.Kind = ClinicalTrialsGovErrorUpstream
	}
	return classified
}

func parseClinicalTrialsGovRetryAfter(raw string, now time.Time) time.Duration {
	var retryAfter time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && seconds > 0 {
		maxSeconds := int64(ClinicalTrialsGovMaxRetryAfter / time.Second)
		if seconds >= maxSeconds {
			return ClinicalTrialsGovMaxRetryAfter
		}
		retryAfter = time.Duration(seconds) * time.Second
	} else if target, err := http.ParseTime(raw); err == nil && target.After(now) {
		retryAfter = target.Sub(now)
	}
	if retryAfter > ClinicalTrialsGovMaxRetryAfter {
		return ClinicalTrialsGovMaxRetryAfter
	}
	return retryAfter
}

type clinicalTrialsGovRawStudy struct {
	ProtocolSection json.RawMessage `json:"protocolSection"`
	ResultsSection  json.RawMessage `json:"resultsSection"`
	HasResults      bool            `json:"hasResults"`
}

type clinicalTrialsGovResultsSection struct {
	ParticipantFlowModule clinicalTrialsGovUpstreamParticipantFlow `json:"participantFlowModule"`
	OutcomeMeasuresModule struct {
		OutcomeMeasures []clinicalTrialsGovUpstreamReportedOutcome `json:"outcomeMeasures"`
	} `json:"outcomeMeasuresModule"`
}

type clinicalTrialsGovUpstreamParticipantFlow struct {
	RecruitmentDetails   string                                 `json:"recruitmentDetails"`
	PreAssignmentDetails string                                 `json:"preAssignmentDetails"`
	TypeUnitsAnalyzed    string                                 `json:"typeUnitsAnalyzed"`
	Groups               []clinicalTrialsGovUpstreamResultGroup `json:"groups"`
	Periods              []struct {
		Title      string `json:"title"`
		Milestones []struct {
			Type         string `json:"type"`
			Achievements []struct {
				GroupID     string `json:"groupId"`
				NumSubjects string `json:"numSubjects"`
			} `json:"achievements"`
		} `json:"milestones"`
		DropWithdraws []struct {
			Type    string `json:"type"`
			Reasons []struct {
				GroupID     string `json:"groupId"`
				NumSubjects string `json:"numSubjects"`
			} `json:"reasons"`
		} `json:"dropWithdraws"`
	} `json:"periods"`
}

type clinicalTrialsGovUpstreamReportedOutcome struct {
	Type                  string                                 `json:"type"`
	Title                 string                                 `json:"title"`
	Description           string                                 `json:"description"`
	TimeFrame             string                                 `json:"timeFrame"`
	UnitOfMeasure         string                                 `json:"unitOfMeasure"`
	Parameter             string                                 `json:"paramType"`
	Dispersion            string                                 `json:"dispersionType"`
	ReportingStatus       string                                 `json:"reportingStatus"`
	PopulationDescription string                                 `json:"populationDescription"`
	Groups                []clinicalTrialsGovUpstreamResultGroup `json:"groups"`
	Classes               []clinicalTrialsGovUpstreamResultClass `json:"classes"`
	Denominators          []clinicalTrialsGovUpstreamDenominator `json:"denoms"`
	Analyses              []clinicalTrialsGovUpstreamAnalysis    `json:"analyses"`
}

type clinicalTrialsGovUpstreamDenominator struct {
	Units  string `json:"units"`
	Counts []struct {
		GroupID string `json:"groupId"`
		Value   string `json:"value"`
	} `json:"counts"`
}

type clinicalTrialsGovUpstreamAnalysis struct {
	GroupIDs           []string `json:"groupIds"`
	NonInferiorityType string   `json:"nonInferiorityType"`
	PValue             string   `json:"pValue"`
	StatisticalMethod  string   `json:"statisticalMethod"`
	EffectParameter    string   `json:"paramType"`
	EffectEstimate     string   `json:"paramValue"`
	DispersionType     string   `json:"dispersionType"`
	DispersionValue    string   `json:"dispersionValue"`
	ConfidenceLevel    string   `json:"ciPctValue"`
	ConfidenceSides    string   `json:"ciNumSides"`
	ConfidenceLower    string   `json:"ciLowerLimit"`
	ConfidenceUpper    string   `json:"ciUpperLimit"`
}

type clinicalTrialsGovUpstreamResultGroup struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type clinicalTrialsGovUpstreamResultClass struct {
	Title      string                                    `json:"title"`
	Categories []clinicalTrialsGovUpstreamResultCategory `json:"categories"`
}

type clinicalTrialsGovUpstreamResultCategory struct {
	Title        string                                      `json:"title"`
	Measurements []clinicalTrialsGovUpstreamGroupMeasurement `json:"measurements"`
}

type clinicalTrialsGovUpstreamGroupMeasurement struct {
	GroupID    string `json:"groupId"`
	Value      string `json:"value"`
	Spread     string `json:"spread"`
	LowerLimit string `json:"lowerLimit"`
	UpperLimit string `json:"upperLimit"`
	Comment    string `json:"comment"`
}

type clinicalTrialsGovProtocol struct {
	IdentificationModule struct {
		NCTID         string `json:"nctId"`
		BriefTitle    string `json:"briefTitle"`
		OfficialTitle string `json:"officialTitle"`
	} `json:"identificationModule"`
	StatusModule struct {
		OverallStatus               string                        `json:"overallStatus"`
		StartDateStruct             clinicalTrialsGovUpstreamDate `json:"startDateStruct"`
		PrimaryCompletionDateStruct clinicalTrialsGovUpstreamDate `json:"primaryCompletionDateStruct"`
		CompletionDateStruct        clinicalTrialsGovUpstreamDate `json:"completionDateStruct"`
		ResultsFirstPostDate        string                        `json:"resultsFirstPostDate"`
		ResultsFirstPostDateStruct  clinicalTrialsGovUpstreamDate `json:"resultsFirstPostDateStruct"`
		LastUpdatePostDateStruct    clinicalTrialsGovUpstreamDate `json:"lastUpdatePostDateStruct"`
	} `json:"statusModule"`
	DesignModule struct {
		StudyType      string   `json:"studyType"`
		Phases         []string `json:"phases"`
		EnrollmentInfo struct {
			Count int64  `json:"count"`
			Type  string `json:"type"`
		} `json:"enrollmentInfo"`
		DesignInfo struct {
			Allocation        string `json:"allocation"`
			InterventionModel string `json:"interventionModel"`
			PrimaryPurpose    string `json:"primaryPurpose"`
			MaskingInfo       struct {
				Masking   string   `json:"masking"`
				WhoMasked []string `json:"whoMasked"`
			} `json:"maskingInfo"`
		} `json:"designInfo"`
	} `json:"designModule"`
	ConditionsModule struct {
		Conditions []string `json:"conditions"`
	} `json:"conditionsModule"`
	ArmsInterventionsModule struct {
		ArmGroups     []ClinicalTrialsGovArm `json:"armGroups"`
		Interventions []struct {
			Type           string   `json:"type"`
			Name           string   `json:"name"`
			Description    string   `json:"description"`
			ArmGroupLabels []string `json:"armGroupLabels"`
			OtherNames     []string `json:"otherNames"`
		} `json:"interventions"`
	} `json:"armsInterventionsModule"`
	OutcomesModule struct {
		PrimaryOutcomes   []clinicalTrialsGovUpstreamOutcome `json:"primaryOutcomes"`
		SecondaryOutcomes []clinicalTrialsGovUpstreamOutcome `json:"secondaryOutcomes"`
	} `json:"outcomesModule"`
	ReferencesModule struct {
		References []ClinicalTrialsGovPublicationReference `json:"references"`
	} `json:"referencesModule"`
}

type clinicalTrialsGovUpstreamDate struct {
	Date string `json:"date"`
	Type string `json:"type"`
}

type clinicalTrialsGovUpstreamOutcome struct {
	Measure     string `json:"measure"`
	Description string `json:"description"`
	TimeFrame   string `json:"timeFrame"`
}

func normalizeClinicalTrialsGovStudy(body []byte, apiVersion string) (ClinicalTrialsGovStudy, error) {
	if err := validateClinicalTrialsGovDocumentBounds(body); err != nil {
		return ClinicalTrialsGovStudy{}, err
	}
	var raw clinicalTrialsGovRawStudy
	if err := json.Unmarshal(body, &raw); err != nil {
		return ClinicalTrialsGovStudy{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
	}
	if len(raw.ProtocolSection) == 0 || string(raw.ProtocolSection) == "null" {
		return ClinicalTrialsGovStudy{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid}
	}
	var protocol clinicalTrialsGovProtocol
	if err := json.Unmarshal(raw.ProtocolSection, &protocol); err != nil {
		return ClinicalTrialsGovStudy{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	study := ClinicalTrialsGovStudy{
		SourceAPIVersion: strings.TrimSpace(apiVersion),
		NCTID:            strings.ToUpper(strings.TrimSpace(protocol.IdentificationModule.NCTID)),
		BriefTitle:       strings.TrimSpace(protocol.IdentificationModule.BriefTitle),
		OfficialTitle:    strings.TrimSpace(protocol.IdentificationModule.OfficialTitle),
		OverallStatus:    strings.TrimSpace(protocol.StatusModule.OverallStatus),
		StudyType:        strings.TrimSpace(protocol.DesignModule.StudyType),
		Phases:           cleanClinicalTrialsGovStrings(protocol.DesignModule.Phases),
		Enrollment: ClinicalTrialsGovEnrollment{
			Count: protocol.DesignModule.EnrollmentInfo.Count,
			Type:  strings.TrimSpace(protocol.DesignModule.EnrollmentInfo.Type),
		},
		Design: ClinicalTrialsGovDesign{
			Allocation:        strings.TrimSpace(protocol.DesignModule.DesignInfo.Allocation),
			InterventionModel: strings.TrimSpace(protocol.DesignModule.DesignInfo.InterventionModel),
			Masking:           strings.TrimSpace(protocol.DesignModule.DesignInfo.MaskingInfo.Masking),
			WhoMasked:         cleanClinicalTrialsGovStrings(protocol.DesignModule.DesignInfo.MaskingInfo.WhoMasked),
			PrimaryPurpose:    strings.TrimSpace(protocol.DesignModule.DesignInfo.PrimaryPurpose),
		},
		Conditions:            cleanClinicalTrialsGovStrings(protocol.ConditionsModule.Conditions),
		Arms:                  normalizeClinicalTrialsGovArms(protocol.ArmsInterventionsModule.ArmGroups),
		Interventions:         normalizeClinicalTrialsGovInterventions(protocol.ArmsInterventionsModule.Interventions),
		PrimaryOutcomes:       normalizeClinicalTrialsGovOutcomes(protocol.OutcomesModule.PrimaryOutcomes),
		SecondaryOutcomes:     normalizeClinicalTrialsGovOutcomes(protocol.OutcomesModule.SecondaryOutcomes),
		HasResults:            raw.HasResults,
		PublicationReferences: normalizeClinicalTrialsGovReferences(protocol.ReferencesModule.References),
		Coverage: ClinicalTrialsGovEvidenceCoverage{
			IncludedModules: []string{"protocol", "participant_flow", "outcome_measures"},
			ExcludedModules: []string{"baseline_characteristics", "adverse_events", "more_info"},
			Limitations:     []string{"ClinicalTrials.gov baseline characteristics, adverse events, and more-info modules are outside v1 normalized evidence coverage."},
		},
	}
	var err error
	if study.ReportedOutcomes, err = normalizeClinicalTrialsGovReportedOutcomes(raw.ResultsSection); err != nil {
		return ClinicalTrialsGovStudy{}, err
	}
	if study.ParticipantFlow, err = normalizeClinicalTrialsGovParticipantFlow(raw.ResultsSection); err != nil {
		return ClinicalTrialsGovStudy{}, err
	}
	if study.StartDate, err = normalizeClinicalTrialsGovDate(protocol.StatusModule.StartDateStruct); err != nil {
		return ClinicalTrialsGovStudy{}, schemaInvalidClinicalTrialsGovError(err)
	}
	if study.PrimaryCompletionDate, err = normalizeClinicalTrialsGovDate(protocol.StatusModule.PrimaryCompletionDateStruct); err != nil {
		return ClinicalTrialsGovStudy{}, schemaInvalidClinicalTrialsGovError(err)
	}
	if study.CompletionDate, err = normalizeClinicalTrialsGovDate(protocol.StatusModule.CompletionDateStruct); err != nil {
		return ClinicalTrialsGovStudy{}, schemaInvalidClinicalTrialsGovError(err)
	}
	resultsFirstPosted := protocol.StatusModule.ResultsFirstPostDateStruct
	if resultsFirstPosted.Date == "" {
		resultsFirstPosted.Date = protocol.StatusModule.ResultsFirstPostDate
	}
	if study.ResultsFirstPosted, err = normalizeClinicalTrialsGovDate(resultsFirstPosted); err != nil {
		return ClinicalTrialsGovStudy{}, schemaInvalidClinicalTrialsGovError(err)
	}
	if study.LastUpdatePosted, err = normalizeClinicalTrialsGovDate(protocol.StatusModule.LastUpdatePostDateStruct); err != nil {
		return ClinicalTrialsGovStudy{}, schemaInvalidClinicalTrialsGovError(err)
	}
	if study.SourceAPIVersion == "" || study.NCTID == "" || study.BriefTitle == "" || study.OverallStatus == "" || study.StudyType == "" || study.LastUpdatePosted.Value == "" {
		return ClinicalTrialsGovStudy{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid}
	}
	return study, nil
}

type clinicalTrialsGovOutcomeBounds struct {
	categories     int
	measurements   int
	denomCounts    int
	analysisGroups int
}

type clinicalTrialsGovFlowBounds struct {
	milestones    int
	achievements  int
	dropWithdraws int
	reasons       int
}

func validateClinicalTrialsGovDocumentBounds(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := walkClinicalTrialsGovJSON(decoder, nil, nil, &clinicalTrialsGovFlowBounds{}); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
	}
	return nil
}

func walkClinicalTrialsGovJSON(decoder *json.Decoder, path []string, outcomeBounds *clinicalTrialsGovOutcomeBounds, flowBounds *clinicalTrialsGovFlowBounds) error {
	token, err := decoder.Token()
	if err != nil {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
			}
			key, ok := keyToken.(string)
			if !ok {
				return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON}
			}
			if err := walkClinicalTrialsGovJSON(decoder, append(path, key), outcomeBounds, flowBounds); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
		}
	case '[':
		limit := clinicalTrialsGovArrayLimit(path, outcomeBounds)
		isOutcomeArray := clinicalTrialsGovPathHasSuffix(path, "resultsSection", "outcomeMeasuresModule", "outcomeMeasures")
		count := 0
		for decoder.More() {
			count++
			if limit > 0 && count > limit {
				return schemaInvalidClinicalTrialsGovError(errors.New("ClinicalTrials.gov collection exceeds limit"))
			}
			childBounds := outcomeBounds
			if isOutcomeArray {
				childBounds = &clinicalTrialsGovOutcomeBounds{}
			}
			if err := incrementClinicalTrialsGovAggregateBound(path, childBounds, flowBounds); err != nil {
				return err
			}
			if err := walkClinicalTrialsGovJSON(decoder, path, childBounds, flowBounds); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON, cause: err}
		}
	default:
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorMalformedJSON}
	}
	return nil
}

func clinicalTrialsGovArrayLimit(path []string, outcomeBounds *clinicalTrialsGovOutcomeBounds) int {
	switch {
	case clinicalTrialsGovPathHasSuffix(path, "resultsSection", "outcomeMeasuresModule", "outcomeMeasures"):
		return ClinicalTrialsGovMaxReportedOutcomes
	case outcomeBounds != nil && clinicalTrialsGovPathHasSuffix(path, "groups"):
		return ClinicalTrialsGovMaxResultGroups
	case outcomeBounds != nil && clinicalTrialsGovPathHasSuffix(path, "classes"):
		return ClinicalTrialsGovMaxResultClasses
	case outcomeBounds != nil && clinicalTrialsGovPathHasSuffix(path, "denoms"):
		return ClinicalTrialsGovMaxResultDenominators
	case outcomeBounds != nil && clinicalTrialsGovPathHasSuffix(path, "analyses"):
		return ClinicalTrialsGovMaxResultAnalyses
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "designModule", "phases"):
		return 16
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "designModule", "designInfo", "maskingInfo", "whoMasked"):
		return 16
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "conditionsModule", "conditions"):
		return ClinicalTrialsGovMaxProtocolConditions
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "armsInterventionsModule", "armGroups"):
		return ClinicalTrialsGovMaxProtocolArms
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "armsInterventionsModule", "interventions"):
		return ClinicalTrialsGovMaxProtocolInterventions
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "armsInterventionsModule", "armGroups", "interventionNames"):
		return ClinicalTrialsGovMaxProtocolInterventionNames
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "armsInterventionsModule", "interventions", "armGroupLabels"):
		return ClinicalTrialsGovMaxProtocolArmGroupLabels
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "armsInterventionsModule", "interventions", "otherNames"):
		return ClinicalTrialsGovMaxProtocolOtherNames
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "outcomesModule", "primaryOutcomes"),
		clinicalTrialsGovPathHasSuffix(path, "protocolSection", "outcomesModule", "secondaryOutcomes"):
		return ClinicalTrialsGovMaxProtocolOutcomes
	case clinicalTrialsGovPathHasSuffix(path, "protocolSection", "referencesModule", "references"):
		return ClinicalTrialsGovMaxProtocolReferences
	case clinicalTrialsGovPathHasSuffix(path, "resultsSection", "participantFlowModule", "groups"):
		return ClinicalTrialsGovMaxFlowGroups
	case clinicalTrialsGovPathHasSuffix(path, "resultsSection", "participantFlowModule", "periods"):
		return ClinicalTrialsGovMaxFlowPeriods
	default:
		return 0
	}
}

func incrementClinicalTrialsGovAggregateBound(path []string, bounds *clinicalTrialsGovOutcomeBounds, flow *clinicalTrialsGovFlowBounds) error {
	switch {
	case bounds != nil && clinicalTrialsGovPathHasSuffix(path, "categories"):
		bounds.categories++
		if bounds.categories > ClinicalTrialsGovMaxResultCategories {
			return schemaInvalidClinicalTrialsGovError(errors.New("reported outcome categories exceed aggregate limit"))
		}
	case bounds != nil && clinicalTrialsGovPathHasSuffix(path, "measurements"):
		bounds.measurements++
		if bounds.measurements > ClinicalTrialsGovMaxGroupMeasurements {
			return schemaInvalidClinicalTrialsGovError(errors.New("reported outcome measurements exceed aggregate limit"))
		}
	case bounds != nil && clinicalTrialsGovPathHasSuffix(path, "counts"):
		bounds.denomCounts++
		if bounds.denomCounts > ClinicalTrialsGovMaxDenominatorCounts {
			return schemaInvalidClinicalTrialsGovError(errors.New("reported outcome denominator counts exceed aggregate limit"))
		}
	case bounds != nil && clinicalTrialsGovPathHasSuffix(path, "groupIds"):
		bounds.analysisGroups++
		if bounds.analysisGroups > ClinicalTrialsGovMaxAnalysisGroups {
			return schemaInvalidClinicalTrialsGovError(errors.New("reported outcome analysis groups exceed aggregate limit"))
		}
	case flow != nil && clinicalTrialsGovPathHasSuffix(path, "participantFlowModule", "periods", "milestones"):
		flow.milestones++
		if flow.milestones > ClinicalTrialsGovMaxFlowMilestones {
			return schemaInvalidClinicalTrialsGovError(errors.New("participant flow milestones exceed aggregate limit"))
		}
	case flow != nil && clinicalTrialsGovPathHasSuffix(path, "participantFlowModule", "periods", "milestones", "achievements"):
		flow.achievements++
		if flow.achievements > ClinicalTrialsGovMaxFlowAchievements {
			return schemaInvalidClinicalTrialsGovError(errors.New("participant flow achievements exceed aggregate limit"))
		}
	case flow != nil && clinicalTrialsGovPathHasSuffix(path, "participantFlowModule", "periods", "dropWithdraws"):
		flow.dropWithdraws++
		if flow.dropWithdraws > ClinicalTrialsGovMaxFlowDropWithdraws {
			return schemaInvalidClinicalTrialsGovError(errors.New("participant flow drop-withdraw rows exceed aggregate limit"))
		}
	case flow != nil && clinicalTrialsGovPathHasSuffix(path, "participantFlowModule", "periods", "dropWithdraws", "reasons"):
		flow.reasons++
		if flow.reasons > ClinicalTrialsGovMaxFlowReasons {
			return schemaInvalidClinicalTrialsGovError(errors.New("participant flow reasons exceed aggregate limit"))
		}
	}
	return nil
}

func clinicalTrialsGovPathHasSuffix(path []string, suffix ...string) bool {
	if len(path) < len(suffix) {
		return false
	}
	start := len(path) - len(suffix)
	for index := range suffix {
		if path[start+index] != suffix[index] {
			return false
		}
	}
	return true
}

func normalizeClinicalTrialsGovDate(upstream clinicalTrialsGovUpstreamDate) (ClinicalTrialsGovDate, error) {
	raw := strings.TrimSpace(upstream.Date)
	if raw == "" {
		return ClinicalTrialsGovDate{}, nil
	}
	formats := []struct {
		layout    string
		precision string
	}{
		{time.RFC3339Nano, "time"},
		{"2006-01-02T15:04:05.999999999", "time"},
		{"2006-01-02", "day"},
		{"2006-01", "month"},
		{"2006", "year"},
	}
	for _, candidate := range formats {
		parsed, err := time.Parse(candidate.layout, raw)
		if err == nil {
			return ClinicalTrialsGovDate{
				Value:     parsed.UTC().Format(time.RFC3339Nano),
				Type:      strings.TrimSpace(upstream.Type),
				Precision: candidate.precision,
			}, nil
		}
	}
	return ClinicalTrialsGovDate{}, fmt.Errorf("invalid upstream date")
}

func canonicalClinicalTrialsGovTimestamp(raw string) (string, error) {
	date, err := normalizeClinicalTrialsGovDate(clinicalTrialsGovUpstreamDate{Date: raw})
	if err != nil || date.Value == "" {
		return "", fmt.Errorf("invalid upstream timestamp")
	}
	return date.Value, nil
}

func cleanClinicalTrialsGovStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func normalizeClinicalTrialsGovArms(arms []ClinicalTrialsGovArm) []ClinicalTrialsGovArm {
	normalized := make([]ClinicalTrialsGovArm, 0, len(arms))
	for _, arm := range arms {
		arm.Label = strings.TrimSpace(arm.Label)
		arm.Type = strings.TrimSpace(arm.Type)
		arm.Description = strings.TrimSpace(arm.Description)
		arm.InterventionNames = cleanClinicalTrialsGovStrings(arm.InterventionNames)
		if arm.Label != "" {
			normalized = append(normalized, arm)
		}
	}
	return normalized
}

func normalizeClinicalTrialsGovInterventions(values []struct {
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	ArmGroupLabels []string `json:"armGroupLabels"`
	OtherNames     []string `json:"otherNames"`
}) []ClinicalTrialsGovIntervention {
	normalized := make([]ClinicalTrialsGovIntervention, 0, len(values))
	for _, value := range values {
		intervention := ClinicalTrialsGovIntervention{
			Type:           strings.TrimSpace(value.Type),
			Name:           strings.TrimSpace(value.Name),
			Description:    strings.TrimSpace(value.Description),
			ArmGroupLabels: cleanClinicalTrialsGovStrings(value.ArmGroupLabels),
			OtherNames:     cleanClinicalTrialsGovStrings(value.OtherNames),
		}
		if intervention.Name != "" {
			normalized = append(normalized, intervention)
		}
	}
	return normalized
}

func normalizeClinicalTrialsGovOutcomes(values []clinicalTrialsGovUpstreamOutcome) []ClinicalTrialsGovOutcome {
	normalized := make([]ClinicalTrialsGovOutcome, 0, len(values))
	for _, value := range values {
		outcome := ClinicalTrialsGovOutcome{
			Measure:     strings.TrimSpace(value.Measure),
			Description: strings.TrimSpace(value.Description),
			TimeFrame:   strings.TrimSpace(value.TimeFrame),
		}
		if outcome.Measure != "" {
			normalized = append(normalized, outcome)
		}
	}
	return normalized
}

func normalizeClinicalTrialsGovReportedOutcomes(raw json.RawMessage) ([]ClinicalTrialsGovReportedOutcome, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var results clinicalTrialsGovResultsSection
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, schemaInvalidClinicalTrialsGovError(err)
	}
	values := results.OutcomeMeasuresModule.OutcomeMeasures
	if len(values) > ClinicalTrialsGovMaxReportedOutcomes {
		return nil, schemaInvalidClinicalTrialsGovError(errors.New("reported outcomes exceed limit"))
	}
	normalized := make([]ClinicalTrialsGovReportedOutcome, 0, len(values))
	for _, value := range values {
		outcome, err := normalizeClinicalTrialsGovReportedOutcome(value)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, outcome)
	}
	return normalized, nil
}

func normalizeClinicalTrialsGovParticipantFlow(raw json.RawMessage) (ClinicalTrialsGovParticipantFlow, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ClinicalTrialsGovParticipantFlow{}, nil
	}
	var results clinicalTrialsGovResultsSection
	if err := json.Unmarshal(raw, &results); err != nil {
		return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(err)
	}
	value := results.ParticipantFlowModule
	if len(value.Groups) > ClinicalTrialsGovMaxFlowGroups || len(value.Periods) > ClinicalTrialsGovMaxFlowPeriods {
		return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow structure exceeds limit"))
	}
	flow := ClinicalTrialsGovParticipantFlow{
		RecruitmentDetails:   strings.TrimSpace(value.RecruitmentDetails),
		PreAssignmentDetails: strings.TrimSpace(value.PreAssignmentDetails),
		TypeUnitsAnalyzed:    strings.TrimSpace(value.TypeUnitsAnalyzed),
		Groups:               make([]ClinicalTrialsGovResultGroup, 0, len(value.Groups)),
		Periods:              make([]ClinicalTrialsGovFlowPeriod, 0, len(value.Periods)),
	}
	groupIDs := make(map[string]struct{}, len(value.Groups))
	for _, rawGroup := range value.Groups {
		group := ClinicalTrialsGovResultGroup{ID: strings.TrimSpace(rawGroup.ID), Title: strings.TrimSpace(rawGroup.Title), Description: strings.TrimSpace(rawGroup.Description)}
		if group.ID == "" || group.Title == "" {
			return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow group identity is required"))
		}
		if _, exists := groupIDs[group.ID]; exists {
			return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("duplicate participant flow group identity"))
		}
		groupIDs[group.ID] = struct{}{}
		flow.Groups = append(flow.Groups, group)
	}
	for _, rawPeriod := range value.Periods {
		period := ClinicalTrialsGovFlowPeriod{Title: strings.TrimSpace(rawPeriod.Title)}
		if period.Title == "" {
			return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow period title is required"))
		}
		period.Milestones = make([]ClinicalTrialsGovFlowMilestone, 0, len(rawPeriod.Milestones))
		for _, rawMilestone := range rawPeriod.Milestones {
			milestone := ClinicalTrialsGovFlowMilestone{Type: strings.TrimSpace(rawMilestone.Type), Achievements: make([]ClinicalTrialsGovFlowCount, 0, len(rawMilestone.Achievements))}
			if milestone.Type == "" {
				return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow milestone type is required"))
			}
			for _, rawCount := range rawMilestone.Achievements {
				count, err := normalizeClinicalTrialsGovFlowCount(rawCount.GroupID, rawCount.NumSubjects, groupIDs)
				if err != nil {
					return ClinicalTrialsGovParticipantFlow{}, err
				}
				milestone.Achievements = append(milestone.Achievements, count)
			}
			period.Milestones = append(period.Milestones, milestone)
		}
		period.DropWithdraws = make([]ClinicalTrialsGovFlowDropWithdraw, 0, len(rawPeriod.DropWithdraws))
		for _, rawDrop := range rawPeriod.DropWithdraws {
			drop := ClinicalTrialsGovFlowDropWithdraw{Type: strings.TrimSpace(rawDrop.Type), Reasons: make([]ClinicalTrialsGovFlowCount, 0, len(rawDrop.Reasons))}
			if drop.Type == "" {
				return ClinicalTrialsGovParticipantFlow{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow drop-withdraw type is required"))
			}
			for _, rawCount := range rawDrop.Reasons {
				count, err := normalizeClinicalTrialsGovFlowCount(rawCount.GroupID, rawCount.NumSubjects, groupIDs)
				if err != nil {
					return ClinicalTrialsGovParticipantFlow{}, err
				}
				drop.Reasons = append(drop.Reasons, count)
			}
			period.DropWithdraws = append(period.DropWithdraws, drop)
		}
		flow.Periods = append(flow.Periods, period)
	}
	return flow, nil
}

func normalizeClinicalTrialsGovFlowCount(groupID, subjects string, groupIDs map[string]struct{}) (ClinicalTrialsGovFlowCount, error) {
	count := ClinicalTrialsGovFlowCount{GroupID: strings.TrimSpace(groupID), Subjects: strings.TrimSpace(subjects)}
	if count.GroupID == "" || count.Subjects == "" {
		return ClinicalTrialsGovFlowCount{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow count identity and subjects are required"))
	}
	if _, exists := groupIDs[count.GroupID]; !exists {
		return ClinicalTrialsGovFlowCount{}, schemaInvalidClinicalTrialsGovError(errors.New("participant flow count references unknown group"))
	}
	return count, nil
}

func normalizeClinicalTrialsGovReportedOutcome(value clinicalTrialsGovUpstreamReportedOutcome) (ClinicalTrialsGovReportedOutcome, error) {
	if len(value.Groups) > ClinicalTrialsGovMaxResultGroups || len(value.Classes) > ClinicalTrialsGovMaxResultClasses || len(value.Denominators) > ClinicalTrialsGovMaxResultDenominators || len(value.Analyses) > ClinicalTrialsGovMaxResultAnalyses {
		return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome structure exceeds limit"))
	}
	outcome := ClinicalTrialsGovReportedOutcome{
		Type:                  strings.TrimSpace(value.Type),
		Title:                 strings.TrimSpace(value.Title),
		Description:           strings.TrimSpace(value.Description),
		TimeFrame:             strings.TrimSpace(value.TimeFrame),
		UnitOfMeasure:         strings.TrimSpace(value.UnitOfMeasure),
		Parameter:             strings.TrimSpace(value.Parameter),
		Dispersion:            strings.TrimSpace(value.Dispersion),
		ReportingStatus:       strings.TrimSpace(value.ReportingStatus),
		PopulationDescription: strings.TrimSpace(value.PopulationDescription),
		Groups:                make([]ClinicalTrialsGovResultGroup, 0, len(value.Groups)),
		Classes:               make([]ClinicalTrialsGovResultClass, 0, len(value.Classes)),
		Denominators:          make([]ClinicalTrialsGovDenominator, 0, len(value.Denominators)),
		Analyses:              make([]ClinicalTrialsGovResultAnalysis, 0, len(value.Analyses)),
	}
	if outcome.Type == "" || outcome.Title == "" {
		return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome identity is required"))
	}
	groupIDs := make(map[string]struct{}, len(value.Groups))
	for _, valueGroup := range value.Groups {
		group := ClinicalTrialsGovResultGroup{
			ID:          strings.TrimSpace(valueGroup.ID),
			Title:       strings.TrimSpace(valueGroup.Title),
			Description: strings.TrimSpace(valueGroup.Description),
		}
		if group.ID == "" || group.Title == "" {
			return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome group identity is required"))
		}
		if _, exists := groupIDs[group.ID]; exists {
			return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("duplicate reported outcome group identity"))
		}
		groupIDs[group.ID] = struct{}{}
		outcome.Groups = append(outcome.Groups, group)
	}
	measurementCount := 0
	for _, valueClass := range value.Classes {
		if len(valueClass.Categories) > ClinicalTrialsGovMaxResultCategories {
			return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome categories exceed limit"))
		}
		class := ClinicalTrialsGovResultClass{
			Title:      strings.TrimSpace(valueClass.Title),
			Categories: make([]ClinicalTrialsGovResultCategory, 0, len(valueClass.Categories)),
		}
		for _, valueCategory := range valueClass.Categories {
			measurementCount += len(valueCategory.Measurements)
			if measurementCount > ClinicalTrialsGovMaxGroupMeasurements {
				return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome measurements exceed limit"))
			}
			category := ClinicalTrialsGovResultCategory{
				Title:        strings.TrimSpace(valueCategory.Title),
				Measurements: make([]ClinicalTrialsGovGroupMeasurement, 0, len(valueCategory.Measurements)),
			}
			for _, valueMeasurement := range valueCategory.Measurements {
				measurement := ClinicalTrialsGovGroupMeasurement{
					GroupID:    strings.TrimSpace(valueMeasurement.GroupID),
					Value:      strings.TrimSpace(valueMeasurement.Value),
					Spread:     strings.TrimSpace(valueMeasurement.Spread),
					LowerLimit: strings.TrimSpace(valueMeasurement.LowerLimit),
					UpperLimit: strings.TrimSpace(valueMeasurement.UpperLimit),
					Comment:    strings.TrimSpace(valueMeasurement.Comment),
				}
				if measurement.GroupID == "" {
					return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome measurement group is required"))
				}
				if _, exists := groupIDs[measurement.GroupID]; !exists {
					return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome measurement references unknown group"))
				}
				category.Measurements = append(category.Measurements, measurement)
			}
			class.Categories = append(class.Categories, category)
		}
		outcome.Classes = append(outcome.Classes, class)
	}
	denominatorCount := 0
	for _, valueDenominator := range value.Denominators {
		denominatorCount += len(valueDenominator.Counts)
		if denominatorCount > ClinicalTrialsGovMaxDenominatorCounts {
			return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome denominator counts exceed limit"))
		}
		denominator := ClinicalTrialsGovDenominator{
			Units:  strings.TrimSpace(valueDenominator.Units),
			Counts: make([]ClinicalTrialsGovDenominatorCount, 0, len(valueDenominator.Counts)),
		}
		for _, valueCount := range valueDenominator.Counts {
			count := ClinicalTrialsGovDenominatorCount{
				GroupID: strings.TrimSpace(valueCount.GroupID),
				Value:   strings.TrimSpace(valueCount.Value),
			}
			if _, exists := groupIDs[count.GroupID]; !exists {
				return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome denominator references unknown group"))
			}
			denominator.Counts = append(denominator.Counts, count)
		}
		outcome.Denominators = append(outcome.Denominators, denominator)
	}
	analysisGroupCount := 0
	for _, valueAnalysis := range value.Analyses {
		analysisGroupCount += len(valueAnalysis.GroupIDs)
		if analysisGroupCount > ClinicalTrialsGovMaxAnalysisGroups {
			return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome analysis groups exceed limit"))
		}
		analysis := ClinicalTrialsGovResultAnalysis{
			GroupIDs:           cleanClinicalTrialsGovStrings(valueAnalysis.GroupIDs),
			NonInferiorityType: strings.TrimSpace(valueAnalysis.NonInferiorityType),
			PValue:             strings.TrimSpace(valueAnalysis.PValue),
			StatisticalMethod:  strings.TrimSpace(valueAnalysis.StatisticalMethod),
			EffectParameter:    strings.TrimSpace(valueAnalysis.EffectParameter),
			EffectEstimate:     strings.TrimSpace(valueAnalysis.EffectEstimate),
			DispersionType:     strings.TrimSpace(valueAnalysis.DispersionType),
			DispersionValue:    strings.TrimSpace(valueAnalysis.DispersionValue),
			ConfidenceLevel:    strings.TrimSpace(valueAnalysis.ConfidenceLevel),
			ConfidenceSides:    strings.TrimSpace(valueAnalysis.ConfidenceSides),
			ConfidenceLower:    strings.TrimSpace(valueAnalysis.ConfidenceLower),
			ConfidenceUpper:    strings.TrimSpace(valueAnalysis.ConfidenceUpper),
		}
		for _, groupID := range analysis.GroupIDs {
			if _, exists := groupIDs[groupID]; !exists {
				return ClinicalTrialsGovReportedOutcome{}, schemaInvalidClinicalTrialsGovError(errors.New("reported outcome analysis references unknown group"))
			}
		}
		outcome.Analyses = append(outcome.Analyses, analysis)
	}
	return outcome, nil
}

func normalizeClinicalTrialsGovReferences(values []ClinicalTrialsGovPublicationReference) []ClinicalTrialsGovPublicationReference {
	normalized := make([]ClinicalTrialsGovPublicationReference, 0, len(values))
	for _, value := range values {
		value.PMID = strings.TrimSpace(value.PMID)
		value.Type = strings.TrimSpace(value.Type)
		value.Citation = strings.TrimSpace(value.Citation)
		if value.Citation != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func schemaInvalidClinicalTrialsGovError(err error) error {
	return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
}
