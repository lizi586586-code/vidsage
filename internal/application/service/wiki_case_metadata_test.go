package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestReconcileCasePageMetadata_AddsAndRemovesSourceScopedClassification(t *testing.T) {
	page := &types.WikiPage{
		PageType: types.WikiPageTypeConcept,
		PageMetadata: types.JSON(`{
			"domain": "training",
			"sub_type": "case",
			"sub_type_version": "v1",
			"case_source_refs": ["source-a"]
		}`),
	}

	changed := reconcileCasePageMetadata(page, []SlugUpdate{
		{
			Type:      types.WikiPageTypeConcept,
			SourceRef: "source-b",
			Item: extractedItem{
				Metadata: extractedItemMetadata{SubType: wikiCaseSubType},
			},
		},
		{
			Type:      "retract",
			SourceRef: "source-a",
		},
	})
	if !changed {
		t.Fatal("expected metadata to change")
	}

	var got map[string]any
	if err := json.Unmarshal(page.PageMetadata, &got); err != nil {
		t.Fatalf("decode page metadata: %v", err)
	}
	if got["domain"] != "training" {
		t.Fatalf("unrelated metadata was not preserved: %v", got)
	}
	if got["sub_type"] != wikiCaseSubType || got["sub_type_version"] != wikiCaseSubTypeVersion {
		t.Fatalf("case metadata = %v, want case/v1", got)
	}
	if refs, ok := got["case_source_refs"].([]any); !ok || len(refs) != 1 || refs[0] != "source-b" {
		t.Fatalf("case_source_refs = %v, want [source-b]", got["case_source_refs"])
	}
}

func TestReconcileCasePageMetadata_RemovesOnlyTheReclassifiedSource(t *testing.T) {
	page := &types.WikiPage{
		PageType:     types.WikiPageTypeConcept,
		PageMetadata: types.JSON(`{"sub_type":"case","sub_type_version":"v1","case_source_refs":["source-a","source-b"]}`),
	}

	changed := reconcileCasePageMetadata(page, []SlugUpdate{
		{
			Type:      types.WikiPageTypeConcept,
			SourceRef: "source-a",
			Item:      extractedItem{},
		},
		{
			Type:      "retract",
			SourceRef: "source-a",
		},
	})
	if !changed {
		t.Fatal("expected metadata to change")
	}

	var got map[string]any
	if err := json.Unmarshal(page.PageMetadata, &got); err != nil {
		t.Fatalf("decode page metadata: %v", err)
	}
	if refs := got["case_source_refs"].([]any); len(refs) != 1 || refs[0] != "source-b" {
		t.Fatalf("case_source_refs = %v, want [source-b]", refs)
	}
	if got["sub_type"] != wikiCaseSubType {
		t.Fatalf("sub_type = %v, want case", got["sub_type"])
	}
}

func TestReconcileCasePageMetadata_ClearsManagedKeysWhenNoCaseSourceRemains(t *testing.T) {
	page := &types.WikiPage{
		PageType:     types.WikiPageTypeConcept,
		PageMetadata: types.JSON(`{"sub_type":"case","sub_type_version":"v1","case_source_refs":["source-a"]}`),
	}

	changed := reconcileCasePageMetadata(page, []SlugUpdate{
		{Type: "retract", SourceRef: "source-a"},
	})
	if !changed {
		t.Fatal("expected metadata to change")
	}
	if page.PageMetadata != nil {
		t.Fatalf("page metadata = %s, want nil after removing the last case source", page.PageMetadata)
	}
}

func TestReconcileCasePageMetadata_IgnoresUnknownSubtypeAndNonConceptPages(t *testing.T) {
	page := &types.WikiPage{PageType: types.WikiPageTypeConcept}
	if changed := reconcileCasePageMetadata(page, []SlugUpdate{
		{
			Type:      types.WikiPageTypeConcept,
			SourceRef: "source-a",
			Item: extractedItem{
				Metadata: extractedItemMetadata{SubType: "unknown"},
			},
		},
	}); changed {
		t.Fatal("unknown subtype must not change page metadata")
	}

	entity := &types.WikiPage{PageType: types.WikiPageTypeEntity}
	if changed := reconcileCasePageMetadata(entity, []SlugUpdate{
		{
			Type:      types.WikiPageTypeConcept,
			SourceRef: "source-a",
			Item: extractedItem{
				Metadata: extractedItemMetadata{SubType: wikiCaseSubType},
			},
		},
	}); changed {
		t.Fatal("case metadata must not be written to entity pages")
	}
}
