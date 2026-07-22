package app

import (
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
	"time"
)

const (
	ClinicalTrialsGovBaseURL         = "https://clinicaltrials.gov"
	ClinicalTrialsGovStudySourceType = "clinicaltrials_gov_study"
	ClinicalTrialsGovMaxStudyBytes   = 1 << 20
	ClinicalTrialsGovMaxVersionBytes = 64 << 10
	ClinicalTrialsGovMaxRetryAfter   = time.Hour

	clinicalTrialsGovUserAgent = "dedao-kbase/clinical-trial-audit"
)

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
	return e.Kind == ClinicalTrialsGovErrorRateLimited || e.Kind == ClinicalTrialsGovErrorTimeout || e.Kind == ClinicalTrialsGovErrorUpstream
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
	PublicationReferences []ClinicalTrialsGovPublicationReference `json:"publication_references,omitempty"`
}

type ClinicalTrialsGovStudyResult struct {
	Study         ClinicalTrialsGovStudy      `json:"study"`
	Snapshot      ClinicalTrialSourceSnapshot `json:"snapshot"`
	APIVersion    string                      `json:"api_version"`
	DataTimestamp string                      `json:"data_timestamp"`
}

type ClinicalTrialsGovOption func(*ClinicalTrialsGovClient)

func WithClinicalTrialsGovClock(clock func() time.Time) ClinicalTrialsGovOption {
	return func(client *ClinicalTrialsGovClient) {
		if clock != nil {
			client.now = clock
		}
	}
}

type ClinicalTrialsGovClient struct {
	httpClient *http.Client
	baseURL    *url.URL
	now        func() time.Time
}

func NewClinicalTrialsGovClient(httpClient *http.Client, baseURL string, options ...ClinicalTrialsGovOption) (*ClinicalTrialsGovClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = ClinicalTrialsGovBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid ClinicalTrials.gov base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	cloned := *httpClient
	originalRedirect := cloned.CheckRedirect
	allowedHost := parsed.Host
	allowedScheme := parsed.Scheme
	cloned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Host != allowedHost || request.URL.Scheme != allowedScheme {
			return errors.New("ClinicalTrials.gov redirect host is not allowed")
		}
		if originalRedirect != nil {
			return originalRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("ClinicalTrials.gov redirect limit exceeded")
		}
		return nil
	}
	client := &ClinicalTrialsGovClient{httpClient: &cloned, baseURL: parsed, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(client)
		}
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
	canonicalStudy, err := json.Marshal(study)
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	snapshot, err := FinalizeClinicalTrialSourceSnapshot(ClinicalTrialSourceSnapshot{
		SourceType:        ClinicalTrialsGovStudySourceType,
		CanonicalID:       study.NCTID,
		RetrievedAt:       c.now().UTC().Format(time.RFC3339Nano),
		UpstreamUpdatedAt: study.LastUpdatePosted.Value,
		ContentHash:       hashClinicalTrialValue(string(canonicalStudy)),
		LicenseScope:      "public_metadata",
	})
	if err != nil {
		return ClinicalTrialsGovStudyResult{}, &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorSchemaInvalid, cause: err}
	}
	return ClinicalTrialsGovStudyResult{
		Study:         study,
		Snapshot:      snapshot,
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
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorTimeout, cause: err}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return &ClinicalTrialsGovError{Kind: ClinicalTrialsGovErrorTimeout, cause: err}
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
	default:
		classified.Kind = ClinicalTrialsGovErrorUpstream
	}
	return classified
}

func parseClinicalTrialsGovRetryAfter(raw string, now time.Time) time.Duration {
	var retryAfter time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && seconds > 0 {
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
	HasResults      bool            `json:"hasResults"`
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
	}
	var err error
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
