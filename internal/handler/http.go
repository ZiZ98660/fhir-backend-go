package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-fhir-phase1/internal"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
)

// LEARNING COMPARISONS
// Read patient_handler.go alongside this file: that file calls these helpers;
// this file contains the implementations. Both belong to package handler, so
// no import between the files is necessary. These are not extra request layers.
//
// BEFORE blocks below are inactive excerpts, not replacement implementations.
// They may refer to removed methods or omit surrounding code. Keep them commented.
// NOW/WHY explain the active function immediately below each comparison.
// Moving code into a helper was OPTIONAL. Fixing incorrect behavior was the
// requirement; the same checks could have stayed directly inside each handler.

// 1 << 20 means 1,048,576 bytes (1 MiB). The earlier decoder had no body limit.
const maxBodySize = 1 << 20

// BEFORE (Patient GET, simplified excerpt):
//
// patient, err := h.service.GetPatientByID(r.Context(), patientID)
//
//	if err != nil {
//	    h.sendOutcome(w, http.StatusNotFound, "error", "patient not found")
//	    return
//	}
//
// NOW: inspect the error category before choosing an HTTP response.
// WHY: a database outage is not a missing patient; it should not become 404.
// errors.Is recognizes categories even when extra context wraps the error.
// errors.As extracts a particular error type, such as *http.MaxBytesError.
// OPTIONAL ABSTRACTION: Patient, Observation, and Condition could each contain
// this switch. Sharing it prevents their error responses from drifting apart.
func sendError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, internal.ErrInvalid):
		internal.WriteOutcome(w, http.StatusBadRequest, "invalid", err.Error())
	case errors.Is(err, internal.ErrNotFound):
		internal.WriteOutcome(w, http.StatusNotFound, "not-found", "resource not found")
	case errors.Is(err, internal.ErrGone):
		internal.WriteOutcome(w, http.StatusGone, "deleted", "resource deleted; previous versions are available through history")
	case errors.Is(err, internal.ErrConflict):
		internal.WriteOutcome(w, http.StatusConflict, "conflict", "resource conflict")
	case errors.Is(err, internal.ErrPrecondition):
		internal.WriteOutcome(w, http.StatusPreconditionFailed, "conflict", "resource version has changed; read it again before updating")
	case errors.Is(err, internal.ErrUnsupportedMedia):
		internal.WriteOutcome(w, http.StatusUnsupportedMediaType, "not-supported", err.Error())
	case errors.As(err, &tooLarge):
		internal.WriteOutcome(w, http.StatusRequestEntityTooLarge, "too-long", "request body exceeds 1 MiB")
	case errors.Is(err, context.DeadlineExceeded):
		internal.WriteOutcome(w, http.StatusGatewayTimeout, "timeout", "request timed out")
	case errors.Is(err, context.Canceled):
		internal.WriteOutcome(w, http.StatusRequestTimeout, "timeout", "request canceled")
	default:
		log.Printf("request failed: %v", err)
		internal.WriteOutcome(w, http.StatusInternalServerError, "exception", "internal server error")
	}
}

// BEFORE (request reading was part of the decoder call):
//
// decoder := json.NewDecoder(r.Body)
// err := decoder.Decode(&p)
//
// NOW: first check the media type, then read at most maxBodySize bytes.
// WHY: resource JSON and JSON Patch have different supported content types,
// and an unlimited body could consume excessive memory. The limit/checks are
// new behavior; extracting them into readBody is only a reuse choice.
// allowed ...string accepts several strings; inside the function it is a slice.
// mime.ParseMediaType separates "application/json" from parameters such as charset.
// MaxBytesReader reports an error on oversized input instead of silently
// truncating it. io.ReadAll returns the accepted bytes or that read error.
func readBody(w http.ResponseWriter, r *http.Request, allowed ...string) ([]byte, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	valid := false
	for _, value := range allowed {
		if media == value {
			valid = true
		}
	}
	if err != nil || !valid {
		return nil, fmt.Errorf("%w: expected %s", internal.ErrUnsupportedMedia, strings.Join(allowed, " or "))
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
}

// BEFORE (inside the Patient handler):
//
// var p internal.Patient
// decoder := json.NewDecoder(r.Body)
//
//	if err := decoder.Decode(&p); err != nil {
//	    h.sendOutcome(w, http.StatusBadRequest, "error", "invalid JSON")
//	    return
//	}
//
// NOW: decodeResource[internal.Patient](w, r) returns a Patient and an error.
// T stands for the selected type; "any" places no method requirement on T.
// var zero T gives us the empty value to return when something goes wrong.
// var resource *T starts nil. Decode(&resource) lets the JSON decoder populate
// it for an object while leaving it nil for JSON null; the nil check rejects null.
// The second Decode must reach io.EOF: a second document or trailing garbage fails.
// WHY: the earlier decoder accepted unknown fields and did not check for an
// extra document. The new checks prevent silent loss or partial acceptance.
// OPTIONAL ABSTRACTION: separate decodePatient/decodeObservation functions could
// do exactly the same checks. Generics share the algorithm, not add correctness
// by themselves. Validate() is called later in the service, not in this helper.
func decodeResource[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	data, err := readBody(w, r, "application/fhir+json", "application/json")
	if err != nil {
		return zero, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Reject unsupported fields so a replacement cannot silently discard them.
	decoder.DisallowUnknownFields()
	var resource *T
	if err := decoder.Decode(&resource); err != nil {
		return zero, internal.Invalid("invalid or unsupported JSON: %v", err)
	}
	if resource == nil {
		return zero, internal.Invalid("a resource object is required")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return zero, internal.Invalid("exactly one JSON resource is required")
	}
	return *resource, nil
}

// BEFORE:
//
// patientID := r.URL.Query().Get("id")
//
// NOW: accept /Patient/p1 as well as the older /Patient?id=p1 form.
// WHY: instance URLs in responses need to be readable, and two conflicting IDs
// must not cause us to select a patient unpredictably. An empty ID with no error
// means "collection request"; it does not mean the patient is missing.
// OPTIONAL ABSTRACTION: this path parsing could be repeated per resource handler.
func requestID(r *http.Request, resourceType string) (string, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/"+resourceType)
	id := ""
	if path != "" {
		id = strings.TrimPrefix(path, "/")
		if !internal.ValidID(id) {
			return "", internal.Invalid("invalid resource path")
		}
	}
	queryID := r.URL.Query().Get("id")
	if id != "" && queryID != "" && id != queryID {
		return "", internal.Invalid("path and query ids disagree")
	}
	if id == "" {
		id = queryID
	}
	if id != "" && !internal.ValidID(id) {
		return "", internal.Invalid("invalid resource id")
	}
	return id, nil
}

// BEFORE: there was no If-Match parser or version-aware update behavior.
//
// NOW: If-Match: W/"3" becomes the version string "3".
// WHY: the caller can say "update only the version I edited". This helper only
// parses the header; PatientService compares versions and the repository guards
// the actual write. An absent header returns "", nil, so the header is optional.
// This function supports one weak ETag, not every possible HTTP If-Match form.
func ifMatch(r *http.Request) (string, error) {
	value := r.Header.Get("If-Match")
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "W/\"") || !strings.HasSuffix(value, "\"") {
		return "", internal.Invalid("If-Match must be a single weak ETag, for example W/\"1\"")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(value, "W/\""), "\"")
	if !internal.ValidID(version) {
		return "", internal.Invalid("invalid If-Match version")
	}
	return version, nil
}

// BEFORE (constructed inside each service's search loop):
//
// fullURL := "http://localhost:8080/api/v1/Patient/" + p.ID
//
// NOW: use the request host and direct TLS state to construct the instance URL.
// WHY: a deployed server should not always advertise localhost. URL construction
// stays in the HTTP package because it depends on the request, not the database.
// A reverse proxy still needs suitable configuration; forwarded headers are not
// interpreted here. Sharing this short function is an optional organization choice.
func resourceURL(r *http.Request, resourceType, id string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v1/" + resourceType + "/" + id
}

// BEFORE (the content type was set elsewhere in each handler):
//
// w.WriteHeader(http.StatusCreated)
// json.NewEncoder(w).Encode(createdPatient)
//
// NOW: set content type, available ETag/Last-Modified, and creation Location,
// then write the status and body. Headers must be set before WriteHeader.
// WHY: clients can locate a created resource and use its version on later writes.
// OPTIONAL ABSTRACTION: the old direct Encode call still works for a basic API;
// the extra headers could also be set explicitly in each resource handler.
// "any" lets this writer accept different resource values; validation already
// happened upstream. It does not make this function a resource validator.
func writeResource(w http.ResponseWriter, r *http.Request, status int, resource any, resourceType, id string, meta internal.Meta) {
	w.Header().Set("Content-Type", "application/fhir+json")
	if meta.VersionID != "" {
		w.Header().Set("ETag", "W/\""+meta.VersionID+"\"")
	}
	if updated, err := time.Parse(time.RFC3339Nano, meta.LastUpdated); err == nil {
		w.Header().Set("Last-Modified", updated.UTC().Format(http.TimeFormat))
	}
	if status == http.StatusCreated {
		w.Header().Set("Location", resourceURL(r, resourceType, id))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resource)
}

// BEFORE:
//
// json.NewEncoder(w).Encode(bundle)
//
// NOW: fill each entry's URL using this request before encoding the Bundle.
// The type switch asks which concrete resource is held in Resource, so it can
// access that resource's ID. The index i lets us update the actual slice entry.
// WHY: the generic service helper builds results without depending on HTTP;
// the handler adds the HTTP-specific URLs. A direct per-resource loop is also valid.
func writeBundle(w http.ResponseWriter, r *http.Request, bundle internal.Bundle) {
	for i := range bundle.Entry {
		switch value := bundle.Entry[i].Resource.(type) {
		case internal.Patient:
			bundle.Entry[i].FullUrl = resourceURL(r, "Patient", value.ID)
		case internal.Observation:
			bundle.Entry[i].FullUrl = resourceURL(r, "Observation", value.ID)
		case internal.Condition:
			bundle.Entry[i].FullUrl = resourceURL(r, "Condition", value.ID)
		}
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(bundle)
}

// BEFORE:
//
// h.sendOutcome(w, http.StatusMethodNotAllowed, "error", "Method not allowed")
//
// NOW: include Allow as well as a structured error response.
// WHY: the client receives both the rejection and the advertised permitted methods.
// OPTIONAL ABSTRACTION: three short explicit statements per handler would work too.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	internal.WriteOutcome(w, http.StatusMethodNotAllowed, "not-supported", "method not allowed for this resource path")
}
