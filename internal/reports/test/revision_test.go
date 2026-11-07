package test

import (
	"math"
	"testing"

	"cpa-usage-ranking/internal/reports"
)

func TestRevisionDecisions(t *testing.T) {
	var one, two [32]byte
	one[0], two[0] = 1, 2
	current := reports.Version{Revision: 8, Digest: one}
	for _, tc := range []struct {
		name     string
		current  *reports.Version
		incoming reports.Version
		want     reports.Decision
	}{
		{"initial zero", nil, reports.Version{Revision: 0, Digest: one}, reports.Replace},
		{"identical", &current, reports.Version{Revision: 8, Digest: one}, reports.Confirm},
		{"same revision changed", &current, reports.Version{Revision: 8, Digest: two}, reports.Conflict},
		{"old same digest", &current, reports.Version{Revision: 7, Digest: one}, reports.Conflict},
		{"old changed", &current, reports.Version{Revision: 7, Digest: two}, reports.Conflict},
		{"new same digest", &current, reports.Version{Revision: 9, Digest: one}, reports.Replace},
		{"jump", &current, reports.Version{Revision: math.MaxInt64, Digest: two}, reports.Replace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reports.DecideVersion(tc.current, tc.incoming); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
