package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"
	"github.com/adehikmatfr/go-pkg/v2/datastore/cache/memory"
)

func TestPing(t *testing.T) {
	c := memory.NewClient()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestStringRoundTrip(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	if err := c.SetString(ctx, "greeting", "hello", 0); err != nil {
		t.Fatalf("SetString() error: %v", err)
	}
	got, err := c.GetString(ctx, "greeting")
	if err != nil {
		t.Fatalf("GetString() error: %v", err)
	}
	if got != "hello" {
		t.Errorf("GetString() = %q, want %q", got, "hello")
	}
}

func TestIntRoundTrip(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	if err := c.SetInt(ctx, "count", 42, 0); err != nil {
		t.Fatalf("SetInt() error: %v", err)
	}
	got, err := c.GetInt(ctx, "count")
	if err != nil {
		t.Fatalf("GetInt() error: %v", err)
	}
	if got != 42 {
		t.Errorf("GetInt() = %d, want 42", got)
	}
}

func TestBoolRoundTrip(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	if err := c.SetBool(ctx, "flag", true, 0); err != nil {
		t.Fatalf("SetBool() error: %v", err)
	}
	got, err := c.GetBool(ctx, "flag")
	if err != nil {
		t.Fatalf("GetBool() error: %v", err)
	}
	if !got {
		t.Error("GetBool() = false, want true")
	}
}

type testObject struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestObjectRoundTrip(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	want := testObject{Name: "alice", Age: 30}
	if err := c.SetObject(ctx, "user:1", want, 0); err != nil {
		t.Fatalf("SetObject() error: %v", err)
	}

	var got testObject
	if err := c.GetObject(ctx, "user:1", &got); err != nil {
		t.Fatalf("GetObject() error: %v", err)
	}
	if got != want {
		t.Errorf("GetObject() = %+v, want %+v", got, want)
	}
}

func TestGetStringNotFound(t *testing.T) {
	c := memory.NewClient()
	_, err := c.GetString(context.Background(), "missing")
	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("GetString() error = %v, want %v", err, cache.ErrNotFound)
	}
}

func TestDelete(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	if err := c.SetString(ctx, "key", "value", 0); err != nil {
		t.Fatalf("SetString() error: %v", err)
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := c.GetString(ctx, "key"); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("GetString() after Delete() error = %v, want %v", err, cache.ErrNotFound)
	}
}

func TestSetStringWithExpiration(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	if err := c.SetString(ctx, "temp", "value", 20*time.Millisecond); err != nil {
		t.Fatalf("SetString() error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	if _, err := c.GetString(ctx, "temp"); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("GetString() after expiration error = %v, want %v", err, cache.ErrNotFound)
	}
}

func TestClose(t *testing.T) {
	c := memory.NewClient()
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := memory.NewClient()
	ctx := context.Background()

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			_ = c.SetInt(ctx, "counter", n, 0)
			_, _ = c.GetInt(ctx, "counter")
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
