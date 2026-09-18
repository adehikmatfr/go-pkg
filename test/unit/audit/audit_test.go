package audit_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/audit"
)

func sampleEvent() audit.AuditEvent {
	return audit.AuditEvent{
		ID:     "evt-1",
		Source: "/orders",
		Type:   "tech.example.order.placed",
		Time:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestAuditEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   audit.AuditEvent
		wantErr error
	}{
		{name: "valid", event: sampleEvent(), wantErr: nil},
		{name: "missing id", event: audit.AuditEvent{Source: "/orders", Type: "t"}, wantErr: audit.ErrMissingID},
		{name: "missing source", event: audit.AuditEvent{ID: "1", Type: "t"}, wantErr: audit.ErrMissingSource},
		{name: "missing type", event: audit.AuditEvent{ID: "1", Source: "/orders"}, wantErr: audit.ErrMissingType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuditEvent_CanonicalJSON_IsOrderAndTimezoneIndependent(t *testing.T) {
	inUTC := audit.AuditEvent{
		ID:     "evt-1",
		Source: "/orders",
		Type:   "tech.example.order.placed",
		Time:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Data:   map[string]any{"b": 2, "a": 1},
	}
	loc := time.FixedZone("UTC+7", 7*60*60)
	inOtherTZ := audit.AuditEvent{
		ID:     "evt-1",
		Source: "/orders",
		Type:   "tech.example.order.placed",
		Time:   time.Date(2024, 1, 1, 19, 0, 0, 0, loc), // same instant as inUTC
		Data:   map[string]any{"a": 1, "b": 2},          // same data, different key order
	}

	got1, err := inUTC.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	got2, err := inOtherTZ.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if string(got1) != string(got2) {
		t.Fatalf("CanonicalJSON differs for the same instant/data:\n%s\nvs\n%s", got1, got2)
	}
}

func TestAuditEvent_CanonicalJSON_SubjectAndNestedData(t *testing.T) {
	e := audit.AuditEvent{
		ID:      "evt-1",
		Source:  "/orders",
		Type:    "tech.example.order.placed",
		Subject: "order-42",
		Time:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Data: map[string]any{
			"items": []any{
				map[string]any{"sku": "b-sku", "qty": 2},
				map[string]any{"sku": "a-sku", "qty": 1},
			},
			"total": 3,
		},
	}

	got, err := e.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	const want = `{"data":{"items":[{"qty":2,"sku":"b-sku"},{"qty":1,"sku":"a-sku"}],"total":3},"id":"evt-1","source":"/orders","subject":"order-42","time":"2024-01-01T00:00:00Z","type":"tech.example.order.placed"}`
	if string(got) != want {
		t.Fatalf("CanonicalJSON() =\n%s\nwant\n%s", got, want)
	}
}

func TestAuditEvent_CanonicalJSON_UnmarshalableData(t *testing.T) {
	e := audit.AuditEvent{
		ID:     "evt-1",
		Source: "/orders",
		Type:   "tech.example.order.placed",
		Data:   map[string]any{"bad": math.NaN()},
	}
	if _, err := e.CanonicalJSON(); err == nil {
		t.Fatalf("expected an error for data that cannot be JSON-marshaled")
	}
}

func TestComputeRowHash_IsDeterministicAndChainSensitive(t *testing.T) {
	e := sampleEvent()

	h1, err := audit.ComputeRowHash(audit.GenesisHash, e)
	if err != nil {
		t.Fatalf("ComputeRowHash: %v", err)
	}
	h2, err := audit.ComputeRowHash(audit.GenesisHash, e)
	if err != nil {
		t.Fatalf("ComputeRowHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("ComputeRowHash is not deterministic: %q vs %q", h1, h2)
	}

	h3, err := audit.ComputeRowHash("some-other-prev-hash", e)
	if err != nil {
		t.Fatalf("ComputeRowHash: %v", err)
	}
	if h1 == h3 {
		t.Fatalf("ComputeRowHash did not change with a different prevHash")
	}
}

func chainOf(t *testing.T, events ...audit.AuditEvent) []audit.Record {
	t.Helper()
	var records []audit.Record
	prev := audit.GenesisHash
	for _, e := range events {
		hash, err := audit.ComputeRowHash(prev, e)
		if err != nil {
			t.Fatalf("ComputeRowHash: %v", err)
		}
		records = append(records, audit.Record{PrevHash: prev, RowHash: hash, Event: e})
		prev = hash
	}
	return records
}

func TestVerifyChain(t *testing.T) {
	e1 := sampleEvent()
	e2 := audit.AuditEvent{ID: "evt-2", Source: "/orders", Type: "tech.example.order.filled", Time: e1.Time.Add(time.Minute)}

	t.Run("empty chain is valid", func(t *testing.T) {
		if err := audit.VerifyChain(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid chain", func(t *testing.T) {
		if err := audit.VerifyChain(chainOf(t, e1, e2)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("tampered event breaks the chain", func(t *testing.T) {
		records := chainOf(t, e1, e2)
		records[0].Event.Subject = "tampered"
		if err := audit.VerifyChain(records); !errors.Is(err, audit.ErrChainBroken) {
			t.Fatalf("got err %v, want ErrChainBroken", err)
		}
	})

	t.Run("wrong genesis prev hash breaks the chain", func(t *testing.T) {
		records := chainOf(t, e1)
		records[0].PrevHash = "not-genesis"
		if err := audit.VerifyChain(records); !errors.Is(err, audit.ErrChainBroken) {
			t.Fatalf("got err %v, want ErrChainBroken", err)
		}
	})

	t.Run("out-of-order records break the chain", func(t *testing.T) {
		records := chainOf(t, e1, e2)
		records[0], records[1] = records[1], records[0]
		if err := audit.VerifyChain(records); !errors.Is(err, audit.ErrChainBroken) {
			t.Fatalf("got err %v, want ErrChainBroken", err)
		}
	})
}
