package engage

import (
	"testing"
)

func TestDefaultPaywallTrigger(t *testing.T) {
	tests := []struct {
		name string
		data *EngagementData
		want string
	}{
		{"power user", &EngagementData{DaysActive: 20, TotalLogs: 60}, "power_user"},
		{"milestone", &EngagementData{GoalsCompletedTotal: 15, PaywallShownCount: 1}, "milestone"},
		{"new user", &EngagementData{DaysActive: 2, TotalLogs: 5}, ""},
		{"already shown", &EngagementData{GoalsCompletedTotal: 15, PaywallShownCount: 5}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultPaywallTrigger(tt.data)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
