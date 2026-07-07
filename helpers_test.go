package main

import (
	"testing"
	"time"
)

func TestParseDeviceTimeSupportedLayouts(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"azx timestamp", "2026-07-07T15:04:05", "2026-07-07T15:04:05+05:30"},
		{"evt date first with space", "07-07-2026 15:04:05", "2026-07-07T15:04:05+05:30"},
		{"evt date first with t", "07-07-2026T15:04:05", "2026-07-07T15:04:05+05:30"},
		{"date first with space", "2026-07-07 15:04:05", "2026-07-07T15:04:05+05:30"},
		{"rfc3339", "2026-07-07T09:34:05Z", "2026-07-07T15:04:05+05:30"},
		{"rfc3339 nano", "2026-07-07T09:34:05.123456789Z", "2026-07-07T15:04:05+05:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDeviceTime(tt.value)
			if err != nil {
				t.Fatalf("parseDeviceTime(%q) returned error: %v", tt.value, err)
			}
			if got.Format(time.RFC3339) != tt.want {
				t.Fatalf("parseDeviceTime(%q) = %s, want %s", tt.value, got.Format(time.RFC3339), tt.want)
			}
		})
	}
}

func TestParseDeviceTimeRejectsUnsupportedLayout(t *testing.T) {
	if _, err := parseDeviceTime("07/07/2026 15:04:05"); err == nil {
		t.Fatal("parseDeviceTime accepted an unsupported timestamp layout")
	}
}
