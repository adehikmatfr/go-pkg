package cron_test

import (
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/scheduler/cron"
)

func TestParseSpec_Invalid(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{name: "too few fields", spec: "* * *"},
		{name: "too many fields", spec: "* * * * * *"},
		{name: "invalid value", spec: "abc * * * *"},
		{name: "out of range", spec: "60 * * * *"},
		{name: "invalid range", spec: "5-abc * * * *"},
		{name: "invalid step", spec: "*/abc * * * *"},
		{name: "zero step", spec: "*/0 * * * *"},
		{name: "range out of order", spec: "10-5 * * * *"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := cron.ParseSpec(tt.spec); err == nil {
				t.Fatalf("expected an error for spec %q", tt.spec)
			}
		})
	}
}

func TestSchedule_Next(t *testing.T) {
	tests := []struct {
		name string
		spec string
		from time.Time
		want time.Time
	}{
		{
			name: "every minute",
			spec: "* * * * *",
			from: time.Date(2024, 1, 1, 10, 30, 15, 0, time.UTC),
			want: time.Date(2024, 1, 1, 10, 31, 0, 0, time.UTC),
		},
		{
			name: "top of every hour",
			spec: "0 * * * *",
			from: time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC),
			want: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "specific minute list",
			spec: "0,15,30,45 * * * *",
			from: time.Date(2024, 1, 1, 10, 16, 0, 0, time.UTC),
			want: time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "step range",
			spec: "*/15 * * * *",
			from: time.Date(2024, 1, 1, 10, 16, 0, 0, time.UTC),
			want: time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "day of week Sunday as 0",
			spec: "0 0 * * 0",
			from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), // a Monday
			want: time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC), // the following Sunday
		},
		{
			name: "day of week Sunday as 7",
			spec: "0 0 * * 7",
			from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2024, 1, 7, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := cron.ParseSpec(tt.spec)
			if err != nil {
				t.Fatalf("ParseSpec: %v", err)
			}
			got := sched.Next(tt.from)
			if !got.Equal(tt.want) {
				t.Errorf("Next(%v) = %v, want %v", tt.from, got, tt.want)
			}
		})
	}
}

func TestSchedule_Next_DomOrDowUnion(t *testing.T) {
	// "0 0 1 * 1" means minute=0, hour=0, day-of-month=1, every month, day-of-week=Monday.
	// Vixie-cron rule: when both dom and dow are restricted, a tick fires if EITHER matches.
	sched, err := cron.ParseSpec("0 0 1 * 1")
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	// From Jan 2 (a Tuesday), the next dom=1 is Feb 1; the next Monday is Jan 8 — the
	// union picks whichever comes first, which is the Monday.
	from := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	got := sched.Next(from)
	want := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v (next Monday, before day-of-month 1)", from, got, want)
	}
}

func TestSchedule_Next_Unsatisfiable(t *testing.T) {
	// Feb 30 never exists, so no minute within the 5-year search window matches.
	sched, err := cron.ParseSpec("0 0 30 2 *")
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	got := sched.Next(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	if !got.IsZero() {
		t.Errorf("Next() for an unsatisfiable spec = %v, want the zero time", got)
	}
}
