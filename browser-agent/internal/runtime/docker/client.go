// Package docker implements the runtime.Driver and runtime.DisplayTransport
// contracts against the Docker Engine API reached through a unix socket. It
// uses only net/http and the standard library, never the Docker SDK.
package docker

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
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

// APIError is a non-2xx Docker Engine API response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("docker api returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("docker api returned status %d: %s", e.StatusCode, e.Message)
}

// Client is a minimal Docker Engine API client bound to one unix socket.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient dials the Docker daemon on socketPath. Requests are sent without an
// API version prefix; the daemon answers with its own API version.
func NewClient(socketPath string) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:       8,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	return newClient("http://docker", &http.Client{Transport: transport})
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *Client) do(ctx context.Context, method string, path string, query url.Values, body any, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("docker api %s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if out == nil || len(data) == 0 {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s %s: %w", method, path, err)
		}
		return nil
	}

	message := ""
	var envelope struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &envelope) == nil {
		message = envelope.Message
	}
	if message == "" && readErr != nil {
		message = readErr.Error()
	}
	return &APIError{StatusCode: response.StatusCode, Message: message}
}

func isStatus(err error, statusCode int) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return false
	}
	return apiError.StatusCode == statusCode
}
