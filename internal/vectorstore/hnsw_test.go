package vectorstore

import (
	"math"
	"math/rand"
	"testing"
)

func randVec(rng *rand.Rand, dim int) []float64 {
	v := make([]float64, dim)
	var norm float64
	for i := range v {
		v[i] = rng.NormFloat64()
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	return v
}

// TestHNSWRecall checks that the approximate index agrees with exact search on
// the top-1 nearest neighbour for the large majority of queries.
func TestHNSWRecall(t *testing.T) {
	const dim, n, queries = 32, 800, 100
	rng := rand.New(rand.NewSource(42))
	flat := NewFlat(dim)
	h := NewHNSW(dim, WithSeed(7), WithM(16), WithEfConstruction(200))

	ids := make([][]float64, n)
	for i := 0; i < n; i++ {
		v := randVec(rng, dim)
		ids[i] = v
		id := itoa(i)
		if err := flat.Add(id, v); err != nil {
			t.Fatal(err)
		}
		if err := h.Add(id, v); err != nil {
			t.Fatal(err)
		}
	}
	if h.Len() != n {
		t.Fatalf("len = %d, want %d", h.Len(), n)
	}

	hits := 0
	for q := 0; q < queries; q++ {
		query := randVec(rng, dim)
		want := flat.Search(query, 1)
		got := h.Search(query, 1)
		if len(got) > 0 && len(want) > 0 && got[0].ID == want[0].ID {
			hits++
		}
	}
	recall := float64(hits) / queries
	if recall < 0.90 {
		t.Fatalf("recall@1 = %.2f, want >= 0.90", recall)
	}
}

func TestHNSWExactMatch(t *testing.T) {
	h := NewHNSW(4, WithSeed(1))
	vecs := map[string][]float64{
		"a": {1, 0, 0, 0}, "b": {0, 1, 0, 0}, "c": {0, 0, 1, 0},
	}
	for id, v := range vecs {
		_ = h.Add(id, v)
	}
	got := h.Search([]float64{1, 0, 0, 0}, 1)
	if len(got) == 0 || got[0].ID != "a" || math.Abs(got[0].Similarity-1) > 1e-9 {
		t.Fatalf("expected exact match on a, got %+v", got)
	}
}

func TestHNSWDelete(t *testing.T) {
	h := NewHNSW(4, WithSeed(1))
	_ = h.Add("a", []float64{1, 0, 0, 0})
	_ = h.Add("b", []float64{0, 1, 0, 0})
	h.Delete("a")
	if h.Len() != 1 {
		t.Fatalf("len after delete = %d, want 1", h.Len())
	}
	got := h.Search([]float64{1, 0, 0, 0}, 2)
	for _, m := range got {
		if m.ID == "a" {
			t.Fatal("deleted id 'a' returned from search")
		}
	}
}

func TestFlatDimMismatch(t *testing.T) {
	f := NewFlat(3)
	if err := f.Add("x", []float64{1, 2}); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func BenchmarkHNSWSearch(b *testing.B) {
	const dim, n = 64, 5000
	rng := rand.New(rand.NewSource(1))
	h := NewHNSW(dim, WithSeed(1))
	for i := 0; i < n; i++ {
		_ = h.Add(itoa(i), randVec(rng, dim))
	}
	q := randVec(rng, dim)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Search(q, 5)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
