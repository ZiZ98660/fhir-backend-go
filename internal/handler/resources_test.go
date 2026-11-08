package handler

import (
	"context"
	"encoding/json"
	"errors"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"go-fhir-phase1/internal/service"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type patientMemory struct {
	data          map[string][]byte
	failure       error
	updateFailure error
	saves         int
	history       map[string][]repository.ResourceVersion
	deleted       map[string]bool
}

func (m *patientMemory) snapshot(p internal.Patient, data []byte, method string) {
	if m.history == nil {
		m.history = make(map[string][]repository.ResourceVersion)
	}
	version := p.Meta.VersionID
	if version == "" {
		version = "legacy"
	}
	for _, entry := range m.history[p.ID] {
		if entry.VersionID == version {
			return
		}
	}
	m.history[p.ID] = append(m.history[p.ID], repository.ResourceVersion{VersionID: version, Data: append([]byte(nil), data...), Method: method, RecordedAt: time.Now().UTC()})
}

func (m *patientMemory) InitTable(context.Context) error { return nil }
func (m *patientMemory) SavePatient(_ context.Context, p internal.Patient, data []byte) error {
	if m.failure != nil {
		return m.failure
	}
	if _, ok := m.data[p.ID]; ok {
		return internal.ErrConflict
	}
	m.data[p.ID] = append([]byte(nil), data...)
	m.snapshot(p, data, "POST")
	m.saves++
	return nil
}
func (m *patientMemory) UpdatePatient(_ context.Context, p internal.Patient, data []byte, version string) error {
	if m.updateFailure != nil {
		return m.updateFailure
	}
	if m.failure != nil {
		return m.failure
	}
	if m.deleted[p.ID] {
		return internal.ErrGone
	}
	var current internal.Patient
	if err := json.Unmarshal(m.data[p.ID], &current); err != nil {
		return err
	}
	if current.Meta.VersionID != version {
		return internal.ErrPrecondition
	}
	m.snapshot(current, m.data[p.ID], "PUT")
	m.data[p.ID] = append([]byte(nil), data...)
	m.snapshot(p, data, "PUT")
	return nil
}
func (m *patientMemory) SearchPatients(context.Context, string, string) ([][]byte, error) {
	if m.failure != nil {
		return nil, m.failure
	}
	var data [][]byte
	for id, value := range m.data {
		if m.deleted[id] {
			continue
		}
		data = append(data, value)
	}
	return data, nil
}
func (m *patientMemory) GetPatientByID(_ context.Context, id string) ([]byte, error) {
	if m.failure != nil {
		return nil, m.failure
	}
	data, ok := m.data[id]
	if !ok {
		return nil, internal.ErrNotFound
	}
	if m.deleted[id] {
		return nil, internal.ErrGone
	}
	return data, nil
}
func (m *patientMemory) DeletePatient(_ context.Context, id, expectedVersion string) error {
	if m.failure != nil {
		return m.failure
	}
	if _, ok := m.data[id]; !ok {
		return internal.ErrNotFound
	}
	if m.deleted[id] {
		return nil
	}
	var p internal.Patient
	if err := json.Unmarshal(m.data[id], &p); err != nil {
		return err
	}
	if expectedVersion != "" && expectedVersion != p.Meta.VersionID {
		return internal.ErrPrecondition
	}
	m.snapshot(p, m.data[id], "PUT")
	n := 0
	if p.Meta.VersionID != "" {
		n, _ = strconv.Atoi(p.Meta.VersionID)
	}
	if m.deleted == nil {
		m.deleted = make(map[string]bool)
	}
	m.deleted[id] = true
	m.history[id] = append(m.history[id], repository.ResourceVersion{VersionID: strconv.Itoa(n + 1), Method: "DELETE", RecordedAt: time.Now().UTC()})
	return nil
}

func (m *patientMemory) GetPatientHistory(_ context.Context, id string) ([]repository.ResourceVersion, error) {
	if m.failure != nil {
		return nil, m.failure
	}
	if _, ok := m.data[id]; !ok {
		return nil, internal.ErrNotFound
	}
	var entries []repository.ResourceVersion
	for i := len(m.history[id]) - 1; i >= 0; i-- {
		entries = append(entries, m.history[id][i])
	}
	return entries, nil
}

func (m *patientMemory) GetPatientVersion(ctx context.Context, id, version string) (repository.ResourceVersion, error) {
	entries, err := m.GetPatientHistory(ctx, id)
	if err != nil {
		return repository.ResourceVersion{}, err
	}
	for _, entry := range entries {
		if entry.VersionID == version {
			return entry, nil
		}
	}
	return repository.ResourceVersion{}, internal.ErrNotFound
}

func call(t *testing.T, fn http.HandlerFunc, method, target, body, media, version string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	if version != "" {
		r.Header.Set("If-Match", version)
	}
	w := httptest.NewRecorder()
	fn(w, r)
	return w
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	if status >= 400 {
		var outcome internal.OperationOutcome
		if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
			t.Fatal(err)
		}
		if outcome.ResourceType != "OperationOutcome" || len(outcome.Issue) == 0 || outcome.Issue[0].Code == "" || outcome.Issue[0].Severity == "" {
			t.Fatalf("incomplete error: %s", w.Body.String())
		}
	}
}

func patientFrom(t *testing.T, w *httptest.ResponseRecorder) internal.Patient {
	t.Helper()
	var p internal.Patient
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPatientCreateReadReplacePatch(t *testing.T) {
	store := &patientMemory{data: make(map[string][]byte)}
	h := NewPatientHandler(service.NewPatientService(store)).HandlePatient
	w := call(t, h, "POST", "http://example.org/api/v1/Patient", `{"resourceType":"Patient","active":true,"name":[{"family":"Original"}]}`, "application/fhir+json", "")
	assertStatus(t, w, 201)
	p := patientFrom(t, w)
	url := w.Header().Get("Location")
	if !internal.ValidID(p.ID) || p.Meta.VersionID != "1" || url != "http://example.org/api/v1/Patient/"+p.ID {
		t.Fatalf("invalid create response: %s", w.Body.String())
	}
	assertStatus(t, call(t, h, "GET", url, "", "", ""), 200)
	assertStatus(t, call(t, h, "GET", "/api/v1/Patient?id="+p.ID, "", "", ""), 200)
	replacement := `{"resourceType":"Patient","id":"` + p.ID + `","active":false}`
	w = call(t, h, "PUT", url, replacement, "application/fhir+json", `W/"1"`)
	assertStatus(t, w, 200)
	p = patientFrom(t, w)
	if p.Meta.VersionID != "2" || p.Active == nil || *p.Active || len(p.Name) != 0 || w.Header().Get("ETag") != `W/"2"` {
		t.Fatalf("replacement failed: %s", w.Body.String())
	}
	assertStatus(t, call(t, h, "PUT", url, replacement, "application/fhir+json", `W/"1"`), 412)
	w = call(t, h, "PATCH", url, `[{"op":"add","path":"/gender","value":"other"},{"op":"replace","path":"/active","value":true}]`, "application/json-patch+json", `W/"2"`)
	assertStatus(t, w, 200)
	p = patientFrom(t, w)
	if p.Gender != "other" || p.Meta.VersionID != "3" || p.Active == nil || !*p.Active || store.saves != 1 {
		t.Fatalf("patch created or lost resource: %s saves=%d", w.Body.String(), store.saves)
	}
	assertStatus(t, call(t, h, "PATCH", url, `[{"op":"replace","path":"/active","value":false}]`, "application/json-patch+json", `W/"2"`), 412)
	if len(store.data) != 1 {
		t.Fatalf("updates created %d patients", len(store.data))
	}
	w = call(t, h, "GET", "http://example.org/api/v1/Patient", "", "", "")
	assertStatus(t, w, 200)
	var bundle internal.Bundle
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Type != "searchset" || bundle.Total == nil || *bundle.Total != 1 || bundle.Entry[0].FullUrl != url {
		t.Fatalf("invalid bundle: %s", w.Body.String())
	}
	assertStatus(t, call(t, h, "DELETE", url, "", "", ""), 204)
	assertStatus(t, call(t, h, "GET", url, "", "", ""), 410)
	w = call(t, h, "GET", "/api/v1/Patient", "", "", "")
	if !strings.Contains(w.Body.String(), `"total":0`) || strings.Contains(w.Body.String(), `"entry"`) {
		t.Fatalf("invalid empty bundle: %s", w.Body.String())
	}
}

func TestPatientRejectsInvalidRequests(t *testing.T) {
	store := &patientMemory{data: map[string][]byte{"p1": []byte(`{"resourceType":"Patient","id":"p1","active":true,"meta":{"versionId":"1"}}`)}}
	h := NewPatientHandler(service.NewPatientService(store)).HandlePatient
	for _, test := range []struct {
		name, method, path, body, media, version string
		status                                   int
	}{
		{"null", "POST", "/api/v1/Patient", `null`, "application/json", "", 400},
		{"unknown field", "POST", "/api/v1/Patient", `{"resourceType":"Patient","unsupported":true}`, "application/json", "", 400},
		{"legacy extension value", "POST", "/api/v1/Patient", `{"resourceType":"Patient","extension":[{"url":"urn:test","value":"x"}]}`, "application/json", "", 400},
		{"multiple documents", "POST", "/api/v1/Patient", `{"resourceType":"Patient"} {}`, "application/json", "", 400},
		{"wrong resource", "POST", "/api/v1/Patient", `{"resourceType":"Condition"}`, "application/json", "", 400},
		{"too large", "POST", "/api/v1/Patient", strings.Repeat(" ", maxBodySize+1), "application/json", "", 413},
		{"missing media", "POST", "/api/v1/Patient", `{}`, "", "", 415},
		{"missing target", "PUT", "/api/v1/Patient", `{"resourceType":"Patient","id":"p1"}`, "application/json", "", 400},
		{"different id", "PUT", "/api/v1/Patient/p1", `{"resourceType":"Patient","id":"p2"}`, "application/json", "", 400},
		{"missing resource", "PUT", "/api/v1/Patient/missing", `{"resourceType":"Patient","id":"missing"}`, "application/json", "", 404},
		{"bad etag", "PUT", "/api/v1/Patient/p1", `{"resourceType":"Patient","id":"p1"}`, "application/json", `"1"`, 400},
		{"patch resource body", "PATCH", "/api/v1/Patient/p1", `{"resourceType":"Patient"}`, "application/json-patch+json", "", 400},
		{"patch null", "PATCH", "/api/v1/Patient/p1", `null`, "application/json-patch+json", "", 400},
		{"patch wrong media", "PATCH", "/api/v1/Patient/p1", `[]`, "application/json", "", 415},
		{"patch changes id", "PATCH", "/api/v1/Patient/p1", `[{"op":"replace","path":"/id","value":"p2"}]`, "application/json-patch+json", "", 400},
		{"patch failed test", "PATCH", "/api/v1/Patient/p1", `[{"op":"test","path":"/active","value":false}]`, "application/json-patch+json", "", 400},
		{"patch unknown field", "PATCH", "/api/v1/Patient/p1", `[{"op":"add","path":"/unknown","value":true}]`, "application/json-patch+json", "", 400},
		{"path query conflict", "GET", "/api/v1/Patient/p1?id=p2", "", "", "", 400},
		{"post instance", "POST", "/api/v1/Patient/p1", `{}`, "application/json", "", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertStatus(t, call(t, h, test.method, test.path, test.body, test.media, test.version), test.status)
		})
	}
	if len(store.data) != 1 || store.saves != 0 {
		t.Fatalf("invalid request wrote data")
	}
}

func TestStorageErrorsAndConcurrentUpdate(t *testing.T) {
	store := &patientMemory{data: make(map[string][]byte), failure: errors.New("private database error")}
	h := NewPatientHandler(service.NewPatientService(store)).HandlePatient
	for _, request := range []struct{ method, path, body string }{
		{"GET", "/api/v1/Patient/p1", ""},
		{"GET", "/api/v1/Patient", ""},
		{"POST", "/api/v1/Patient", `{"resourceType":"Patient"}`},
	} {
		w := call(t, h, request.method, request.path, request.body, "application/json", "")
		assertStatus(t, w, 500)
		if strings.Contains(w.Body.String(), "private database") {
			t.Fatal("database details leaked")
		}
	}
	store.failure = nil
	store.data["p1"] = []byte(`{"resourceType":"Patient","id":"p1","meta":{"versionId":"1"}}`)
	store.updateFailure = internal.ErrPrecondition
	w := call(t, h, "PUT", "/api/v1/Patient/p1", `{"resourceType":"Patient","id":"p1","active":false}`, "application/json", `W/"1"`)
	assertStatus(t, w, 412)
	store.data["broken"] = []byte(`{`)
	assertStatus(t, call(t, h, "GET", "/api/v1/Patient", "", "", ""), 500)
}

func TestPatientHistoryAndSoftDeletion(t *testing.T) {
	store := &patientMemory{data: make(map[string][]byte)}
	h := NewPatientHandler(service.NewPatientService(store)).HandlePatient
	created := call(t, h, "POST", "http://example.org/api/v1/Patient", `{"resourceType":"Patient","name":[{"family":"Original"}],"active":true}`, "application/json", "")
	assertStatus(t, created, 201)
	p := patientFrom(t, created)
	url := created.Header().Get("Location")
	historyURL := url + "/_history"
	assertStatus(t, call(t, h, "PUT", url, `{"resourceType":"Patient","id":"`+p.ID+`","active":false}`, "application/json", `W/"1"`), 200)
	assertStatus(t, call(t, h, "PATCH", url, `[{"op":"add","path":"/gender","value":"other"}]`, "application/json-patch+json", `W/"2"`), 200)
	// Failed writes must not add a history entry.
	assertStatus(t, call(t, h, "PUT", url, `{"resourceType":"Patient","id":"`+p.ID+`"}`, "application/json", `W/"1"`), 412)
	assertStatus(t, call(t, h, "DELETE", url, "", "", `W/"2"`), 412)
	assertStatus(t, call(t, h, "DELETE", url, "", "", `W/"3"`), 204)
	assertStatus(t, call(t, h, "DELETE", url, "", "", ""), 204)
	assertStatus(t, call(t, h, "GET", url, "", "", ""), 410)
	assertStatus(t, call(t, h, "GET", "/api/v1/Patient?id="+p.ID, "", "", ""), 410)
	assertStatus(t, call(t, h, "PUT", url, `{"resourceType":"Patient","id":"`+p.ID+`"}`, "application/json", ""), 410)
	assertStatus(t, call(t, h, "PATCH", url, `[]`, "application/json-patch+json", ""), 410)
	if _, ok := store.data[p.ID]; !ok {
		t.Fatal("soft deletion removed the stored document")
	}
	w := call(t, h, "GET", historyURL, "", "", "")
	assertStatus(t, w, 200)
	var bundle struct {
		ResourceType string `json:"resourceType"`
		Type         string `json:"type"`
		Total        int    `json:"total"`
		Entry        []struct {
			FullURL  string                  `json:"fullUrl"`
			Resource json.RawMessage         `json:"resource"`
			Request  internal.BundleRequest  `json:"request"`
			Response internal.BundleResponse `json:"response"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Type != "history" || bundle.Total != 4 || len(bundle.Entry) != 4 {
		t.Fatalf("invalid timeline: %s", w.Body.String())
	}
	for i, entry := range bundle.Entry {
		if entry.FullURL != url || entry.Response.ETag != "W/\""+strconv.Itoa(4-i)+"\"" || entry.Response.LastModified == "" {
			t.Fatalf("invalid history entry: %+v", entry)
		}
		if i == 0 {
			if len(entry.Resource) != 0 || entry.Request.Method != "DELETE" || entry.Request.URL != "Patient/"+p.ID || entry.Response.Status != "204" {
				t.Fatalf("invalid deletion marker: %+v", entry)
			}
		} else {
			if len(entry.Resource) == 0 {
				t.Fatal("live version has no resource")
			}
			var snapshot internal.Patient
			if err := json.Unmarshal(entry.Resource, &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.ID != p.ID || snapshot.Meta.VersionID != strconv.Itoa(4-i) {
				t.Fatalf("wrong snapshot: %+v", snapshot)
			}
		}
	}
	if bundle.Entry[3].Request.Method != "POST" || bundle.Entry[3].Request.URL != "Patient" || bundle.Entry[3].Response.Status != "201" {
		t.Fatal("creation event was not retained")
	}
	w = call(t, h, "GET", historyURL+"/1", "", "", "")
	assertStatus(t, w, 200)
	original := patientFrom(t, w)
	if original.Meta.VersionID != "1" || len(original.Name) != 1 || original.Name[0].Family != "Original" || original.Active == nil || !*original.Active || w.Header().Get("ETag") != `W/"1"` {
		t.Fatalf("original changed: %s", w.Body.String())
	}
	w = call(t, h, "GET", historyURL+"/2", "", "", "")
	assertStatus(t, w, 200)
	second := patientFrom(t, w)
	if second.Active == nil || *second.Active || second.Gender != "" {
		t.Fatalf("version 2 changed: %s", w.Body.String())
	}
	assertStatus(t, call(t, h, "GET", historyURL+"/3", "", "", ""), 200)
	assertStatus(t, call(t, h, "GET", historyURL+"/4", "", "", ""), 410)
	assertStatus(t, call(t, h, "GET", historyURL+"/999", "", "", ""), 404)
	assertStatus(t, call(t, h, "GET", "/api/v1/Patient/missing/_history", "", "", ""), 404)
	assertStatus(t, call(t, h, "GET", "/api/v1/Patient/missing/_history/1", "", "", ""), 404)
	assertStatus(t, call(t, h, "GET", historyURL+"/bad_version", "", "", ""), 400)
	assertStatus(t, call(t, h, "GET", historyURL+"/1/extra", "", "", ""), 400)
	assertStatus(t, call(t, h, "GET", historyURL+"?id=other", "", "", ""), 400)
	assertStatus(t, call(t, h, "GET", historyURL+"?_count=1", "", "", ""), 400)
	assertStatus(t, call(t, h, "POST", historyURL, `{}`, "application/json", ""), 405)
	assertStatus(t, call(t, h, "DELETE", historyURL+"/1", "", "", ""), 405)
	w = call(t, h, "GET", "/api/v1/Patient", "", "", "")
	assertStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("deleted patient appears in search: %s", w.Body.String())
	}
	store.failure = errors.New("history storage unavailable")
	assertStatus(t, call(t, h, "GET", historyURL, "", "", ""), 500)
	assertStatus(t, call(t, h, "GET", historyURL+"/1", "", "", ""), 500)
}
