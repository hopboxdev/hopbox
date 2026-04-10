package metrics

import (
	"math"
	"testing"
)

func TestParseContainerName(t *testing.T) {
	cases := []struct {
		name     string
		input    []string
		wantUser string
		wantBox  string
		wantOK   bool
	}{
		{"happy path", []string{"/hopbox-alice-dev"}, "alice", "dev", true},
		{"no slash prefix", []string{"hopbox-bob-staging"}, "bob", "staging", true},
		{"dashed username", []string{"/hopbox-alice-smith-dev"}, "alice-smith", "dev", true},
		{"empty after prefix", []string{"/hopbox-"}, "", "", false},
		{"no box part", []string{"/hopbox-alice"}, "", "", false},
		{"trailing dash", []string{"/hopbox-alice-"}, "", "", false},
		{"not hopbox", []string{"/other-foo-bar"}, "", "", false},
		{"empty names", []string{}, "", "", false},
		{"picks first matching", []string{"/other", "/hopbox-alice-dev"}, "alice", "dev", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, box, ok := parseContainerName(tc.input)
			if user != tc.wantUser || box != tc.wantBox || ok != tc.wantOK {
				t.Errorf("parseContainerName(%v) = (%q, %q, %v); want (%q, %q, %v)",
					tc.input, user, box, ok, tc.wantUser, tc.wantBox, tc.wantOK)
			}
		})
	}
}

func TestCalcCPUPercent(t *testing.T) {
	t.Run("zero deltas returns zero", func(t *testing.T) {
		var s dockerStats
		if got := calcCPUPercent(&s); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("nonzero deltas", func(t *testing.T) {
		var s dockerStats
		s.CPUStats.CPUUsage.TotalUsage = 2000
		s.PreCPUStats.CPUUsage.TotalUsage = 1000
		s.CPUStats.SystemCPUUsage = 20000
		s.PreCPUStats.SystemCPUUsage = 10000
		s.CPUStats.OnlineCPUs = 4
		// (1000/10000) * 4 * 100 = 40.0
		got := calcCPUPercent(&s)
		if math.Abs(got-40.0) > 1e-9 {
			t.Errorf("got %v, want 40.0", got)
		}
	})

	t.Run("falls back to percpu length when OnlineCPUs is zero", func(t *testing.T) {
		var s dockerStats
		s.CPUStats.CPUUsage.TotalUsage = 2000
		s.PreCPUStats.CPUUsage.TotalUsage = 1000
		s.CPUStats.SystemCPUUsage = 20000
		s.PreCPUStats.SystemCPUUsage = 10000
		s.CPUStats.CPUUsage.PercpuUsage = []uint64{1, 2}
		// (1000/10000) * 2 * 100 = 20.0
		got := calcCPUPercent(&s)
		if math.Abs(got-20.0) > 1e-9 {
			t.Errorf("got %v, want 20.0", got)
		}
	})

	t.Run("negative cpu delta returns zero", func(t *testing.T) {
		var s dockerStats
		s.CPUStats.CPUUsage.TotalUsage = 500
		s.PreCPUStats.CPUUsage.TotalUsage = 1000
		s.CPUStats.SystemCPUUsage = 20000
		s.PreCPUStats.SystemCPUUsage = 10000
		s.CPUStats.OnlineCPUs = 4
		if got := calcCPUPercent(&s); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
}
