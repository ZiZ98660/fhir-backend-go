package service

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"io"
	"time"
)

// BEFORE (repeated in each creation method):
// p.Meta = internal.Meta{VersionID: "1", LastUpdated: time.Now().Format(time.RFC3339)}
// NOW: call newMeta(). UTC gives one timezone representation; RFC3339Nano keeps
// available subsecond precision. This timestamp is not a concurrency guarantee.
// OPTIONAL ABSTRACTION: the same struct literal could remain in each service.
func newMeta() internal.Meta {
	return internal.Meta{VersionID: "1", LastUpdated: time.Now().UTC().Format(time.RFC3339Nano)}
}

// BEFORE:
//
//	Patient:     fmt.Sprintf("pt-%d", time.Now().UnixNano())
//	Condition:   fmt.Sprintf("cond-%d", time.Now().UnixNano())
//	Observation: "obs-fhir-123"
//
// NOW: a readable prefix plus new random text for each creation.
// WHY: a fixed Observation ID plus upsert overwrote previous observations.
// Random IDs are collision-resistant; primary keys still enforce uniqueness.
// Extracting this expression into a helper is an optional organization choice.
func newID(prefix string) string { return prefix + "-" + rand.Text() }

// BEFORE: each service knew its concrete resource type and wrote its own loop.
// NOW: this interface means "must have a Validate() error method". Types satisfy
// it implicitly by having the method; no "implements" declaration is needed.
// The generic constraint lets the functions below call Validate on their T value.
type validatedResource interface{ Validate() error }

// BEFORE (concrete Patient decoding):
// var p internal.Patient
// if err := json.Unmarshal(data, &p); err != nil { return internal.Patient{}, err }
// return p, nil
//
// NOW: decodeStored[internal.Patient](data, id) selects T = internal.Patient.
// Unlike handler.decodeResource, this reads DATABASE bytes, not an HTTP body.
// READ IN ORDER: reject unsupported JSON -> reject null/trailing data -> check ID
// -> validate values -> return the concrete resource. expectedID == "" means a
// search has no separate ID to compare, but the stored ID must still be valid.
// var resource *T and Decode(&resource) distinguish null from an object;
// *resource retrieves the value after the nil check.
// WHY: basic Unmarshal silently ignored unknown fields that could disappear on
// later edits. Strict reads reveal records that need migration; they do not fix them.
// OPTIONAL ABSTRACTION: a concrete decodeStoredPatient could do the same checks.
func decodeStored[T validatedResource](data []byte, expectedID string) (T, error) {
	var zero T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var resource *T
	if err := decoder.Decode(&resource); err != nil {
		return zero, fmt.Errorf("decode stored resource: %w", err)
	}
	if resource == nil {
		return zero, fmt.Errorf("stored resource is null")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return zero, fmt.Errorf("stored resource contains trailing data")
	}
	var header struct{ ID string }
	if err := json.Unmarshal(data, &header); err != nil {
		return zero, err
	}
	if !internal.ValidID(header.ID) || (expectedID != "" && header.ID != expectedID) {
		return zero, fmt.Errorf("stored resource id is missing, invalid, or mismatched")
	}
	if err := (*resource).Validate(); err != nil {
		// Stored-data faults are server errors, not errors in the client's request.
		// %v intentionally does not wrap ErrInvalid: a bad database record should
		// become 500, not blame the current request with 400. Compare internal.Invalid.
		return zero, fmt.Errorf("invalid stored resource: %v", err)
	}
	return *resource, nil
}

// BEFORE: each service built a Bundle and looped through its own document type;
// a failed unmarshal used continue, silently dropping the record.
// NOW: one typed loop handles all three. An invalid document fails the search.
// The service has no HTTP request; handler.writeBundle adds the entry URLs later.
// OPTIONAL ABSTRACTION: sharing the loop is independent of fixing its behavior.
func searchBundle[T validatedResource](documents [][]byte) (internal.Bundle, error) {
	total := len(documents)
	bundle := internal.Bundle{ResourceType: "Bundle", Type: "searchset", Total: &total}
	for _, data := range documents {
		resource, err := decodeStored[T](data, "")
		if err != nil {
			return internal.Bundle{}, err
		}
		bundle.Entry = append(bundle.Entry, internal.BundleEntry{Resource: resource})
	}
	return bundle, nil
}

// Same history envelope for each resource. This is a plain function, not a
// generic repository or a new request layer; SQL and write flows stay explicit.
func resourceHistoryBundle(versions []repository.ResourceVersion, id, kind string) (internal.Bundle, error) {
	total := len(versions)
	bundle := internal.Bundle{ResourceType: "Bundle", Type: "history", Total: &total}
	for _, version := range versions {
		entry := internal.BundleEntry{
			Request:  &internal.BundleRequest{Method: version.Method, URL: kind + "/" + id},
			Response: &internal.BundleResponse{Status: "200", ETag: "W/\"" + version.VersionID + "\"", LastModified: version.RecordedAt.UTC().Format(time.RFC3339Nano)},
		}
		if version.Method == "POST" {
			entry.Request.URL = kind
			entry.Response.Status = "201"
		}
		if version.Method == "DELETE" {
			entry.Response.Status = "204"
		} else {
			resource, meta, err := resourceVersionResource(version, id, kind)
			if err != nil {
				return internal.Bundle{}, err
			}
			entry.Resource = resource
			entry.Response.LastModified = meta.LastUpdated
		}
		bundle.Entry = append(bundle.Entry, entry)
	}
	return bundle, nil
}

// Historical reads retain the whole saved JSON, including fields unsupported by
// today's resource struct. RawMessage preserves numbers and unknown properties.
// Only server metadata is filled from the history row for legacy snapshots.
func resourceVersionResource(version repository.ResourceVersion, id, resourceType string) (json.RawMessage, internal.Meta, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(version.Data, &document); err != nil {
		return nil, internal.Meta{}, err
	}
	if document == nil {
		return nil, internal.Meta{}, fmt.Errorf("stored resource version is null")
	}
	var header struct {
		ID           string
		ResourceType string
	}
	if err := json.Unmarshal(version.Data, &header); err != nil {
		return nil, internal.Meta{}, err
	}
	if header.ID != id || header.ResourceType != resourceType {
		return nil, internal.Meta{}, fmt.Errorf("stored resource version has invalid identity")
	}
	var metadata map[string]json.RawMessage
	if raw, ok := document["meta"]; ok {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, internal.Meta{}, err
		}
	}
	if metadata == nil {
		metadata = make(map[string]json.RawMessage)
	}
	meta := internal.Meta{VersionID: version.VersionID}
	if raw, ok := metadata["lastUpdated"]; ok {
		if err := json.Unmarshal(raw, &meta.LastUpdated); err != nil {
			return nil, internal.Meta{}, err
		}
	}
	if meta.LastUpdated == "" {
		meta.LastUpdated = version.RecordedAt.UTC().Format(time.RFC3339Nano)
	}
	metadata["versionId"], _ = json.Marshal(meta.VersionID)
	metadata["lastUpdated"], _ = json.Marshal(meta.LastUpdated)
	document["meta"], _ = json.Marshal(metadata)
	data, err := json.Marshal(document)
	return data, meta, err
}
