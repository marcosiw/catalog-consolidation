package similarity

import (
	"math"
	"testing"
)

func TestJaccard(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{"identical", "router wifi 6 tp link", "router wifi 6 tp link", 1},
		{"finding 9: router translated", "roteador wifi 6 tp link", "router wifi 6 tp link", 4.0 / 6},
		{"finding 9: processor translated", "processador amd ryzen 9 7950x", "processor amd ryzen 9 7950x", 4.0 / 6},
		{"most similar distinct same-brand pair in the catalog", "tennis racket adult", "tennis racket bag", 0.5},
		{"long shared suffix does not dominate", "colander stainless steel", "ladle stainless steel", 0.5},
		{"repeated words count once", "set set of 2", "set of 2", 1},
		{"nothing shared", "abc", "xyz", 0},
		{"both empty", "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Jaccard(tt.a, tt.b); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Jaccard(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// The default threshold (0.6) must separate the measured cases: both
// translations above, the closest distinct same-brand products below.
func TestDefaultThresholdSeparatesMeasuredCases(t *testing.T) {
	const threshold = 0.6
	above := [][2]string{
		{"roteador wifi 6 tp link", "router wifi 6 tp link"},
		{"processador amd ryzen 9 7950x", "processor amd ryzen 9 7950x"},
	}
	below := [][2]string{
		{"tennis racket adult", "tennis racket bag"},
		{"colander stainless steel", "ladle stainless steel"},
		{"hockey stick ice", "hockey skates ice"},
	}
	for _, p := range above {
		if s := Jaccard(p[0], p[1]); s < threshold {
			t.Errorf("%q vs %q = %v, want >= %v", p[0], p[1], s, threshold)
		}
	}
	for _, p := range below {
		if s := Jaccard(p[0], p[1]); s >= threshold {
			t.Errorf("%q vs %q = %v, want < %v", p[0], p[1], s, threshold)
		}
	}
}
