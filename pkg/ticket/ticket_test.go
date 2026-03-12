package ticket

import (
	"testing"
	"time"
)

func TestValidateType(t *testing.T) {
	valid := []TicketType{TypeTask, TypeFeature, TypeBug, TypeEpic, TypeChore}
	for _, tt := range valid {
		if err := ValidateType(tt); err != nil {
			t.Errorf("ValidateType(%q) = %v, want nil", tt, err)
		}
	}

	invalid := []TicketType{"", "story", "TASK"}
	for _, tt := range invalid {
		if err := ValidateType(tt); err == nil {
			t.Errorf("ValidateType(%q) = nil, want error", tt)
		}
	}
}

func TestValidatePriority(t *testing.T) {
	for p := 0; p <= 4; p++ {
		if err := ValidatePriority(p); err != nil {
			t.Errorf("ValidatePriority(%d) = %v, want nil", p, err)
		}
	}

	invalid := []int{-1, 5, 100, -50}
	for _, p := range invalid {
		if err := ValidatePriority(p); err == nil {
			t.Errorf("ValidatePriority(%d) = nil, want error", p)
		}
	}
}

func TestTicketValidate(t *testing.T) {
	base := func() *Ticket {
		return &Ticket{
			ID:       "t-abc1",
			Stage:    StageTriage,
			Type:     TypeTask,
			Priority: 2,
			Created:  time.Now(),
			Deps:     []string{},
			Links:    []string{},
		}
	}

	// Valid ticket passes.
	if err := base().Validate(); err != nil {
		t.Fatalf("valid ticket: %v", err)
	}

	// Missing ID.
	tk := base()
	tk.ID = ""
	if err := tk.Validate(); err == nil {
		t.Error("empty ID should fail validation")
	}

	// Missing stage.
	tk = base()
	tk.Stage = ""
	if err := tk.Validate(); err == nil {
		t.Error("empty stage should fail validation")
	}

	// Bad stage.
	tk = base()
	tk.Stage = "nope"
	if err := tk.Validate(); err == nil {
		t.Error("invalid stage should fail validation")
	}

	// Bad type.
	tk = base()
	tk.Type = "nope"
	if err := tk.Validate(); err == nil {
		t.Error("invalid type should fail validation")
	}

	// Bad priority.
	tk = base()
	tk.Priority = 9
	if err := tk.Validate(); err == nil {
		t.Error("invalid priority should fail validation")
	}
}
