package room

import (
	"math"
	"strings"
	"testing"
	"time"
)

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

func TestSkipVoteCountsAsSubmittedWithoutAffectingResults(t *testing.T) {
	r, err := New([]float64{1, 3, 5}, []string{"Backend", "QA"})
	if err != nil {
		t.Fatal(err)
	}
	skipped := &Participant{ID: "skipped", Name: "Ann", Role: "Backend"}
	voter := &Participant{ID: "voter", Name: "Bob", Role: "QA"}
	if err := r.Add(skipped); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(voter); err != nil {
		t.Fatal(err)
	}
	if done, err := r.SkipVote(skipped.ID); err != nil || done {
		t.Fatalf("skip vote: done=%v err=%v", done, err)
	}
	if !skipped.Submitted || !skipped.Skipped || skipped.Selected != nil {
		t.Fatalf("skip state was not stored: submitted=%v skipped=%v selected=%v", skipped.Submitted, skipped.Skipped, skipped.Selected)
	}
	if _, ok := r.Votes[skipped.ID]; ok {
		t.Fatal("skipped vote must not be included in votes")
	}

	done, err := r.Vote(voter.ID, 5, true)
	if err != nil || !done {
		t.Fatalf("final vote: done=%v err=%v", done, err)
	}
	if !r.Revealed || r.Average != 5 {
		t.Fatalf("unexpected results: revealed=%v average=%v", r.Revealed, r.Average)
	}
	if _, ok := r.RoleAverages["Backend"]; ok {
		t.Fatal("skipped role must not have an average")
	}
	if r.RoleAverages["QA"] != 5 {
		t.Fatalf("unexpected role averages: %#v", r.RoleAverages)
	}
	state := r.Snapshot()
	if state["hasVotes"] != true {
		t.Fatal("results should report that at least one vote was counted")
	}
}

func TestAllParticipantsCanSkipVote(t *testing.T) {
	r, err := New([]float64{1, 3, 5}, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	first := &Participant{ID: "first", Name: "Ann", Role: "Backend"}
	second := &Participant{ID: "second", Name: "Bob", Role: "Backend"}
	_ = r.Add(first)
	_ = r.Add(second)
	if done, err := r.SkipVote(first.ID); err != nil || done {
		t.Fatalf("first skip: done=%v err=%v", done, err)
	}
	if done, err := r.SkipVote(second.ID); err != nil || !done {
		t.Fatalf("second skip: done=%v err=%v", done, err)
	}
	if !r.Revealed || len(r.Votes) != 0 || r.Snapshot()["hasVotes"] != false {
		t.Fatalf("all-skipped round has unexpected state: revealed=%v votes=%d state=%#v", r.Revealed, len(r.Votes), r.Snapshot())
	}
}

func TestSubmittedVoteCanBeChangedBeforeReveal(t *testing.T) {
	r, err := New([]float64{1, 3, 5}, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	first := &Participant{ID: "first", Name: "Ann", Role: "Backend"}
	second := &Participant{ID: "second", Name: "Bob", Role: "Backend"}
	if err := r.Add(first); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(second); err != nil {
		t.Fatal(err)
	}
	if done, err := r.Vote(first.ID, 1, true); err != nil || done {
		t.Fatalf("first vote: done=%v err=%v", done, err)
	}
	if done, err := r.Vote(first.ID, 3, true); err != nil || done {
		t.Fatalf("changed vote: done=%v err=%v", done, err)
	}
	if got := r.Votes[first.ID]; got != 3 {
		t.Fatalf("stored vote=%v, want 3", got)
	}
	if !first.Submitted || first.Selected == nil || *first.Selected != 3 {
		t.Fatalf("participant vote was not updated: submitted=%v selected=%v", first.Submitted, first.Selected)
	}
	if r.Revealed {
		t.Fatal("room revealed before all participants voted")
	}
	if done, err := r.Vote(second.ID, 5, true); err != nil || !done {
		t.Fatalf("second vote: done=%v err=%v", done, err)
	}
	if r.Average != 4 {
		t.Fatalf("average=%v, want 4", r.Average)
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
	if lead.Skipped || guest.Skipped {
		t.Fatal("reset did not clear skipped state")
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

func TestReconnectReplacesParticipantAndProtectsNewConnection(t *testing.T) {
	r, _ := New(nil, []string{"Backend"})
	first := &Participant{ID: "first", Name: "Ann", Role: "Backend"}
	replaced, reconnectToken, err := r.AddConnectionWithToken(first, "client", "", "", "connection-1")
	if err != nil || replaced || reconnectToken == "" {
		t.Fatalf("first connection: replaced=%v token=%q err=%v", replaced, reconnectToken, err)
	}
	second := &Participant{ID: "second", Name: "Ann", Role: "Backend"}
	if replaced, returnedToken, err := r.AddConnectionWithToken(second, "client", reconnectToken, "", "connection-2"); err != nil || !replaced || returnedToken != reconnectToken {
		t.Fatalf("reconnect: replaced=%v token=%q err=%v", replaced, returnedToken, err)
	}
	if second.ID != first.ID {
		t.Fatalf("participant id changed: first=%q second=%q", first.ID, second.ID)
	}
	if got := len(r.Snapshot()["participants"].([]map[string]any)); got != 1 {
		t.Fatalf("participants=%d", got)
	}
	if r.RemoveConnection(first.ID, "connection-1") {
		t.Fatal("stale connection removed the replacement")
	}
	if !r.RemoveConnection(second.ID, "connection-2") {
		t.Fatal("active connection was not removed")
	}
}

func TestReconnectAfterDisconnectRestoresIdentityWithoutStaleVote(t *testing.T) {
	r, _ := New([]float64{1, 3}, []string{"Backend"})
	first := &Participant{ID: "first", Name: "Ann", Role: "Backend"}
	other := &Participant{ID: "other", Name: "Bob", Role: "Backend"}
	_, token, err := r.AddConnectionWithToken(first, "client", "", "", "connection-1")
	if err != nil || token == "" {
		t.Fatalf("first connection: token=%q err=%v", token, err)
	}
	if err := r.Add(other); err != nil {
		t.Fatalf("second connection: %v", err)
	}
	if _, err := r.Vote(first.ID, 1, true); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if !r.RemoveConnection(first.ID, "connection-1") {
		t.Fatal("connection was not removed")
	}
	if r.IsEmpty() {
		t.Fatal("active participant must keep the room alive")
	}
	state := r.State()
	if len(state.Participants) != 1 || state.HasVotes {
		t.Fatalf("disconnected participant leaked into state: %#v", state)
	}

	reconnected := &Participant{ID: "second", Name: "Ann", Role: "Backend"}
	replaced, returnedToken, err := r.AddConnectionWithToken(reconnected, "client", token, "", "connection-2")
	if err != nil || !replaced || returnedToken != token {
		t.Fatalf("reconnect: replaced=%v token=%q err=%v", replaced, returnedToken, err)
	}
	if reconnected.ID != first.ID {
		t.Fatalf("participant identity changed: first=%q second=%q", first.ID, reconnected.ID)
	}
	if len(r.State().Participants) != 2 {
		t.Fatal("reconnected participant is not active")
	}
}

func TestRoomOwnerRegainsLeadAfterReturning(t *testing.T) {
	r, err := newRoom(nil, []string{"Backend"}, 0, 0, "owner-token")
	if err != nil {
		t.Fatal(err)
	}
	guest := &Participant{ID: "guest", Name: "Guest", Role: "Backend"}
	if replaced, err := r.AddConnection(guest, "guest-client", "", "guest-1"); err != nil || replaced {
		t.Fatalf("guest connection: replaced=%v err=%v", replaced, err)
	}
	if !guest.Lead {
		t.Fatal("first participant should temporarily be lead")
	}

	owner := &Participant{ID: "owner", Name: "Owner", Role: "Backend"}
	if replaced, err := r.AddConnection(owner, "owner-client", "owner-token", "owner-1"); err != nil || replaced {
		t.Fatalf("owner connection: replaced=%v err=%v", replaced, err)
	}
	if !owner.Lead || guest.Lead {
		t.Fatal("owner should become lead when joining")
	}
	if !r.RemoveConnection(owner.ID, "owner-1") {
		t.Fatal("owner connection was not removed")
	}
	if !guest.Lead {
		t.Fatal("lead should transfer after owner leaves")
	}

	returningOwner := &Participant{ID: "owner-return", Name: "Owner", Role: "Backend"}
	if replaced, err := r.AddConnection(returningOwner, "owner-client", "owner-token", "owner-2"); err != nil || !replaced {
		t.Fatalf("owner return: replaced=%v err=%v", replaced, err)
	}
	if !returningOwner.Lead || guest.Lead {
		t.Fatal("returning owner should regain lead")
	}
}

func TestParticipantNameHasReasonableLimit(t *testing.T) {
	r, _ := New(nil, []string{"Backend"})
	if err := r.Add(&Participant{ID: "p", Name: "12345678901234567890123456789012345678901"}); err == nil {
		t.Fatal("expected long name to be rejected")
	}
}

func TestRoomRejectsInvalidModelValues(t *testing.T) {
	tests := []struct {
		name  string
		cards []float64
		roles []string
	}{
		{name: "negative card", cards: []float64{-1}, roles: []string{"Backend"}},
		{name: "nan card", cards: []float64{math.NaN()}, roles: []string{"Backend"}},
		{name: "infinite card", cards: []float64{math.Inf(1)}, roles: []string{"Backend"}},
		{name: "duplicate role", cards: []float64{1}, roles: []string{"Backend", "backend"}},
		{name: "long role", cards: []float64{1}, roles: []string{strings.Repeat("x", maxRoleNameLength+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cards, tt.roles); err == nil {
				t.Fatal("expected invalid room model")
			}
		})
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

func TestReconnectRequiresTokenForNetworkAPI(t *testing.T) {
	r, err := New(nil, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	first := &Participant{ID: "first", Name: "Ann", Role: "Backend"}
	_, token, err := r.AddConnectionWithToken(first, "client", "", "", "connection-1")
	if err != nil || token == "" {
		t.Fatalf("first connection: token=%q err=%v", token, err)
	}
	second := &Participant{ID: "second", Name: "Ann", Role: "Backend"}
	if _, _, err := r.AddConnectionWithToken(second, "client", "wrong", "", "connection-2"); err != ErrInvalidReconnect {
		t.Fatalf("wrong token error=%v, want %v", err, ErrInvalidReconnect)
	}
	if got := len(r.Participants); got != 1 {
		t.Fatalf("participants after rejected reconnect=%d, want 1", got)
	}
	if replaced, returned, err := r.AddConnectionWithToken(second, "client", token, "", "connection-2"); err != nil || !replaced || returned != token {
		t.Fatalf("valid reconnect: replaced=%v token=%q err=%v", replaced, returned, err)
	}
}

func TestRemovingParticipantClearsUnrevealedVote(t *testing.T) {
	r, err := New([]float64{1, 3}, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	p := &Participant{ID: "p", Name: "Ann", Role: "Backend"}
	other := &Participant{ID: "other", Name: "Bob", Role: "Backend"}
	if err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(other); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Vote(p.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	r.Remove(p.ID)
	if len(r.Votes) != 0 || r.Snapshot()["hasVotes"] != false {
		t.Fatalf("stale vote remains: votes=%v state=%v", r.Votes, r.Snapshot())
	}
}

func TestRegistryReservationExpiresAndActivatesAtomically(t *testing.T) {
	registry := NewRegistry(1, 2, 2, 2)
	reservation, err := registry.ReserveWithOwner([]float64{1}, []string{"Backend"}, "owner", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := registry.PublicState(reservation.ID); err != ErrNotFound {
		t.Fatalf("expired reservation error=%v, want %v", err, ErrNotFound)
	}
	reservation, err = registry.ReserveWithOwner([]float64{1}, []string{"Backend"}, "owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	p := &Participant{ID: "p", Name: "Ann", Role: "Backend"}
	r, _, _, err := registry.Join(reservation.ID, p, "client", "", "owner", "connection")
	if err != nil || r == nil {
		t.Fatalf("join reservation: room=%v err=%v", r, err)
	}
	if _, err := registry.PublicState(reservation.ID); err != nil {
		t.Fatalf("active room disappeared after join: %v", err)
	}
}

func FuzzNewRoomDoesNotPanic(f *testing.F) {
	f.Add([]byte("cards"), "Backend")
	f.Add([]byte(""), "")
	f.Fuzz(func(t *testing.T, data []byte, role string) {
		cards := []float64{float64(len(data))}
		if len(data) > 0 && data[0] == 0 {
			cards[0] = math.NaN()
		}
		_, _ = New(cards, []string{role})
	})
}
