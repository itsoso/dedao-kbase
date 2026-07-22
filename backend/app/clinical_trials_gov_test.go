package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const clinicalTrialsGovVersionFixture = `{"apiVersion":"2.0.4","dataTimestamp":"2026-07-21T15:04:05"}`

const clinicalTrialsGovStudyFixture = `{
  "protocolSection": {
    "identificationModule": {
      "nctId": "NCT01234567",
      "briefTitle": "Synthetic Trial of Evidence",
      "officialTitle": "A Synthetic Randomized Trial for Contract Testing"
    },
    "statusModule": {
      "overallStatus": "COMPLETED",
      "startDateStruct": {"date": "2024-01-02", "type": "ACTUAL"},
      "primaryCompletionDateStruct": {"date": "2025-03", "type": "ACTUAL"},
      "completionDateStruct": {"date": "2025", "type": "ACTUAL"},
      "resultsFirstPostDate": "2025-08-09",
      "lastUpdatePostDateStruct": {"date": "2026-07-20", "type": "ACTUAL"}
    },
    "designModule": {
      "studyType": "INTERVENTIONAL",
      "phases": ["PHASE2", "PHASE3"],
      "enrollmentInfo": {"count": 240, "type": "ACTUAL"},
      "designInfo": {
        "allocation": "RANDOMIZED",
        "interventionModel": "PARALLEL",
        "primaryPurpose": "TREATMENT",
        "maskingInfo": {"masking": "DOUBLE", "whoMasked": ["PARTICIPANT", "INVESTIGATOR"]}
      }
    },
    "conditionsModule": {"conditions": ["Synthetic condition", "Evidence disorder"]},
    "armsInterventionsModule": {
      "armGroups": [
        {"label": "Experimental", "type": "EXPERIMENTAL", "description": "Synthetic intervention arm", "interventionNames": ["DRUG: Study drug"]},
        {"label": "Control", "type": "PLACEBO_COMPARATOR", "interventionNames": ["DRUG: Placebo"]}
      ],
      "interventions": [
        {"type": "DRUG", "name": "Study drug", "description": "Synthetic only", "armGroupLabels": ["Experimental"], "otherNames": ["Compound S"]},
        {"type": "DRUG", "name": "Placebo", "armGroupLabels": ["Control"]}
      ]
    },
    "outcomesModule": {
      "primaryOutcomes": [{"measure": "Primary synthetic score", "description": "Change from baseline", "timeFrame": "Week 24"}],
      "secondaryOutcomes": [{"measure": "Secondary synthetic score", "timeFrame": "Week 12"}]
    },
    "referencesModule": {
      "references": [
        {"pmid": "12345678", "type": "RESULT", "citation": "Synthetic Author. Synthetic trial report."},
        {"type": "BACKGROUND", "citation": "Synthetic background reference."}
      ]
    }
  },
  "resultsSection": {
    "outcomeMeasuresModule": {
      "outcomeMeasures": [{
        "type": "PRIMARY",
        "title": "Primary synthetic score",
        "description": "Reported change from baseline",
        "populationDescription": "Synthetic analysis population",
        "reportingStatus": "POSTED",
        "paramType": "MEAN",
        "dispersionType": "STANDARD_DEVIATION",
        "units": "points",
        "timeFrame": "Week 24",
        "groups": [
          {"id": "FG000", "title": "Experimental", "description": "Synthetic intervention arm"},
          {"id": "FG001", "title": "Control", "description": "Synthetic control arm"}
        ],
        "classes": [{
          "title": "Overall",
          "categories": [{
            "title": "Change from baseline",
            "measurements": [
              {"groupId": "FG000", "value": "8.2", "spread": "1.1", "lowerLimit": "7.9", "upperLimit": "8.5", "comment": "Synthetic value"},
              {"groupId": "FG001", "value": "5.0", "spread": "1.4"}
            ]
          }]
        }]
      }]
    }
  },
  "annotationSection": {"annotationModule": {}},
  "documentSection": {"largeDocumentModule": {}},
  "derivedSection": {"miscInfoModule": {"versionHolder": "2026-07-20"}},
  "hasResults": true
}`

func TestClinicalTrialsGovGetStudyNormalizesCurrentRecord(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "dedao-kbase/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/api/v2/version":
			fmt.Fprint(w, clinicalTrialsGovVersionFixture)
		case "/api/v2/studies/NCT01234567":
			fmt.Fprint(w, clinicalTrialsGovStudyFixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	retrievedAt := time.Date(2026, 7, 22, 9, 30, 0, 123, time.FixedZone("test", -4*60*60))
	client, err := newClinicalTrialsGovClient(server.Client(), server.URL, withClinicalTrialsGovClock(func() time.Time { return retrievedAt }))
	if err != nil {
		t.Fatalf("NewClinicalTrialsGovClient() error = %v", err)
	}
	result, err := client.GetStudy(context.Background(), " nct 01234567 ")
	if err != nil {
		t.Fatalf("GetStudy() error = %v", err)
	}

	if got, want := strings.Join(calls, ","), "/api/v2/version,/api/v2/studies/NCT01234567"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if result.APIVersion != "2.0.4" || result.DataTimestamp != "2026-07-21T15:04:05Z" {
		t.Fatalf("version metadata = %#v", result)
	}
	study := result.Study
	if study.NCTID != "NCT01234567" || study.BriefTitle != "Synthetic Trial of Evidence" || study.OfficialTitle == "" {
		t.Fatalf("identity = %#v", study)
	}
	if study.OverallStatus != "COMPLETED" || study.StudyType != "INTERVENTIONAL" || strings.Join(study.Phases, ",") != "PHASE2,PHASE3" {
		t.Fatalf("status/design = %#v", study)
	}
	if study.Enrollment.Count != 240 || study.Enrollment.Type != "ACTUAL" || study.Design.Allocation != "RANDOMIZED" || study.Design.Masking != "DOUBLE" {
		t.Fatalf("enrollment/design = %#v", study)
	}
	if len(study.Conditions) != 2 || len(study.Arms) != 2 || len(study.Interventions) != 2 || len(study.PrimaryOutcomes) != 1 || len(study.SecondaryOutcomes) != 1 {
		t.Fatalf("normalized collections = %#v", study)
	}
	if study.StartDate.Value != "2024-01-02T00:00:00Z" || study.StartDate.Precision != "day" || study.PrimaryCompletionDate.Precision != "month" || study.CompletionDate.Precision != "year" {
		t.Fatalf("dates = %#v", study)
	}
	if study.ResultsFirstPosted.Value != "2025-08-09T00:00:00Z" || study.LastUpdatePosted.Value != "2026-07-20T00:00:00Z" {
		t.Fatalf("posted dates = %#v", study)
	}
	if !study.HasResults || len(study.PublicationReferences) != 2 || study.PublicationReferences[0].PMID != "12345678" {
		t.Fatalf("results/references = %#v", study)
	}
	if len(study.ReportedOutcomes) != 1 {
		t.Fatalf("reported outcomes = %#v", study.ReportedOutcomes)
	}
	reported := study.ReportedOutcomes[0]
	if reported.Type != "PRIMARY" || reported.Title != "Primary synthetic score" || reported.Description == "" || reported.TimeFrame != "Week 24" || reported.Units != "points" || reported.Parameter != "MEAN" || reported.Dispersion != "STANDARD_DEVIATION" || reported.ReportingStatus != "POSTED" || reported.PopulationDescription == "" {
		t.Fatalf("reported outcome identity = %#v", reported)
	}
	if len(reported.Groups) != 2 || reported.Groups[0].ID != "FG000" || len(reported.Classes) != 1 || len(reported.Classes[0].Categories) != 1 || len(reported.Classes[0].Categories[0].Measurements) != 2 {
		t.Fatalf("reported outcome values = %#v", reported)
	}
	measurement := reported.Classes[0].Categories[0].Measurements[0]
	if measurement.GroupID != "FG000" || measurement.Value != "8.2" || measurement.Spread != "1.1" || measurement.LowerLimit != "7.9" || measurement.UpperLimit != "8.5" {
		t.Fatalf("reported measurement = %#v", measurement)
	}
	if result.Snapshot.SourceType != ClinicalTrialsGovStudySourceType || result.Snapshot.CanonicalID != "NCT01234567" || result.Snapshot.LicenseScope != "public_metadata" {
		t.Fatalf("snapshot identity = %#v", result.Snapshot)
	}
	if result.Snapshot.RetrievedAt != "2026-07-22T13:30:00.000000123Z" || result.Snapshot.UpstreamUpdatedAt != "2026-07-20T00:00:00Z" || result.Snapshot.ContentHash == "" || result.Snapshot.Fingerprint == "" {
		t.Fatalf("snapshot metadata = %#v", result.Snapshot)
	}
}

func TestClinicalTrialsGovSnapshotIdentityIsCanonical(t *testing.T) {
	var requestCount atomic.Int32
	apiVersion := "2.0.4"
	studyBody := clinicalTrialsGovStudyFixture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/version":
			fmt.Fprintf(w, `{"dataTimestamp":"2026-07-21T15:04:05Z","apiVersion":%q}`, apiVersion)
		case "/api/v2/studies/NCT01234567":
			if requestCount.Add(1)%2 == 0 {
				fmt.Fprintf(w, " { \n \"hasResults\": true, \"resultsSection\": %s, \"protocolSection\": %s } ", extractResultsSection(t, studyBody), extractProtocolSection(t, studyBody))
				return
			}
			fmt.Fprint(w, studyBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	client, err := newClinicalTrialsGovClient(server.Client(), server.URL, withClinicalTrialsGovClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.GetStudy(context.Background(), "NCT01234567")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Hour)
	second, err := client.GetStudy(context.Background(), "NCT01234567")
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.ContentHash != second.Snapshot.ContentHash || first.Snapshot.Fingerprint != second.Snapshot.Fingerprint {
		t.Fatalf("object order/retrieval time changed identity: first=%#v second=%#v", first.Snapshot, second.Snapshot)
	}
	if first.Snapshot.RetrievedAt == second.Snapshot.RetrievedAt {
		t.Fatal("retrieval timestamps must reflect each collection")
	}

	apiVersion = "2.1.0"
	versionChanged, err := client.GetStudy(context.Background(), "NCT01234567")
	if err != nil {
		t.Fatal(err)
	}
	if versionChanged.Snapshot.Fingerprint == first.Snapshot.Fingerprint {
		t.Fatal("API version change must change the source fingerprint")
	}

	apiVersion = "2.0.4"
	studyBody = strings.Replace(clinicalTrialsGovStudyFixture, `"count": 240`, `"count": 241`, 1)
	dataChanged, err := client.GetStudy(context.Background(), "NCT01234567")
	if err != nil {
		t.Fatal(err)
	}
	if dataChanged.Snapshot.ContentHash == first.Snapshot.ContentHash || dataChanged.Snapshot.Fingerprint == first.Snapshot.Fingerprint {
		t.Fatal("normalized study change must change content hash and fingerprint")
	}
}

func TestClinicalTrialsGovRejectsNonNCTBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	client, err := newClinicalTrialsGovClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetStudy(context.Background(), "doi:10.1234/example")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorInvalidInput)
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestClinicalTrialsGovClassifiesUpstreamFailures(t *testing.T) {
	tests := []struct {
		name      string
		studyBody string
		status    int
		headers   map[string]string
		kind      ClinicalTrialsGovErrorKind
		check     func(*testing.T, *ClinicalTrialsGovError)
	}{
		{name: "not found", status: http.StatusNotFound, kind: ClinicalTrialsGovErrorNotFound},
		{name: "rate limited", status: http.StatusTooManyRequests, headers: map[string]string{"Retry-After": "999999"}, kind: ClinicalTrialsGovErrorRateLimited, check: func(t *testing.T, err *ClinicalTrialsGovError) {
			if err.RetryAfter <= 0 || err.RetryAfter > ClinicalTrialsGovMaxRetryAfter {
				t.Fatalf("RetryAfter = %v", err.RetryAfter)
			}
		}},
		{name: "oversized", studyBody: strings.Repeat("x", ClinicalTrialsGovMaxStudyBytes+1), kind: ClinicalTrialsGovErrorResponseTooLarge},
		{name: "malformed", studyBody: `{"protocolSection":`, kind: ClinicalTrialsGovErrorMalformedJSON},
		{name: "identifier mismatch", studyBody: strings.Replace(clinicalTrialsGovStudyFixture, "NCT01234567", "NCT87654321", 1), kind: ClinicalTrialsGovErrorIdentifierMismatch},
		{name: "missing required module", studyBody: `{"protocolSection":{"identificationModule":{"nctId":"NCT01234567"}},"hasResults":false}`, kind: ClinicalTrialsGovErrorSchemaInvalid},
		{name: "schema drift", studyBody: `{"protocolSection":[],"hasResults":false}`, kind: ClinicalTrialsGovErrorSchemaInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/version" {
					fmt.Fprint(w, clinicalTrialsGovVersionFixture)
					return
				}
				for key, value := range test.headers {
					w.Header().Set(key, value)
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
					return
				}
				fmt.Fprint(w, test.studyBody)
			}))
			defer server.Close()
			client, err := newClinicalTrialsGovClient(server.Client(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetStudy(context.Background(), "NCT01234567")
			classified := assertClinicalTrialsGovError(t, err, test.kind)
			if strings.Contains(classified.Error(), test.studyBody) && len(test.studyBody) > 32 {
				t.Fatal("error leaked upstream response body")
			}
			if test.check != nil {
				test.check(t, classified)
			}
		})
	}
}

func TestClinicalTrialsGovClassifiesContextCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := newClinicalTrialsGovClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GetStudy(canceled, "NCT01234567")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorCanceled)

	timed, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.GetStudy(timed, "NCT01234567")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorTimeout)
}

func TestClinicalTrialsGovClassifiesHTTPClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 10 * time.Millisecond
	client, err := newClinicalTrialsGovClient(httpClient, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetStudy(context.Background(), "NCT01234567")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorTimeout)
}

func TestClinicalTrialsGovAppliesInternalTimeoutToZeroTimeoutHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 0
	client, err := newClinicalTrialsGovClient(httpClient, server.URL, withClinicalTrialsGovRequestTimeout(15*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = client.GetStudy(context.Background(), "NCT01234567")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorTimeout)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("internal timeout took %v", elapsed)
	}
}

func TestClinicalTrialsGovProductionConstructorIsFixed(t *testing.T) {
	client := NewClinicalTrialsGovClient()
	if client.baseURL.Scheme != "https" || client.baseURL.Host != "clinicaltrials.gov" || client.baseURL.Path != "" {
		t.Fatalf("production base URL = %q", client.baseURL.String())
	}
	if client.requestTimeout <= 0 || client.requestTimeout > ClinicalTrialsGovMaxRequestTimeout {
		t.Fatalf("production request timeout = %v", client.requestTimeout)
	}
}

func TestClinicalTrialsGovRejectsResultsSchemaDriftAndBounds(t *testing.T) {
	protocol := extractProtocolSection(t, clinicalTrialsGovStudyFixture)
	validOutcome := `{"type":"PRIMARY","title":"Synthetic outcome","groups":[{"id":"FG000","title":"Arm"}],"classes":[{"categories":[{"measurements":[{"groupId":"FG000","value":"1"}]}]}]}`
	tests := []struct {
		name    string
		results string
	}{
		{name: "outcomes schema drift", results: `{"outcomeMeasuresModule":{"outcomeMeasures":"unexpected"}}`},
		{name: "too many outcomes", results: fmt.Sprintf(`{"outcomeMeasuresModule":{"outcomeMeasures":[%s]}}`, strings.Join(repeatClinicalTrialsGovFixture(validOutcome, ClinicalTrialsGovMaxReportedOutcomes+1), ","))},
		{name: "too many groups", results: fmt.Sprintf(`{"outcomeMeasuresModule":{"outcomeMeasures":[{"type":"PRIMARY","title":"Synthetic outcome","groups":[%s]}]}}`, strings.Join(repeatClinicalTrialsGovFixture(`{"id":"FG000","title":"Arm"}`, ClinicalTrialsGovMaxResultGroups+1), ","))},
		{name: "too many classes", results: fmt.Sprintf(`{"outcomeMeasuresModule":{"outcomeMeasures":[{"type":"PRIMARY","title":"Synthetic outcome","classes":[%s]}]}}`, strings.Join(repeatClinicalTrialsGovFixture(`{"title":"Class"}`, ClinicalTrialsGovMaxResultClasses+1), ","))},
		{name: "too many categories", results: fmt.Sprintf(`{"outcomeMeasuresModule":{"outcomeMeasures":[{"type":"PRIMARY","title":"Synthetic outcome","classes":[{"categories":[%s]}]}]}}`, strings.Join(repeatClinicalTrialsGovFixture(`{"title":"Category"}`, ClinicalTrialsGovMaxResultCategories+1), ","))},
		{name: "too many measurements", results: fmt.Sprintf(`{"outcomeMeasuresModule":{"outcomeMeasures":[{"type":"PRIMARY","title":"Synthetic outcome","classes":[{"categories":[{"measurements":[%s]}]}]}]}}`, strings.Join(repeatClinicalTrialsGovFixture(`{"groupId":"FG000","value":"1"}`, ClinicalTrialsGovMaxGroupMeasurements+1), ","))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"protocolSection":%s,"resultsSection":%s,"hasResults":true}`, protocol, test.results)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/version" {
					fmt.Fprint(w, clinicalTrialsGovVersionFixture)
					return
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			client, err := newClinicalTrialsGovClient(server.Client(), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetStudy(context.Background(), "NCT01234567")
			assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorSchemaInvalid)
		})
	}
}

func TestClinicalTrialsGovBlocksRedirectHostEscape(t *testing.T) {
	var escaped atomic.Bool
	escape := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Store(true) }))
	defer escape.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, escape.URL+r.URL.Path, http.StatusFound)
	}))
	defer origin.Close()

	client, err := newClinicalTrialsGovClient(origin.Client(), origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetStudy(context.Background(), "NCT01234567")
	assertClinicalTrialsGovError(t, err, ClinicalTrialsGovErrorUpstream)
	if escaped.Load() {
		t.Fatal("redirect escaped configured ClinicalTrials.gov host")
	}
}

func assertClinicalTrialsGovError(t *testing.T, err error, kind ClinicalTrialsGovErrorKind) *ClinicalTrialsGovError {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want kind %q", kind)
	}
	var classified *ClinicalTrialsGovError
	if !errors.As(err, &classified) {
		t.Fatalf("error type = %T, want *ClinicalTrialsGovError: %v", err, err)
	}
	if classified.Kind != kind {
		t.Fatalf("error kind = %q, want %q: %v", classified.Kind, kind, err)
	}
	return classified
}

func extractProtocolSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `"protocolSection":`)
	if start < 0 {
		t.Fatal("fixture lacks protocolSection")
	}
	start += len(`"protocolSection":`)
	end := strings.Index(body[start:], `,
  "resultsSection"`)
	if end < 0 {
		t.Fatal("fixture lacks resultsSection boundary")
	}
	return strings.TrimSpace(body[start : start+end])
}

func extractResultsSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `"resultsSection":`)
	if start < 0 {
		t.Fatal("fixture lacks resultsSection")
	}
	start += len(`"resultsSection":`)
	end := strings.Index(body[start:], `,
  "annotationSection"`)
	if end < 0 {
		t.Fatal("fixture lacks annotationSection boundary")
	}
	return strings.TrimSpace(body[start : start+end])
}

func repeatClinicalTrialsGovFixture(value string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = value
	}
	return values
}
