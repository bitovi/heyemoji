package web

import (
	"testing"
	"time"

	"github.com/bitovi/heyemoji/event"
)

func TestMonthOptions(t *testing.T) {
	loc := event.BusinessLocation()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, loc)
	earliest := time.Date(2026, 6, 10, 0, 0, 0, 0, loc) // mid-June

	opts := monthOptions(earliest, now)
	want := []string{"2026-09", "2026-08", "2026-07", "2026-06"}
	if len(opts) != len(want) {
		t.Fatalf("got %d options, want %d: %+v", len(opts), len(want), opts)
	}
	for i, w := range want {
		if opts[i].Value != w {
			t.Errorf("opts[%d].Value = %q, want %q", i, opts[i].Value, w)
		}
	}
	if opts[0].Label != "September 2026" {
		t.Errorf("opts[0].Label = %q, want %q", opts[0].Label, "September 2026")
	}
}

func TestQuarterOptions(t *testing.T) {
	loc := event.BusinessLocation()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, loc)     // Q3 2026
	earliest := time.Date(2025, 11, 1, 0, 0, 0, 0, loc) // Q4 2025

	opts := quarterOptions(earliest, now)
	want := []string{"2026-Q3", "2026-Q2", "2026-Q1", "2025-Q4"}
	if len(opts) != len(want) {
		t.Fatalf("got %d options, want %d: %+v", len(opts), len(want), opts)
	}
	for i, w := range want {
		if opts[i].Value != w {
			t.Errorf("opts[%d].Value = %q, want %q", i, opts[i].Value, w)
		}
	}
}

func TestYearOptions(t *testing.T) {
	loc := event.BusinessLocation()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, loc)
	earliest := time.Date(2024, 3, 1, 0, 0, 0, 0, loc)

	opts := yearOptions(earliest, now)
	want := []string{"2026", "2025", "2024"}
	if len(opts) != len(want) {
		t.Fatalf("got %d options, want %d: %+v", len(opts), len(want), opts)
	}
	for i, w := range want {
		if opts[i].Value != w {
			t.Errorf("opts[%d].Value = %q, want %q", i, opts[i].Value, w)
		}
	}
}

func TestMonthOptions_SameMonth(t *testing.T) {
	loc := event.BusinessLocation()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, loc)
	earliest := now // first-ever event was also this month

	opts := monthOptions(earliest, now)
	if len(opts) != 1 || opts[0].Value != "2026-09" {
		t.Errorf("opts = %+v, want exactly one entry for 2026-09", opts)
	}
}
