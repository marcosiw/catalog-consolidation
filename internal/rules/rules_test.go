package rules

import (
	"path/filepath"
	"testing"
)

func TestExclusionList_Match(t *testing.T) {
	l, err := NewExclusionList([]string{"Security Test Product", "SELECT 1;"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		fields   []string
		wantTerm string
		wantHit  bool
	}{
		{"rejects_known_test_product_name", []string{"Security Test Product", "TestBrand"}, "Security Test Product", true},
		{"rejects_listed_term_inside_brand", []string{"Anything", "TestBrand'; SELECT 1; --"}, "SELECT 1;", true},
		{"ignores_case_and_repeated_spaces", []string{"x", "select  1;"}, "SELECT 1;", true},
		{"requires_punctuation_of_the_term", []string{"Select 1 Kit", ""}, "", false},
		{"accepts_brand_with_apostrophe", []string{"Belt Leather Reversible", "Levi's"}, "", false},
		{"matches_term_anywhere_in_the_text", []string{"New Security Test Product v2", ""}, "Security Test Product", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term, hit := l.Match(tt.fields...)
			if hit != tt.wantHit || term != tt.wantTerm {
				t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tt.fields, term, hit, tt.wantTerm, tt.wantHit)
			}
		})
	}
}

func TestNewExclusionList_RejectsEmptyTerm(t *testing.T) {
	if _, err := NewExclusionList([]string{"  "}); err == nil {
		t.Error("want error for an empty term")
	}
}

func TestLoadExclusionList_ShippedFileExcludesTestProduct(t *testing.T) {
	l, err := LoadExclusionList(filepath.Join("..", "..", "rules", "exclusion-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if term, hit := l.Match("Security Test Product"); !hit {
		t.Errorf("Match(%q) = (%q, %v), want a hit", "Security Test Product", term, hit)
	}
}
