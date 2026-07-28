package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type echoBody struct {
	Value string `json:"value"`
}

func TestMakeRequestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(echoBody{Value: "hello"})
	}))
	defer srv.Close()

	result := Get(srv.URL).Execute()
	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}

	var body echoBody
	if err := result.Consume(&body); err != nil {
		t.Fatalf("Consume() error: %v", err)
	}
	if body.Value != "hello" {
		t.Errorf("body.Value = %q, want %q", body.Value, "hello")
	}
}

func TestMakeRequestNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	result := Get(srv.URL).Execute()
	if result.Error != nil {
		t.Fatalf("Execute() transport error: %v", result.Error)
	}

	var body echoBody
	err := result.Consume(&body)
	var notOK *ErrorStatusNotOK
	if !errors.As(err, &notOK) {
		t.Fatalf("Consume() error = %v, want *ErrorStatusNotOK", err)
	}
	if notOK.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", notOK.StatusCode, http.StatusBadRequest)
	}
}

func TestMakeRequestHostFallbackOnInvalidURL(t *testing.T) {
	// A malformed URL must not panic (regression test for the host/uri bug
	// where a failed url.Parse dereferenced a nil *url.URL).
	result := Get("://bad-url").Execute()
	if result.Error == nil {
		t.Fatal("Execute() with malformed URL should produce an error, not a panic")
	}
}

func TestMakeRequestHeadersAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := Get(srv.URL).AddHeader("X-Test", "1").WithTimeout(5).Execute()
	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.Response.StatusCode, http.StatusOK)
	}
}

func TestMakeRequestTimeoutExceeded(t *testing.T) {
	// Timeout is expressed in whole seconds, so the handler must sleep well
	// past 1s for a 1s timeout to reliably fire.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := Do(http.MethodGet, srv.URL).WithTimeout(1).Execute()
	if result.Error == nil {
		t.Fatal("Execute() should time out")
	}
}

func TestConsumeEmptyBody(t *testing.T) {
	resp := &Response{}
	var v echoBody
	if err := resp.Consume(&v); !errors.Is(err, ErrEmptyResponseBody) {
		t.Errorf("Consume() error = %v, want %v", err, ErrEmptyResponseBody)
	}
}

func TestNoRetryDoesNotRetry(t *testing.T) {
	var calls int32
	fake := fakeRest{fn: func(r Request) *Result {
		atomic.AddInt32(&calls, 1)
		return &Result{Error: errors.New("boom")}
	}}

	(&NoRetry{}).DoRequest(fake, Request{})
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestRetryAllErrorsRetriesUntilSuccess(t *testing.T) {
	var calls int32
	fake := fakeRest{fn: func(r Request) *Result {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return &Result{Error: errors.New("transient")}
		}
		return &Result{}
	}}

	rt := &RetryAllErrors{RetryConfig: RetryConfig{NumRetry: 5, DelayType: func(n uint) time.Duration { return 0 }}}
	result := rt.DoRequest(fake, Request{})
	if result.Error != nil {
		t.Fatalf("DoRequest() error: %v", result.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRetryAllErrorsExhausted(t *testing.T) {
	var calls int32
	fake := fakeRest{fn: func(r Request) *Result {
		atomic.AddInt32(&calls, 1)
		return &Result{Error: errors.New("always fails")}
	}}

	rt := &RetryAllErrors{RetryConfig: RetryConfig{NumRetry: 2, DelayType: func(n uint) time.Duration { return 0 }}}
	result := rt.DoRequest(fake, Request{})
	if result.Error == nil {
		t.Fatal("DoRequest() should return the last error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 { // initial + 2 retries
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestBackOffDelay(t *testing.T) {
	delay := BackOffDelay(100 * time.Millisecond)
	if got := delay(1); got != 100*time.Millisecond {
		t.Errorf("delay(1) = %v, want 100ms", got)
	}
	if got := delay(2); got != 200*time.Millisecond {
		t.Errorf("delay(2) = %v, want 200ms", got)
	}
	if got := delay(3); got != 400*time.Millisecond {
		t.Errorf("delay(3) = %v, want 400ms", got)
	}
}

func TestAPIRequestClientUsesBaseURLAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAPIRequestClient("svc", srv.URL, 5)
	result := c.Get("/ping").Execute()
	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.Response.StatusCode, http.StatusOK)
	}
}

func TestAPIRequestClientUseProxyInvalidURLDoesNotPanic(t *testing.T) {
	c := NewAPIRequestClient("svc", "http://example.invalid", 5)
	arc, ok := c.(*APIRequestClient)
	if !ok {
		t.Fatal("expected *APIRequestClient")
	}
	// A control character makes url.Parse fail; UseProxy must degrade
	// gracefully (log + no-op) instead of panicking or silently misrouting.
	arc.UseProxy("http://\x7f")
	if arc.httpClient != nil {
		t.Error("UseProxy() with invalid URL should leave httpClient unset")
	}
}

func TestSetBasicAuth(t *testing.T) {
	rb := Get("http://example.invalid").SetBasicAuth("alice", "secret")
	got := rb.request.Headers["Authorization"]
	if got == "" || got[:6] != "Basic " {
		t.Errorf("Authorization header = %q, want it to start with %q", got, "Basic ")
	}
}

// fakeRest lets tests observe/control MakeRequest without a real network call.
type fakeRest struct {
	fn func(r Request) *Result
}

func (f fakeRest) MakeRequest(r Request) *Result {
	return f.fn(r)
}
