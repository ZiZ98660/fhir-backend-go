package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DeliveryResult represents the response recorded from an external destination.
type DeliveryResult struct {
	StatusCode   int    `json:"statusCode"`
	ResponseBody string `json:"responseBody,omitempty"`
}

// Connector is the abstraction for delivering clinical data to an external system.
// Connectors adapt our internal FHIR bundles to each external EMR's expected
// transport protocol, endpoint, headers, and authentication.
type Connector interface {
	Destination() string
	Deliver(ctx context.Context, bundle []byte) (DeliveryResult, error)
}

// HTTPConnector sends FHIR transaction bundles over HTTP to a target EMR.
type HTTPConnector struct {
	destination string
	endpointURL string
	bearerToken string
	client      *http.Client
}

func NewHTTPConnector(destination, endpointURL, bearerToken string, client *http.Client) *HTTPConnector {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPConnector{
		destination: destination,
		endpointURL: endpointURL,
		bearerToken: bearerToken,
		client:      client,
	}
}

func (c *HTTPConnector) Destination() string {
	return c.destination
}

func (c *HTTPConnector) Deliver(ctx context.Context, bundle []byte) (DeliveryResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointURL, bytes.NewReader(bundle))
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("create delivery request: %w", err)
	}

	req.Header.Set("Content-Type", "application/fhir+json")
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("outbound HTTP delivery failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit response reading to 1 MiB to avoid unbounded memory consumption.
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("read delivery response body: %w", err)
	}

	result := DeliveryResult{
		StatusCode:   resp.StatusCode,
		ResponseBody: string(respBytes),
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("external EMR rejected bundle with status %d: %s", resp.StatusCode, string(respBytes))
	}

	return result, nil
}
