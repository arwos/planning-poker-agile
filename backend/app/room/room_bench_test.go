package room

import "testing"

func BenchmarkRoomState(b *testing.B) {
	r, err := New([]float64{1, 2, 3, 5, 8}, []string{"Backend", "Frontend", "QA"})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if err := r.Add(&Participant{ID: string(rune('a' + i)), Name: "Participant", Role: "Backend"}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = r.State()
	}
}

func BenchmarkRoomSnapshotCompatibility(b *testing.B) {
	r, err := New(nil, []string{"Backend"})
	if err != nil {
		b.Fatal(err)
	}
	if err := r.Add(&Participant{ID: "p", Name: "Participant", Role: "Backend"}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = r.Snapshot()
	}
}
