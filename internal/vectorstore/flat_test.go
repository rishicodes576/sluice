package vectorstore

import "testing"

func TestFlatAddSearchDeleteLen(t *testing.T) {
	f := NewFlat(3)
	_ = f.Add("a", []float64{1, 0, 0})
	_ = f.Add("b", []float64{0, 1, 0})
	_ = f.Add("a", []float64{1, 0, 0}) // update existing, len stays 2
	if f.Len() != 2 {
		t.Fatalf("len = %d, want 2", f.Len())
	}
	got := f.Search([]float64{1, 0, 0}, 1)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("search = %+v, want a", got)
	}
	f.Delete("a")
	if f.Len() != 1 {
		t.Fatalf("len after delete = %d, want 1", f.Len())
	}
	for _, m := range f.Search([]float64{1, 0, 0}, 2) {
		if m.ID == "a" {
			t.Fatal("deleted id returned by search")
		}
	}
}
