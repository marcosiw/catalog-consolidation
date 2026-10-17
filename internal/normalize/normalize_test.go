package normalize

import "testing"

func TestText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"removes_accents", "Câmera Canon EOS R6", "camera canon eos r6"},
		{"collapses_repeated_spaces", "Xbox  Series X", "xbox series x"},
		{"drops_inch_mark_written_as_double_quote", `Tablet iPad Pro 12.9"`, "tablet ipad pro 12 9"},
		{"drops_inch_mark_written_as_two_apostrophes", "Tablet iPad Pro 12.9''", "tablet ipad pro 12 9"},
		{"matches_name_without_inch_mark", "Tablet iPad Pro 12.9", "tablet ipad pro 12 9"},
		{"lowercases_upper_case_brand", "BLACK+DECKER", "black decker"},
		{"lowercases_mixed_case_brand", "Black+Decker", "black decker"},
		{"deletes_apostrophe_instead_of_spacing_it", "Levi's", "levis"},
		{"deletes_typographic_apostrophe", "Levi’s", "levis"},
		{"trims_leading_and_trailing_punctuation", "  -Kit-  ", "kit"},
		{"turns_hyphen_into_space", "TP-Link", "tp link"},
		{"keeps_empty_input_empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBrand(t *testing.T) {
	t.Run("returns_empty_string_for_null_brand", func(t *testing.T) {
		if got := Brand(nil); got != "" {
			t.Errorf("Brand(nil) = %q, want empty", got)
		}
	})
	t.Run("normalizes_present_brand", func(t *testing.T) {
		b := "BLACK+DECKER"
		if got := Brand(&b); got != "black decker" {
			t.Errorf("Brand(%q) = %q", b, got)
		}
	})
}
