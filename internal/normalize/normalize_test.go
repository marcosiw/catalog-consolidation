package normalize

import "testing"

func TestText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"finding 4 and 9: accents are removed", "Câmera Canon EOS R6", "camera canon eos r6"},
		{"finding 5: repeated spaces collapse", "Xbox  Series X", "xbox series x"},
		{"finding 6: inch mark as double quote", `Tablet iPad Pro 12.9"`, "tablet ipad pro 12 9"},
		{"finding 6: inch mark as two apostrophes", "Tablet iPad Pro 12.9''", "tablet ipad pro 12 9"},
		{"finding 6: inch mark missing", "Tablet iPad Pro 12.9", "tablet ipad pro 12 9"},
		{"finding 7: brand casing", "BLACK+DECKER", "black decker"},
		{"finding 7: brand casing, other spelling", "Black+Decker", "black decker"},
		{"finding 14: apostrophe is deleted, not spaced", "Levi's", "levis"},
		{"typographic apostrophe", "Levi’s", "levis"},
		{"leading and trailing noise", "  -Kit-  ", "kit"},
		{"hyphenated brand", "TP-Link", "tp link"},
		{"empty", "", ""},
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
	t.Run("finding 10: NULL brand becomes empty string", func(t *testing.T) {
		if got := Brand(nil); got != "" {
			t.Errorf("Brand(nil) = %q, want empty", got)
		}
	})
	t.Run("present brand is normalized", func(t *testing.T) {
		b := "BLACK+DECKER"
		if got := Brand(&b); got != "black decker" {
			t.Errorf("Brand(%q) = %q", b, got)
		}
	})
}
