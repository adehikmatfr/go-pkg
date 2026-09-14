package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/adehikmatfr/go-pkg/v2/client/rest"
	restmock "github.com/adehikmatfr/go-pkg/v2/test/mock/client/rest"
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

	result := rest.Get(srv.URL).Execute()
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

	result := rest.Get(srv.URL).Execute()
	if result.Error != nil {
		t.Fatalf("Execute() transport error: %v", result.Error)
	}

	var body echoBody
	err := result.Consume(&body)
	var notOK *rest.ErrorStatusNotOK
	if !errors.As(err, &notOK) {
		t.Fatalf("Consume() error = %v, want *ErrorStatusNotOK", err)
	}
	if notOK.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", notOK.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(notOK.Error(), "bad request") {
		t.Errorf("Error() = %q, want it to contain the response body", notOK.Error())
	}
}

func TestMakeRequestHostFallbackOnInvalidURL(t *testing.T) {
	// A malformed URL must not panic (regression test for the host/uri bug
	// where a failed url.Parse dereferenced a nil *url.URL).
	result := rest.Get("://bad-url").Execute()
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

	result := rest.Get(srv.URL).AddHeader("X-Test", "1").WithTimeout(5).Execute()
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

	result := rest.Do(http.MethodGet, srv.URL).WithTimeout(1).Execute()
	if result.Error == nil {
		t.Fatal("Execute() should time out")
	}
}

func TestConsumeEmptyBody(t *testing.T) {
	resp := &rest.Response{}
	var v echoBody
	if err := resp.Consume(&v); !errors.Is(err, rest.ErrEmptyResponseBody) {
		t.Errorf("Consume() error = %v, want %v", err, rest.ErrEmptyResponseBody)
	}
}

func TestNoRetryDoesNotRetry(t *testing.T) {
	var calls int32
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		atomic.AddInt32(&calls, 1)
		return &rest.Result{Error: errors.New("boom")}
	})

	(&rest.NoRetry{}).DoRequest(client, rest.Request{})
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestRetryAllErrorsRetriesUntilSuccess(t *testing.T) {
	var calls int32
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return &rest.Result{Error: errors.New("transient")}
		}
		return &rest.Result{}
	})

	rt := &rest.RetryAllErrors{RetryConfig: rest.RetryConfig{NumRetry: 5, DelayType: func(n uint) time.Duration { return 0 }}}
	result := rt.DoRequest(client, rest.Request{})
	if result.Error != nil {
		t.Fatalf("DoRequest() error: %v", result.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRetryAllErrorsExhausted(t *testing.T) {
	var calls int32
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		atomic.AddInt32(&calls, 1)
		return &rest.Result{Error: errors.New("always fails")}
	})

	rt := &rest.RetryAllErrors{RetryConfig: rest.RetryConfig{NumRetry: 2, DelayType: func(n uint) time.Duration { return 0 }}}
	result := rt.DoRequest(client, rest.Request{})
	if result.Error == nil {
		t.Fatal("DoRequest() should return the last error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 { // initial + 2 retries
		t.Errorf("calls = %d, want 3", got)
	}

	// RetryError formats every *retried* attempt's error for logging (the
	// final, non-retried failure isn't appended to errorLog).
	msg := rest.RetryError(&rt.RetryConfig)
	if !strings.Contains(msg, "#1:") || !strings.Contains(msg, "#2:") || !strings.Contains(msg, "always fails") {
		t.Errorf("RetryError() = %q, want it to enumerate both retried attempts", msg)
	}
}

func TestRetryAllErrorsReplaysBodyAcrossAttempts(t *testing.T) {
	var gotBodies []string
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		b, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(b))
		if len(gotBodies) < 2 {
			return &rest.Result{Error: errors.New("transient")}
		}
		return &rest.Result{}
	})

	rt := &rest.RetryAllErrors{RetryConfig: rest.RetryConfig{NumRetry: 2, DelayType: func(uint) time.Duration { return 0 }}}
	result := rt.DoRequest(client, rest.Request{Body: strings.NewReader("payload")})
	if result.Error != nil {
		t.Fatalf("DoRequest() error: %v", result.Error)
	}
	for i, b := range gotBodies {
		if b != "payload" {
			t.Errorf("attempt %d body = %q, want %q (replay after read)", i+1, b, "payload")
		}
	}
}

func TestRetryIfTimeoutRetriesOnlyOnTimeout(t *testing.T) {
	var calls int32
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return &rest.Result{Error: fakeTimeoutError{}}
		}
		return &rest.Result{}
	})

	rt := &rest.RetryIfTimeout{RetryConfig: rest.RetryConfig{NumRetry: 3}}
	result := rt.DoRequest(client, rest.Request{})
	if result.Error != nil {
		t.Fatalf("DoRequest() error: %v", result.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestRetryIfTimeoutDoesNotRetryNonTimeout(t *testing.T) {
	var calls int32
	client := restmock.NewDefaultRest(t)
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		atomic.AddInt32(&calls, 1)
		return &rest.Result{Error: errors.New("not a timeout")}
	})

	rt := &rest.RetryIfTimeout{RetryConfig: rest.RetryConfig{NumRetry: 3}}
	rt.DoRequest(client, rest.Request{})
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (non-timeout errors must not retry)", got)
	}
}

// fakeTimeoutError implements net.Error with Timeout() == true.
type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "i/o timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

func TestBackOffDelay(t *testing.T) {
	delay := rest.BackOffDelay(100 * time.Millisecond)
	if got := delay(0); got != 100*time.Millisecond {
		t.Errorf("delay(0) = %v, want 100ms", got)
	}
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

func TestNewRetryAllErrorsDefaults(t *testing.T) {
	rt := rest.NewRetryAllErrors()
	if rt.NumRetry != 3 {
		t.Errorf("NumRetry = %d, want 3", rt.NumRetry)
	}
	if rt.DelayType == nil {
		t.Fatal("DelayType should be set")
	}
	if got := rt.DelayType(1); got != 100*time.Millisecond {
		t.Errorf("DelayType(1) = %v, want 100ms", got)
	}
}

func TestPackageLevelConvenienceMethods(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cases := []struct {
		name string
		call func(url string) *rest.RequestBuilder
		want string
	}{
		{"Post", rest.Post, http.MethodPost},
		{"Get", rest.Get, http.MethodGet},
		{"Put", rest.Put, http.MethodPut},
		{"Patch", rest.Patch, http.MethodPatch},
		{"Delete", rest.Delete, http.MethodDelete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.call(srv.URL).Execute()
			if result.Error != nil {
				t.Fatalf("Execute() error: %v", result.Error)
			}
			if gotMethod != tc.want {
				t.Errorf("method = %q, want %q", gotMethod, tc.want)
			}
		})
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

	c := rest.NewAPIRequestClient("svc", srv.URL, 5)
	result := c.Get("/ping").Execute()
	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.Response.StatusCode, http.StatusOK)
	}
}

func TestAPIRequestClientConvenienceMethods(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := rest.NewAPIRequestClient("svc", srv.URL, 5)
	cases := []struct {
		name string
		call func(path string) *rest.RequestBuilder
		want string
	}{
		{"Post", c.Post, http.MethodPost},
		{"Get", c.Get, http.MethodGet},
		{"Put", c.Put, http.MethodPut},
		{"Patch", c.Patch, http.MethodPatch},
		{"Delete", c.Delete, http.MethodDelete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.call("/x").Execute()
			if result.Error != nil {
				t.Fatalf("Execute() error: %v", result.Error)
			}
			if gotMethod != tc.want {
				t.Errorf("method = %q, want %q", gotMethod, tc.want)
			}
		})
	}
}

func TestAPIRequestClientUseProxyInvalidURLDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := rest.NewAPIRequestClient("svc", srv.URL, 5)
	arc, ok := c.(*rest.APIRequestClient)
	if !ok {
		t.Fatal("expected *APIRequestClient")
	}
	// A control character makes url.Parse fail; UseProxy must degrade
	// gracefully (log + no-op) instead of panicking or silently misrouting —
	// verified here by confirming the client still makes a normal,
	// unproxied request afterward instead of inspecting the unexported
	// httpClient field.
	arc.UseProxy("http://\x7f")

	result := c.Get("/").Execute()
	if result.Error != nil {
		t.Fatalf("Execute() after invalid UseProxy() error: %v", result.Error)
	}
}

func TestAPIRequestClientHooks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := rest.NewAPIRequestClient("svc", srv.URL, 5)
	arc, ok := c.(*rest.APIRequestClient)
	if !ok {
		t.Fatal("expected *APIRequestClient")
	}

	var startCalled, finishedCalled bool
	arc.OnRequestStart(func(r *http.Request) error {
		startCalled = true
		return nil
	})
	arc.OnRequestFinished(func(res *rest.Result) error {
		finishedCalled = true
		return nil
	})

	if result := c.Get("/").Execute(); result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if !startCalled {
		t.Error("OnRequestStart hook set on the client was not applied to the built request")
	}
	if !finishedCalled {
		t.Error("OnRequestFinished hook set on the client was not applied to the built request")
	}
}

func TestAPIRequestClientUseProxyValidURL(t *testing.T) {
	c := rest.NewAPIRequestClient("svc", "http://example.invalid", 5)
	arc, ok := c.(*rest.APIRequestClient)
	if !ok {
		t.Fatal("expected *APIRequestClient")
	}
	if got := arc.UseProxy("http://proxy.example.invalid:8080"); got != arc {
		t.Error("UseProxy() should return the receiver for chaining")
	}
}

func TestSetBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := rest.Get(srv.URL).SetBasicAuth("alice", "secret").Execute()
	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("Authorization header = %q, want it to start with %q", gotAuth, "Basic ")
	}
}

func TestRequestBuilderSettersAffectExecution(t *testing.T) {
	var gotBody string
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "ctx-value")

	var gotCtxValue interface{}
	var startCalled, finishedCalled bool

	result := rest.Post(srv.URL).
		WithBody(strings.NewReader("payload")).
		WithContext(ctx).
		AddHeaders(map[string]string{"X-A": "1", "X-B": "2"}).
		OnRequestStart(func(r *http.Request) error {
			startCalled = true
			gotCtxValue = r.Context().Value(ctxKey{})
			return nil
		}).
		OnRequestFinished(func(res *rest.Result) error {
			finishedCalled = true
			return nil
		}).
		Execute()

	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if gotBody != "payload" {
		t.Errorf("body = %q, want %q", gotBody, "payload")
	}
	if gotHeaders.Get("X-A") != "1" || gotHeaders.Get("X-B") != "2" {
		t.Errorf("headers = %v, want X-A=1 X-B=2", gotHeaders)
	}
	if !startCalled {
		t.Error("OnRequestStart hook was not called")
	}
	if !finishedCalled {
		t.Error("OnRequestFinished hook was not called")
	}
	if gotCtxValue != "ctx-value" {
		t.Errorf("request context value = %v, want %q (WithContext not propagated)", gotCtxValue, "ctx-value")
	}
}

func TestRequestBuilderWithClientAndRetryStrategy(t *testing.T) {
	client := restmock.NewDefaultRest(t)
	var calls int
	client.EXPECT().MakeRequest(mock.Anything).RunAndReturn(func(r rest.Request) *rest.Result {
		calls++
		if calls < 2 {
			return &rest.Result{Error: errors.New("transient")}
		}
		return &rest.Result{}
	})

	result := rest.Get("http://example.invalid").
		WithClient(client).
		WithRetryStrategy(&rest.RetryAllErrors{RetryConfig: rest.RetryConfig{NumRetry: 3, DelayType: func(uint) time.Duration { return 0 }}}).
		Execute()

	if result.Error != nil {
		t.Fatalf("Execute() error: %v", result.Error)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one failure, one retry)", calls)
	}
}

func TestWithClientIgnoresNilOrZeroValue(t *testing.T) {
	// A nil or zero-value client must be ignored so the builder keeps its
	// default DefaultRestClient, per WithClient's documented contract.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := rest.Get(srv.URL).WithClient(nil).Execute()
	if result.Error != nil {
		t.Fatalf("Execute() with WithClient(nil) error: %v", result.Error)
	}
}
