package humanize

import (
	"testing"
	"time"
)

func TestUnderscores(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{12, "12"},
		{123, "123"},
		{1234, "1_234"},
		{1234567, "1_234_567"},
		{1000000000, "1_000_000_000"},
		{-1, "-1"},
		{-12, "-12"},
		{-123, "-123"},
		{-1234, "-1_234"},
		{-1234567, "-1_234_567"},
	}

	for _, tc := range tests {
		got := Underscores(tc.input)
		if got != tc.expected {
			t.Errorf("Underscores(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}

	// Test uint64 with very large numbers
	var maxUint uint64 = 18446744073709551615
	expectedUint := "18_446_744_073_709_551_615"
	if got := Underscores(maxUint); got != expectedUint {
		t.Errorf("Underscores(%d) = %q, want %q", maxUint, got, expectedUint)
	}
}

func TestBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{-1024, "-1.0 KiB"},
		{1048576, "1.0 MiB"},    // 1024 * 1024
		{1610612736, "1.5 GiB"}, // 1.5 * 1024 * 1024 * 1024
	}

	for _, tc := range tests {
		got := Bytes(tc.input)
		if got != tc.expected {
			t.Errorf("Bytes(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			// Round to first digit after decimal point.
			name:     "milliseconds",
			duration: 123*time.Millisecond + 456*time.Microsecond,
			want:     "123.5ms",
		},
		{
			name:     "seconds with fraction",
			duration: 5*time.Second + 678*time.Millisecond,
			want:     "5.7s",
		},
		{
			// If in the range of hours, ignore the seconds.
			name:     "hours and minutes",
			duration: 2*time.Hour + 31*time.Minute + 46*time.Second,
			want:     "2h31m",
		},
		{
			name:     "minutes and seconds",
			duration: 21*time.Minute + 7*time.Second + 123*time.Millisecond,
			want:     "21m7s",
		},
		{
			name:     "microseconds",
			duration: 12*time.Microsecond + 345*time.Nanosecond,
			want:     "12.3µs",
		},
		{
			name:     "nanoseconds",
			duration: 50 * time.Nanosecond,
			want:     "50ns",
		},
		{
			// Days, ignore minutes and smaller.
			name:     "days",
			duration: 22*24*time.Hour + 3*time.Hour + 40*time.Minute,
			want:     "22d3h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Duration(tt.duration); got != tt.want {
				t.Errorf("Duration() = %v, want %v (original: %s)", got, tt.want, tt.duration.String())
			}
		})
	}
}
