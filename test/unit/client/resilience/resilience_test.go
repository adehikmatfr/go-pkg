package resilience_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/client/resilience"
)

func TestNew_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     resilience.Config
		wantErr error
	}{
		{name: "negative max retries", cfg: resilience.Config{MaxRetries: -1, FailureThreshold: 1}, wantErr: resilience.ErrInvalidMaxRetries},
		{name: "zero failure threshold", cfg: resilience.Config{FailureThreshold: 0}, wantErr: resilience.ErrInvalidFailureThreshold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resilience.New(nil, tt.cfg); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNew_Valid(t *testing.T) {
	rt, err := resilience.New(nil, resilience.Config{FailureThreshold: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rt == nil {
		t.Fatalf("expected a non-nil RoundTripper")
	}
}

func TestRoundTrip_SuccessPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{FailureThreshold: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestRoundTrip_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{
		MaxRetries:       5,
		RetryBackoff:     time.Millisecond,
		RetryMaxBackoff:  5 * time.Millisecond,
		FailureThreshold: 10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server received %d calls, want 3 (2 failures + 1 success)", got)
	}
}

func TestRoundTrip_DoesNotRetry4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{
		MaxRetries:       5,
		RetryBackoff:     time.Millisecond,
		FailureThreshold: 10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("server received %d calls, want 1 (a 4xx must not be retried)", got)
	}
}

func TestRoundTrip_CircuitOpensAfterConsecutiveFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{
		MaxRetries:       0,
		FailureThreshold: 2,
		OpenDelay:        time.Hour, // stays open for the duration of this test
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	// Two consecutive failures trip the breaker.
	for i := 0; i < 2; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("Get() call %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	// The next call should be short-circuited by the open breaker.
	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatalf("expected an error once the circuit is open")
	}
	if !resilience.IsCircuitOpen(err) {
		t.Fatalf("got err %v, want it to satisfy IsCircuitOpen", err)
	}
}

func TestRoundTrip_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{
		FailureThreshold: 10,
		Timeout:          10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatalf("expected a timeout error")
	}
	if !resilience.IsTimeout(err) {
		t.Fatalf("got err %v, want it to satisfy IsTimeout", err)
	}
}

func TestRoundTrip_ContextCancellationIsNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt, err := resilience.New(nil, resilience.Config{
		MaxRetries:       5,
		RetryBackoff:     time.Millisecond,
		FailureThreshold: 10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := &http.Client{Transport: rt}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected an error for a cancelled context")
	}
}
