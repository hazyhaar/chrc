package catalog

import (
	"context"
	"testing"
)

func TestPopulate_InsertsForCategory(t *testing.T) {
	// WHAT: Populate inserts all sources from a known category.
	// WHY: Seed catalog must populate a space with the right sources.
	ctx := context.Background()

	var inserted []*SourceInput
	addSource := func(ctx context.Context, s *SourceInput) error {
		inserted = append(inserted, s)
		return nil
	}

	count, err := Populate(ctx, addSource, "tech")
	if err != nil {
		t.Fatalf("populate: %v", err)
	}

	defs, _ := Sources("tech")
	if count != len(defs) {
		t.Errorf("count: got %d, want %d", count, len(defs))
	}
	if len(inserted) != len(defs) {
		t.Fatalf("inserted: got %d", len(inserted))
	}

	// Verify first source.
	first := inserted[0]
	if first.Name != "Hacker News" {
		t.Errorf("first name: got %q", first.Name)
	}
	if first.SourceType != "rss" {
		t.Errorf("first type: got %q", first.SourceType)
	}
	if !first.Enabled {
		t.Error("should be enabled")
	}
}

func TestPopulate_SkipsDuplicateURLs(t *testing.T) {
	// WHAT: Second call to Populate skips already-inserted sources.
	// WHY: Re-running catalog should not create duplicates.
	ctx := context.Background()

	seen := map[string]bool{}
	addSource := func(ctx context.Context, s *SourceInput) error {
		if seen[s.URL] {
			return &duplicateError{}
		}
		seen[s.URL] = true
		return nil
	}

	count1, _ := Populate(ctx, addSource, "academic")
	count2, _ := Populate(ctx, addSource, "academic")

	if count1 == 0 {
		t.Error("first populate should insert sources")
	}
	if count2 != 0 {
		t.Errorf("second populate should insert 0, got %d", count2)
	}
}

func TestPopulate_UnknownCategory(t *testing.T) {
	// WHAT: Unknown category returns an error.
	// WHY: Typos should be caught early.
	_, err := Populate(context.Background(), nil, "nonexistent")
	if err == nil {
		t.Error("expected error for unknown category")
	}
}

func TestCategories_NotEmpty(t *testing.T) {
	// WHAT: Categories() returns known category names.
	// WHY: Must have at least the 5 defined categories.
	cats := Categories()
	if len(cats) < 5 {
		t.Errorf("categories: got %d, want >= 5", len(cats))
	}
}

func TestSources_HasEntries(t *testing.T) {
	// WHAT: Each category has at least one source.
	// WHY: Empty categories are useless.
	for _, cat := range Categories() {
		defs, ok := Sources(cat)
		if !ok {
			t.Errorf("category %q not found", cat)
			continue
		}
		if len(defs) == 0 {
			t.Errorf("category %q has no sources", cat)
		}
	}
}

func TestPopulateSearchEngines_InsertsAll(t *testing.T) {
	// WHAT: PopulateSearchEngines inserts the default search engines.
	// WHY: Search engines must be seeded for question runner to work.
	ctx := context.Background()

	var inserted []*SearchEngineInput
	insertFn := func(ctx context.Context, e *SearchEngineInput) error {
		inserted = append(inserted, e)
		return nil
	}

	count, err := PopulateSearchEngines(ctx, insertFn)
	if err != nil {
		t.Fatalf("populate: %v", err)
	}
	if count != 4 {
		t.Errorf("count: got %d, want 4", count)
	}
	if len(inserted) != 4 {
		t.Fatalf("inserted: got %d", len(inserted))
	}

	// Verify enabled engines (brave_api, github_search) and disabled stubs.
	for _, e := range inserted {
		switch e.ID {
		case "brave_api", "github_search":
			if !e.Enabled {
				t.Errorf("%s should be enabled", e.ID)
			}
			if e.Strategy != "api" {
				t.Errorf("%s strategy: got %q, want api", e.ID, e.Strategy)
			}
		default:
			if e.Enabled {
				t.Errorf("%s should be disabled (generic stub)", e.ID)
			}
			if e.Strategy != "generic" {
				t.Errorf("%s strategy: got %q, want generic", e.ID, e.Strategy)
			}
		}
	}
}

func TestPopulateSearchEngines_SkipsDuplicates(t *testing.T) {
	// WHAT: Second call skips already-inserted engines.
	// WHY: Re-seeding must not create duplicates.
	ctx := context.Background()

	seen := map[string]bool{}
	insertFn := func(ctx context.Context, e *SearchEngineInput) error {
		if seen[e.ID] {
			return &duplicateError{}
		}
		seen[e.ID] = true
		return nil
	}

	c1, _ := PopulateSearchEngines(ctx, insertFn)
	c2, _ := PopulateSearchEngines(ctx, insertFn)

	if c1 != 4 {
		t.Errorf("first: got %d, want 4", c1)
	}
	if c2 != 0 {
		t.Errorf("second: got %d, want 0", c2)
	}
}

type duplicateError struct{}

func (e *duplicateError) Error() string { return "duplicate" }
