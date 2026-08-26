package room

import "testing"

func TestVotesRevealAfterAllVotersSubmit(t *testing.T) {
	r, err := New([]float64{1, 3, 5}, []string{"Backend", "QA"})
	if err != nil {
		t.Fatal(err)
	}
	a := &Participant{ID: "a", Name: "Ann", Role: "Backend"}
	b := &Participant{ID: "b", Name: "Bob", Role: "QA"}
	observer := &Participant{ID: "c", Name: "Cam"}
	if r.Add(a) != nil || r.Add(b) != nil || r.Add(observer) != nil {
		t.Fatal("add participant")
	}
	if done, _ := r.Vote("a", 3, true); done {
		t.Fatal("revealed before all voters submitted")
	}
	done, err := r.Vote("b", 5, true)
	if err != nil || !done {
		t.Fatal("did not reveal after second vote")
	}
	if !r.Revealed || r.Average != 4 {
		t.Fatalf("unexpected result: revealed=%v avg=%v", r.Revealed, r.Average)
	}
	if r.RoleAverages["Backend"] != 3 || r.RoleAverages["QA"] != 5 {
		t.Fatalf("unexpected role averages: %#v", r.RoleAverages)
	}
}

func TestOnlyLeadCanReset(t *testing.T) {
	r, _ := New(nil, []string{"Backend"})
	lead := &Participant{ID: "lead", Name: "Lead", Role: "Backend"}
	guest := &Participant{ID: "guest", Name: "Guest", Role: "Backend"}
	_ = r.Add(lead)
	_ = r.Add(guest)
	_, _ = r.Vote("lead", 1, true)
	_, _ = r.Vote("guest", 2, true)
	if r.Reset("guest") == nil {
		t.Fatal("guest reset should fail")
	}
	if err := r.Reset("lead"); err != nil {
		t.Fatal(err)
	}
	if r.Revealed || len(r.Votes) != 0 {
		t.Fatal("reset did not clear round")
	}
}

func TestLeadTransfersWhenLeadLeaves(t *testing.T) {
	r, _ := New(nil, []string{"Backend"})
	lead := &Participant{ID: "lead", Name: "Lead"}
	next := &Participant{ID: "next", Name: "Next"}
	_ = r.Add(lead)
	_ = r.Add(next)
	if r.Remove("lead") {
		t.Fatal("room should still contain a participant")
	}
	if !next.Lead {
		t.Fatal("remaining participant should become lead")
	}
}

func TestParticipantNameHasReasonableLimit(t *testing.T) {
	r, _ := New(nil, []string{"Backend"})
	if err := r.Add(&Participant{ID: "p", Name: "12345678901234567890123456789012345678901"}); err == nil {
		t.Fatal("expected long name to be rejected")
	}
}

func TestRoomRequiresVotingRole(t *testing.T) {
	if _, err := New(nil, nil); err == nil {
		t.Fatal("expected room without roles to be rejected")
	}
}

func TestRegistryRespectsRoomCapacity(t *testing.T) {
	registry := NewRegistry(1)
	if _, err := registry.Create(nil, []string{"Backend"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create(nil, []string{"QA"}); err != ErrCapacity {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryRespectsRoleAndCardLimits(t *testing.T) {
	registry := NewRegistry(0, 2, 2)
	if _, err := registry.Create([]float64{1, 2, 3}, []string{"Backend"}); err == nil {
		t.Fatal("expected card limit error")
	}
	if _, err := registry.Create([]float64{1}, []string{"Backend", "QA", "Analytic"}); err == nil {
		t.Fatal("expected role limit error")
	}
}
