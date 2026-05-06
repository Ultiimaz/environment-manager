package iac

import (
	"strings"
	"testing"
)

func TestCollectClaimedDomains_Empty(t *testing.T) {
	got := CollectClaimedDomains(nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestCollectClaimedDomains_FlattensProdAndPreview(t *testing.T) {
	configs := map[string]*Config{
		"projA": {
			ProjectName: "projA",
			Domains: Domains{
				Prod:    []string{"a.com", "www.a.com"},
				Preview: PreviewDomains{Pattern: "{branch}.a.com"},
			},
		},
		"projB": {
			ProjectName: "projB",
			Domains:     Domains{Prod: []string{"b.com"}},
		},
	}
	got := CollectClaimedDomains(configs)
	expectations := map[string]string{
		"a.com":          "projA",
		"www.a.com":      "projA",
		"{branch}.a.com": "projA",
		"b.com":          "projB",
	}
	if len(got) != len(expectations) {
		t.Errorf("got %d entries, want %d: %+v", len(got), len(expectations), got)
	}
	for d, want := range expectations {
		if got[d] != want {
			t.Errorf("domain %q: got owner %q, want %q", d, got[d], want)
		}
	}
}

func TestCheckDomainConflict_NoConflict(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"b.com", "c.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err != nil {
		t.Errorf("expected no conflict, got %v", err)
	}
}

func TestCheckDomainConflict_ConflictWithProd(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com", "shared.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"shared.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "shared.com") || !strings.Contains(err.Error(), "projA") {
		t.Errorf("error should name the conflicting domain + owner, got %q", err.Error())
	}
}

func TestCheckDomainConflict_ConflictWithPreviewPattern(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Preview: PreviewDomains{Pattern: "{branch}.a.com"}}},
	}
	candidate := &Config{Domains: Domains{Preview: PreviewDomains{Pattern: "{branch}.a.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err == nil {
		t.Fatal("expected conflict on preview pattern")
	}
}

func TestCheckDomainConflict_SameProjectIsNotAConflict(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"a.com"}}}
	// projectID is projA — re-saving its own config shouldn't conflict with itself.
	err := CheckDomainConflict(candidate, "projA", existing)
	if err != nil {
		t.Errorf("self-reuse should not conflict, got %v", err)
	}
}
