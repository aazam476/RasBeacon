package webserver

// This file lets rbweb talk to rbd over rbd's Unix socket.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"
)

// RBDClient sends requests to rbd's local API.
type RBDClient struct {
	http *http.Client
	// stream has no time limit, for uploading update files.
	stream *http.Client
	// baseURL is a placeholder host: requests are sent over the Unix socket
	// no matter what the URL's host says.
	baseURL string
}

// NewRBDClient connects to rbd's Unix socket at socketPath.
func NewRBDClient(socketPath string) *RBDClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &RBDClient{
		http:    &http.Client{Transport: transport, Timeout: 60 * time.Second},
		stream:  &http.Client{Transport: transport},
		baseURL: "http://rbd",
	}
}

// NewRBDClientForHandler sends requests straight to an http.Handler instead
// of a socket. Tests use it to run rbweb against a real agent in memory.
func NewRBDClientForHandler(handler http.Handler) *RBDClient {
	client := &http.Client{Transport: handlerTransport{handler}}
	return &RBDClient{http: client, stream: client, baseURL: "http://rbd"}
}

// Stream sends a large request body to rbd as it arrives (without reading
// it into memory first) and returns the status code and response body.
func (c *RBDClient) Stream(path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("contacting rbd: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	return resp.StatusCode, respBody, err
}

// Open sends a JSON request to rbd without a time limit and returns the
// response for the caller to stream (and close).
func (c *RBDClient) Open(method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting rbd: %w", err)
	}
	return resp, nil
}

// Do sends a request to rbd and returns the status code and response body.
// body may be nil for requests without one.
func (c *RBDClient) Do(method, path string, body []byte) (int, []byte, error) {
	return c.do(c.http, method, path, body)
}

// DoSlow is Do without the time limit, for requests rbd itself limits
// (the speed test).
func (c *RBDClient) DoSlow(method, path string, body []byte) (int, []byte, error) {
	return c.do(c.stream, method, path, body)
}

func (c *RBDClient) do(client *http.Client, method, path string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("contacting rbd: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	return resp.StatusCode, respBody, err
}

// GetJSON fetches path from rbd and decodes the JSON response into target.
func (c *RBDClient) GetJSON(path string, target any) error {
	status, body, err := c.Do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("rbd %s returned %d: %s", path, status, body)
	}
	return json.Unmarshal(body, target)
}

// handlerTransport is an http.RoundTripper that calls a handler directly,
// recording the response with the standard library's httptest recorder.
type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}
