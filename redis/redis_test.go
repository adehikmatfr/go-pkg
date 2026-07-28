package redis

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestClient(t *testing.T) Redis {
	t.Helper()
	mr := miniredis.RunT(t)
	port, err := strconv.Atoi(mr.Port())
	if err != nil {
		t.Fatalf("invalid miniredis port: %v", err)
	}
	return NewClient(&Config{Host: mr.Host(), Port: port})
}

func TestPing(t *testing.T) {
	c := newTestClient(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestStringRoundTrip(t *testing.T) {
	c := newTestClient(t)
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
	c := newTestClient(t)
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
	c := newTestClient(t)
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
	c := newTestClient(t)
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
	c := newTestClient(t)
	_, err := c.GetString(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetString() error = %v, want %v", err, ErrNotFound)
	}
}

func TestDelete(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	if err := c.SetString(ctx, "key", "value", 0); err != nil {
		t.Fatalf("SetString() error: %v", err)
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := c.GetString(ctx, "key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetString() after Delete() error = %v, want %v", err, ErrNotFound)
	}
}

func TestSetStringWithExpiration(t *testing.T) {
	mr := miniredis.RunT(t)
	port, _ := strconv.Atoi(mr.Port())
	c := NewClient(&Config{Host: mr.Host(), Port: port})
	ctx := context.Background()

	if err := c.SetString(ctx, "temp", "value", 100*time.Millisecond); err != nil {
		t.Fatalf("SetString() error: %v", err)
	}
	mr.FastForward(200 * time.Millisecond)

	if _, err := c.GetString(ctx, "temp"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetString() after expiration error = %v, want %v", err, ErrNotFound)
	}
}

func TestContextCancellation(t *testing.T) {
	c := newTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.SetString(ctx, "key", "value", 0); err == nil {
		t.Error("SetString() with canceled context should error")
	}
}

func TestClose(t *testing.T) {
	c := newTestClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}
