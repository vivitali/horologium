package timeutil

import (
	"testing"
	"time"
)

func TestIsSameDay(t *testing.T) {
	mon := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC) // Monday
	tests := []struct {
		name string
		a, b time.Time
		want bool
	}{
		{"same day same time", mon, mon, true},
		{"same day different hour", mon, mon.Add(3 * time.Hour), true},
		{"next day", mon, mon.Add(25 * time.Hour), false},
		{"year boundary", time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSameDay(tt.a, tt.b); got != tt.want {
				t.Errorf("IsSameDay(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsThisWeek(t *testing.T) {
	// Anchor "now" to Wed 2026-01-07 (week is Sun 2026-01-04 .. Sat 2026-01-10)
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		event time.Time
		want  bool
	}{
		{"sunday of this week", time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC), true},
		{"saturday of this week", time.Date(2026, 1, 10, 20, 0, 0, 0, time.UTC), true},
		{"previous saturday", time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC), false},
		{"next sunday", time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsThisWeek(tt.event, now); got != tt.want {
				t.Errorf("IsThisWeek(%v, now=%v) = %v, want %v", tt.event, now, got, tt.want)
			}
		})
	}
}

func TestIsNextWeek(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC) // Wed; next week = Sun 2026-01-11 .. Sat 2026-01-17
	tests := []struct {
		name  string
		event time.Time
		want  bool
	}{
		{"next sunday", time.Date(2026, 1, 11, 9, 0, 0, 0, time.UTC), true},
		{"next saturday", time.Date(2026, 1, 17, 20, 0, 0, 0, time.UTC), true},
		{"this saturday", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), false},
		{"week after next", time.Date(2026, 1, 18, 12, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNextWeek(tt.event, now); got != tt.want {
				t.Errorf("IsNextWeek(%v, now=%v) = %v, want %v", tt.event, now, got, tt.want)
			}
		})
	}
}

func TestGroupByPeriod(t *testing.T) {
	now := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC) // Wed
	events := []time.Time{
		now, // Today
		time.Date(2026, 1, 9, 18, 0, 0, 0, time.UTC),  // Fri (This Week)
		time.Date(2026, 1, 14, 18, 0, 0, 0, time.UTC), // Wed (Next Week)
		time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),  // Later
	}
	got := GroupByPeriod(events, now)
	if len(got["Today"]) != 1 {
		t.Errorf("Today: got %d, want 1", len(got["Today"]))
	}
	if len(got["This Week"]) != 1 {
		t.Errorf("This Week: got %d, want 1", len(got["This Week"]))
	}
	if len(got["Next Week"]) != 1 {
		t.Errorf("Next Week: got %d, want 1", len(got["Next Week"]))
	}
	if len(got["Later"]) != 1 {
		t.Errorf("Later: got %d, want 1", len(got["Later"]))
	}
}
