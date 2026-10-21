package webserver

import (
	"net/http"
	"net/http/httptest"
)

// NewRBDClientForHandler sends requests straight to an http.Handler instead
// of a socket, so tests run rbweb against a fake rbd in memory.
func NewRBDClientForHandler(handler http.Handler) *RBDClient {
	client := &http.Client{Transport: handlerTransport{handler}}
	return &RBDClient{http: client, stream: client, baseURL: "http://rbd"}
}

// handlerTransport is an http.RoundTripper that calls a handler directly,
// recording the response with the standard library's httptest recorder.
type handlerTransport struct{ handler http.Handler }

// RoundTrip answers a request by calling the handler directly.
func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}
