package embed

import (
	"context"
	"math"
	"testing"
)

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func TestLocalEmbedDeterministicAndNormalised(t *testing.T) {
	e := NewLocal(128)
	v, err := e.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v[0]) != 128 {
		t.Fatalf("dim = %d", len(v[0]))
	}
	var norm float64
	for _, x := range v[0] {
		norm += x * x
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-9 {
		t.Fatalf("vector not unit-normalised: |v| = %v", math.Sqrt(norm))
	}
}

func TestLocalEmbedSimilarNearerThanDissimilar(t *testing.T) {
	e := NewLocal(256)
	ctx := context.Background()
	base, _ := e.Embed(ctx, []string{"the weather in london is rainy today"})
	near, _ := e.Embed(ctx, []string{"the weather in london is rainy today"})
	far, _ := e.Embed(ctx, []string{"quantum entanglement in particle physics"})

	simNear := cosine(base[0], near[0])
	simFar := cosine(base[0], far[0])
	if simNear <= simFar {
		t.Fatalf("expected near (%.3f) > far (%.3f)", simNear, simFar)
	}
}
