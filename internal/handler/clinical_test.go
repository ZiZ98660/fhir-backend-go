package handler

import (
	"context"
	"encoding/json"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"go-fhir-phase1/internal/service"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type clinicalMemory struct {
	data        map[string][]byte
	lastPatient string
	failure     error
	history     map[string][]repository.ResourceVersion
	deleted     map[string]bool
}

func (m *clinicalMemory) snapshot(id string, data []byte, method string) {
	if m.history == nil {
		m.history = make(map[string][]repository.ResourceVersion)
	}
	var header struct{ Meta internal.Meta }
	_ = json.Unmarshal(data, &header)
	version := header.Meta.VersionID
	if version == "" {
		version = "legacy"
	}
	for _, entry := range m.history[id] {
		if entry.VersionID == version {
			return
		}
	}
	m.history[id] = append(m.history[id], repository.ResourceVersion{VersionID: version, Data: append([]byte(nil), data...), Method: method, RecordedAt: time.Now().UTC()})
}

func (m *clinicalMemory) InitTable(context.Context) error { return nil }
func (m *clinicalMemory) save(id string, data []byte) error {
	if m.failure != nil {
		return m.failure
	}
	if _, ok := m.data[id]; ok {
		return internal.ErrConflict
	}
	m.data[id] = append([]byte(nil), data...)
	m.snapshot(id, data, "POST")
	return nil
}
func (m *clinicalMemory) SaveObservation(_ context.Context, o internal.Observation, data []byte) error {
	return m.save(o.ID, data)
}
func (m *clinicalMemory) SaveCondition(_ context.Context, c internal.Condition, data []byte) error {
	return m.save(c.ID, data)
}
func (m *clinicalMemory) search(patient string) ([][]byte, error) {
	m.lastPatient = patient
	if m.failure != nil {
		return nil, m.failure
	}
	var data [][]byte
	reference := patient
	if !strings.HasPrefix(reference, "Patient/") {
		reference = "Patient/" + patient
	}
	for id, value := range m.data {
		if m.deleted[id] {
			continue
		}
		var resource struct{ Subject internal.Reference }
		_ = json.Unmarshal(value, &resource)
		if resource.Subject.Reference != reference {
			continue
		}
		data = append(data, value)
	}
	return data, nil
}
func (m *clinicalMemory) GetObservationsByPatient(_ context.Context, patient string) ([][]byte, error) {
	return m.search(patient)
}
func (m *clinicalMemory) GetConditionsByPatient(_ context.Context, patient string) ([][]byte, error) {
	return m.search(patient)
}
func (m *clinicalMemory) get(id string) ([]byte, error) {
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

func (m *clinicalMemory) update(id string, data []byte, previous string) error {
	old, err := m.get(id)
	if err != nil {
		return err
	}
	var current struct{ Meta internal.Meta }
	if err := json.Unmarshal(old, &current); err != nil {
		return err
	}
	if current.Meta.VersionID != previous {
		return internal.ErrPrecondition
	}
	m.snapshot(id, old, "PUT")
	m.data[id] = append([]byte(nil), data...)
	m.snapshot(id, data, "PUT")
	return nil
}
func (m *clinicalMemory) remove(id, expected string) error {
	if m.failure != nil {
		return m.failure
	}
	data, ok := m.data[id]
	if !ok {
		return internal.ErrNotFound
	}
	if m.deleted[id] {
		return nil
	}
	var resource struct{ Meta internal.Meta }
	if err := json.Unmarshal(data, &resource); err != nil {
		return err
	}
	if expected != "" && expected != resource.Meta.VersionID {
		return internal.ErrPrecondition
	}
	m.snapshot(id, data, "PUT")
	n, _ := strconv.Atoi(resource.Meta.VersionID)
	if m.deleted == nil {
		m.deleted = make(map[string]bool)
	}
	m.deleted[id] = true
	m.history[id] = append(m.history[id], repository.ResourceVersion{VersionID: strconv.Itoa(n + 1), Method: "DELETE", RecordedAt: time.Now().UTC()})
	return nil
}
func (m *clinicalMemory) versions(id string) ([]repository.ResourceVersion, error) {
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
func (m *clinicalMemory) version(id, version string) (repository.ResourceVersion, error) {
	entries, err := m.versions(id)
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
func (m *clinicalMemory) UpdateObservation(_ context.Context, o internal.Observation, data []byte, v string) error {
	return m.update(o.ID, data, v)
}
func (m *clinicalMemory) UpdateCondition(_ context.Context, c internal.Condition, data []byte, v string) error {
	return m.update(c.ID, data, v)
}
func (m *clinicalMemory) DeleteObservation(_ context.Context, id, v string) error {
	return m.remove(id, v)
}
func (m *clinicalMemory) DeleteCondition(_ context.Context, id, v string) error {
	return m.remove(id, v)
}
func (m *clinicalMemory) GetObservationHistory(_ context.Context, id string) ([]repository.ResourceVersion, error) {
	return m.versions(id)
}
func (m *clinicalMemory) GetConditionHistory(_ context.Context, id string) ([]repository.ResourceVersion, error) {
	return m.versions(id)
}
func (m *clinicalMemory) GetObservationVersion(_ context.Context, id, v string) (repository.ResourceVersion, error) {
	return m.version(id, v)
}
func (m *clinicalMemory) GetConditionVersion(_ context.Context, id, v string) (repository.ResourceVersion, error) {
	return m.version(id, v)
}
func (m *clinicalMemory) GetObservationByID(_ context.Context, id string) ([]byte, error) {
	return m.get(id)
}
func (m *clinicalMemory) GetConditionByID(_ context.Context, id string) ([]byte, error) {
	return m.get(id)
}

func TestClinicalCreateSearchAndRead(t *testing.T) {
	for _, kind := range []string{"Observation", "Condition"} {
		t.Run(kind, func(t *testing.T) {
			store := &clinicalMemory{data: make(map[string][]byte)}
			var h http.HandlerFunc
			if kind == "Observation" {
				h = NewObservationHandler(service.NewObservationService(store)).HandleObservation
			} else {
				h = NewConditionHandler(service.NewConditionService(store)).HandleCondition
			}
			body := `{"resourceType":"` + kind + `","subject":{"reference":"Patient/p1"},"code":{"coding":[{"system":"urn:test","code":"test"}]}`
			if kind == "Observation" {
				body += `,"status":"final","valueQuantity":{"value":0,"unit":"test-unit"}`
			}
			body += `}`
			base := "http://example.org/api/v1/" + kind
			for i := 0; i < 2; i++ {
				w := call(t, h, "POST", base, body, "application/fhir+json", "")
				assertStatus(t, w, 201)
				if kind == "Observation" && !strings.Contains(w.Body.String(), `"value":0`) {
					t.Fatal("zero measurement was lost")
				}
				if w.Header().Get("ETag") != `W/"1"` {
					t.Fatal("missing version")
				}
				assertStatus(t, call(t, h, "GET", w.Header().Get("Location"), "", "", ""), 200)
			}
			if len(store.data) != 2 {
				t.Fatalf("expected two distinct resources, got %d", len(store.data))
			}
			for _, patient := range []string{"p1", "Patient/p1"} {
				w := call(t, h, "GET", base+"?patient="+patient, "", "", "")
				assertStatus(t, w, 200)
				var bundle internal.Bundle
				if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
					t.Fatal(err)
				}
				if bundle.Type != "searchset" || bundle.Total == nil || *bundle.Total != 2 {
					t.Fatalf("bad bundle: %s", w.Body.String())
				}
				for _, entry := range bundle.Entry {
					assertStatus(t, call(t, h, "GET", entry.FullUrl, "", "", ""), 200)
				}
				want := "p1"
				if kind == "Condition" {
					want = "Patient/p1"
				}
				if store.lastPatient != want {
					t.Fatalf("reference was not normalized: %q", store.lastPatient)
				}
			}
			assertStatus(t, call(t, h, "GET", base+"/missing", "", "", ""), 404)
			assertStatus(t, call(t, h, "GET", base, "", "", ""), 400)
			assertStatus(t, call(t, h, "POST", base, strings.Replace(body, "Patient/p1", "P", 1), "application/json", ""), 400)
			store.failure = internal.Invalid("referenced patient does not exist")
			assertStatus(t, call(t, h, "POST", base, body, "application/json", ""), 400)
		})
	}
}

func TestClinicalUpdatesSoftDeletionAndHistory(t *testing.T) {
	for _, kind := range []string{"Observation", "Condition"} {
		t.Run(kind, func(t *testing.T) {
			store := &clinicalMemory{data: make(map[string][]byte)}
			var h http.HandlerFunc
			if kind == "Observation" {
				h = NewObservationHandler(service.NewObservationService(store)).HandleObservation
			} else {
				h = NewConditionHandler(service.NewConditionService(store)).HandleCondition
			}
			base := "http://example.org/api/v1/" + kind
			document := map[string]any{"resourceType": kind, "subject": map[string]string{"reference": "Patient/p1"}, "code": map[string]string{"text": "Original"}}
			if kind == "Observation" {
				document["status"] = "final"
			}
			body, _ := json.Marshal(document)
			created := call(t, h, "POST", base, string(body), "application/json", "")
			assertStatus(t, created, 201)
			var original struct{ ID string }
			if err := json.Unmarshal(created.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
			url := created.Header().Get("Location")
			document["id"] = original.ID
			document["code"] = map[string]string{"text": "Replacement"}
			body, _ = json.Marshal(document)
			updated := call(t, h, "PUT", url, string(body), "application/json", `W/"1"`)
			assertStatus(t, updated, 200)
			if updated.Header().Get("ETag") != `W/"2"` {
				t.Fatalf("missing update version: %s", updated.Body.String())
			}
			assertStatus(t, call(t, h, "PUT", url, string(body), "application/json", `W/"1"`), 412)
			assertStatus(t, call(t, h, "PATCH", url, `[{"op":"replace","path":"/code/text","value":"Patched"}]`, "application/json-patch+json", `W/"2"`), 200)
			assertStatus(t, call(t, h, "PATCH", url, `[{"op":"replace","path":"/subject/reference","value":"Patient/p2"}]`, "application/json-patch+json", ""), 400)
			document["subject"] = map[string]string{"reference": "Patient/p2"}
			body, _ = json.Marshal(document)
			assertStatus(t, call(t, h, "PUT", url, string(body), "application/json", ""), 400)
			document["subject"] = map[string]string{"reference": "Patient/p1"}
			body, _ = json.Marshal(document)
			assertStatus(t, call(t, h, "PATCH", url, `null`, "application/json-patch+json", ""), 400)
			assertStatus(t, call(t, h, "DELETE", url, "", "", `W/"2"`), 412)
			assertStatus(t, call(t, h, "DELETE", url, "", "", `W/"3"`), 204)
			assertStatus(t, call(t, h, "DELETE", url, "", "", ""), 204)
			assertStatus(t, call(t, h, "GET", url, "", "", ""), 410)
			assertStatus(t, call(t, h, "PUT", url, string(body), "application/json", ""), 410)
			assertStatus(t, call(t, h, "PATCH", url, `[]`, "application/json-patch+json", ""), 410)
			if _, ok := store.data[original.ID]; !ok {
				t.Fatal("soft deletion removed data")
			}
			w := call(t, h, "GET", base+"?patient=p1", "", "", "")
			assertStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"total":0`) {
				t.Fatalf("deleted record appears in search: %s", w.Body.String())
			}
			historyURL := url + "/_history"
			w = call(t, h, "GET", historyURL, "", "", "")
			assertStatus(t, w, 200)
			var bundle internal.Bundle
			if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
				t.Fatal(err)
			}
			if bundle.Type != "history" || bundle.Total == nil || *bundle.Total != 4 || len(bundle.Entry) != 4 {
				t.Fatalf("wrong history: %s", w.Body.String())
			}
			for i, entry := range bundle.Entry {
				if entry.Request == nil || entry.Response == nil || entry.FullUrl != url || entry.Response.ETag != "W/\""+strconv.Itoa(4-i)+"\"" {
					t.Fatalf("invalid entry: %+v", entry)
				}
			}
			if bundle.Entry[0].Resource != nil || bundle.Entry[0].Request.Method != "DELETE" {
				t.Fatal("invalid deletion marker")
			}
			w = call(t, h, "GET", historyURL+"/1", "", "", "")
			assertStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), `"text":"Original"`) || w.Header().Get("ETag") != `W/"1"` {
				t.Fatalf("old snapshot changed: %s", w.Body.String())
			}
			assertStatus(t, call(t, h, "GET", historyURL+"/2", "", "", ""), 200)
			assertStatus(t, call(t, h, "GET", historyURL+"/3", "", "", ""), 200)
			assertStatus(t, call(t, h, "GET", historyURL+"/4", "", "", ""), 410)
			assertStatus(t, call(t, h, "GET", historyURL+"/999", "", "", ""), 404)
			assertStatus(t, call(t, h, "GET", base+"/missing/_history", "", "", ""), 404)
			assertStatus(t, call(t, h, "GET", historyURL+"?id=other", "", "", ""), 400)
			assertStatus(t, call(t, h, "GET", historyURL+"?_count=1", "", "", ""), 400)
			assertStatus(t, call(t, h, "GET", historyURL+"/1/extra", "", "", ""), 400)
			assertStatus(t, call(t, h, "POST", historyURL, `{}`, "application/json", ""), 405)
			assertStatus(t, call(t, h, "DELETE", historyURL+"/1", "", "", ""), 405)
		})
	}
}

type pinger struct {
	err      error
	called   bool
	deadline bool
}

func (p *pinger) Ping(ctx context.Context) error {
	p.called = true
	_, p.deadline = ctx.Deadline()
	return p.err
}

func TestHealthActuallyPingsDatabase(t *testing.T) {
	for _, failure := range []error{nil, context.DeadlineExceeded} {
		p := &pinger{err: failure}
		w := call(t, Health(p), "GET", "/health", "", "", "")
		want := 200
		if failure != nil {
			want = 503
		}
		if w.Code != want || !p.called || !p.deadline {
			t.Fatalf("health status=%d ping=%v bounded=%v", w.Code, p.called, p.deadline)
		}
	}
}
