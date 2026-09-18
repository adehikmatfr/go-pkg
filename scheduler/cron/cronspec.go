package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed 5-field crontab schedule (minute, hour, day-of-month,
// month, day-of-week), usable by any adapter that needs to compute fire
// times without depending on an external cron library (see
// scheduler/cron/memory).
//
// Supported per field: "*", a single value, a comma list ("1,15"), a range
// ("1-5") and a step ("*/5" or "10-30/5"). Day-of-week 0 and 7 both mean
// Sunday. Named months/weekdays and "@hourly"-style macros are not
// supported.
type Schedule struct {
	minute  uint64 // bit i set => minute i allowed (0..59)
	hour    uint64 // 0..23
	dom     uint64 // 1..31
	month   uint64 // 1..12
	dow     uint64 // 0..6 (Sunday=0)
	domStar bool   // true if day-of-month was "*"
	dowStar bool   // true if day-of-week was "*"
}

type fieldSpec struct {
	min, max int
}

var cronFields = []fieldSpec{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 7},  // day of week (7 folds to 0)
}

// ParseSpec parses a standard 5-field crontab expression.
func ParseSpec(spec string) (Schedule, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron: spec %q: expected 5 fields, got %d", spec, len(fields))
	}
	bits := make([]uint64, 5)
	for i, f := range fields {
		b, err := parseCronField(f, cronFields[i])
		if err != nil {
			return Schedule{}, fmt.Errorf("cron: spec %q field %d: %w", spec, i, err)
		}
		bits[i] = b
	}
	// Normalize day-of-week: bit 7 (Sunday) folds onto bit 0.
	if bits[4]&(1<<7) != 0 {
		bits[4] = (bits[4] &^ (1 << 7)) | 1
	}
	return Schedule{
		minute:  bits[0],
		hour:    bits[1],
		dom:     bits[2],
		month:   bits[3],
		dow:     bits[4],
		domStar: fields[2] == "*",
		dowStar: fields[4] == "*",
	}, nil
}

func parseCronField(field string, fs fieldSpec) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(field, ",") {
		rangePart, stepStr, _ := strings.Cut(part, "/")
		step := 1
		if stepStr != "" {
			s, err := strconv.Atoi(stepStr)
			if err != nil || s <= 0 {
				return 0, fmt.Errorf("invalid step %q", stepStr)
			}
			step = s
		}

		lo, hi := fs.min, fs.max
		switch {
		case rangePart == "*":
			// full range
		case strings.Contains(rangePart, "-"):
			ends := strings.SplitN(rangePart, "-", 2)
			a, err1 := strconv.Atoi(ends[0])
			b, err2 := strconv.Atoi(ends[1])
			if err1 != nil || err2 != nil {
				return 0, fmt.Errorf("invalid range %q", rangePart)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(rangePart)
			if err != nil {
				return 0, fmt.Errorf("invalid value %q", rangePart)
			}
			lo, hi = v, v
		}
		if lo < fs.min || hi > fs.max || lo > hi {
			return 0, fmt.Errorf("value out of range %q (allowed %d-%d)", rangePart, fs.min, fs.max)
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

// Next returns the earliest time strictly after t that matches the
// schedule, in t's location, truncated to the minute. It searches
// minute-by-minute and is bounded to ~5 years to guarantee termination on an
// unsatisfiable spec (in which case it returns the zero time.Time).
func (s Schedule) Next(t time.Time) time.Time {
	// Start from the next whole minute.
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s Schedule) matches(t time.Time) bool {
	if s.minute&(1<<uint(t.Minute())) == 0 {
		return false
	}
	if s.hour&(1<<uint(t.Hour())) == 0 {
		return false
	}
	if s.month&(1<<uint(int(t.Month()))) == 0 {
		return false
	}
	domMatch := s.dom&(1<<uint(t.Day())) != 0
	dowMatch := s.dow&(1<<uint(int(t.Weekday()))) != 0
	// Vixie-cron rule: with both day-of-month and day-of-week restricted the
	// job runs if either matches; a "*" side is a pass-through satisfied by
	// the other.
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowMatch
	case s.dowStar:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}
