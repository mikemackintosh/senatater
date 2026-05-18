package store

import (
	"math"
	"testing"
)

func TestEncodeDecodeVec_RoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 3.14159, -2.71828, 1e-7, 1e7}
	b := encodeVec(in)
	if len(b) != 4*len(in) {
		t.Fatalf("encoded length %d, want %d", len(b), 4*len(in))
	}
	out := decodeVec(b)
	if len(out) != len(in) {
		t.Fatalf("decoded length %d, want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("index %d: got %v, want %v", i, out[i], in[i])
		}
	}
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	c := []float32{0, 1, 0}
	d := []float32{-1, 0, 0}

	if got := cosine(a, b, norm(a)); !approx(got, 1) {
		t.Errorf("parallel: got %v, want 1", got)
	}
	if got := cosine(a, c, norm(a)); !approx(got, 0) {
		t.Errorf("orthogonal: got %v, want 0", got)
	}
	if got := cosine(a, d, norm(a)); !approx(got, -1) {
		t.Errorf("opposite: got %v, want -1", got)
	}
}

func TestCosine_ZeroAndMismatched(t *testing.T) {
	zero := []float32{0, 0, 0}
	v := []float32{1, 2, 3}
	if got := cosine(zero, v, norm(zero)); got != 0 {
		t.Errorf("zero query: got %v, want 0", got)
	}
	if got := cosine(v, zero, norm(v)); got != 0 {
		t.Errorf("zero target: got %v, want 0", got)
	}
	if got := cosine(v, []float32{1, 2}, norm(v)); got != 0 {
		t.Errorf("mismatched dims: got %v, want 0", got)
	}
}

func approx(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-5
}
