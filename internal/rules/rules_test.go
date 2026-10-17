package rules

import (
	"path/filepath"
	"testing"
)

func TestExclusionListMatch(t *testing.T) {
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
		{"finding 11: test product name", []string{"Security Test Product", "TestBrand"}, "Security Test Product", true},
		{"finding 11: injection payload in brand", []string{"Anything", "TestBrand'; SELECT 1; --"}, "SELECT 1;", true},
		{"case and repeated spaces ignored", []string{"x", "select  1;"}, "SELECT 1;", true},
		{"punctuation is kept", []string{"Select 1 Kit", ""}, "", false},
		{"apostrophes are fine", []string{"Belt Leather Reversible", "Levi's"}, "", false},
		{"contained anywhere", []string{"New Security Test Product v2", ""}, "Security Test Product", true},
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

func TestNewExclusionListRejectsEmptyTerm(t *testing.T) {
	if _, err := NewExclusionList([]string{"  "}); err == nil {
		t.Error("want error for an empty term")
	}
}

func TestShippedExclusionListLoads(t *testing.T) {
	l, err := LoadExclusionList(filepath.Join("..", "..", "rules", "exclusion-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, hit := l.Match("Security Test Product"); !hit {
		t.Error("shipped list should exclude the known test product")
	}
}
