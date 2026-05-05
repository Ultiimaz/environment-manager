package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin wrapper around http.Client that injects the Bearer
// header and decodes JSON responses + error envelopes.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// NewClient constructs a Client. endpoint is the env-manager base URL
// (e.g. https://manager.blocksweb.nl); the API path is appended per call.
func NewClient(cfg *Config) *Client {
	return &Client{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		token:    cfg.Token,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Do issues a request to the env-manager API. body is JSON-encoded if non-nil.
// out is JSON-decoded if non-nil and the response is 2xx.
//
// Non-2xx responses are returned as a formatted error including status code
// and the server's error envelope (or response body when not JSON).
func (c *Client) Do(method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequest(method, c.endpoint+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
		}
	}
	return nil
}
