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
		{"scores_identical_names_as_one", "router wifi 6 tp link", "router wifi 6 tp link", 1},
		{"scores_router_translation_above_threshold", "roteador wifi 6 tp link", "router wifi 6 tp link", 4.0 / 6},
		{"scores_processor_translation_above_threshold", "processador amd ryzen 9 7950x", "processor amd ryzen 9 7950x", 4.0 / 6},
		{"scores_closest_distinct_same_brand_pair_at_half", "tennis racket adult", "tennis racket bag", 0.5},
		{"ignores_length_of_shared_suffix", "colander stainless steel", "ladle stainless steel", 0.5},
		{"counts_repeated_words_once", "set set of 2", "set of 2", 1},
		{"scores_disjoint_names_as_zero", "abc", "xyz", 0},
		{"scores_two_empty_names_as_zero", "", "", 0},
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
func TestJaccard_DefaultThresholdSeparatesMeasuredCases(t *testing.T) {
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
