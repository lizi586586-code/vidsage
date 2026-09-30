package videoevidence

import "testing"

func TestParseCandidateManifestStrictJSON(t *testing.T) {
	valid := `{"version": "1", "task_type": "multi_video_location", "topic": "AI提示词", "videos": [{"title":"视频一","knowledge_base_id":"kb-1"},{"title":"视频二","knowledge_base_id":"kb-1"}]}`
	if !IsCandidateManifestOutput(valid) || !IsCandidateManifestOutput(`{"task_type" : "multi_video_location", "videos": [`) {
		t.Fatal("candidate envelope must be recognized with whitespace or truncated fields")
	}
	if IsCandidateManifestOutput(`{"schema_version":"answer_contract/v1","content_markdown":"multi_video_location"}`) {
		t.Fatal("final answer must not be treated as candidate manifest")
	}
	if _, err := ParseCandidateManifest(valid); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	for name, raw := range map[string]string{
		"truncated":  `{"version":"1"`,
		"fence":      "```json\n" + valid + "\n```",
		"trailing":   valid + " done",
		"unknown":    `{"version":"1","task_type":"multi_video_location","topic":"x","extra":true,"videos":[]}`,
		"unknownID":  `{"version":"1","task_type":"multi_video_location","topic":"x","videos":[{"candidate_ref":"x","title":"一"},{"title":"二"}]}`,
		"duplicate":  `{"version":"1","task_type":"multi_video_location","topic":"x","videos":[{"title":"一","knowledge_base_id":"kb"},{"title":"一","knowledge_base_id":"kb"}]}`,
		"emptyTitle": `{"version":"1","task_type":"multi_video_location","topic":"x","videos":[{"title":" "},{"title":"二"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCandidateManifest(raw); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestMatchCandidateTitleRequiresExactUniqueScopedResult(t *testing.T) {
	whitelist := []VideoCandidate{{CandidateRef: "1", Title: "视频一", KnowledgeBaseID: "kb-1"}}
	if got, status := MatchCandidateTitle("视频一", "kb-1", whitelist); status != CandidateTitleMatched || got.CandidateRef != "1" {
		t.Fatalf("exact title match = (%+v, %s)", got, status)
	}
	if _, status := MatchCandidateTitle("改写的视频一", "kb-1", whitelist); status != CandidateTitleNotFound {
		t.Fatalf("rewritten title status = %s", status)
	}
	if _, status := MatchCandidateTitle("视频一", "kb-2", whitelist); status != CandidateTitleOutOfScope {
		t.Fatalf("out-of-scope status = %s", status)
	}
	duplicate := append(whitelist, VideoCandidate{CandidateRef: "2", Title: "视频一", KnowledgeBaseID: "kb-1"})
	if _, status := MatchCandidateTitle("视频一", "", duplicate); status != CandidateTitleAmbiguous {
		t.Fatalf("ambiguous title status = %s", status)
	}
	otherKB := []VideoCandidate{{CandidateRef: "1", Title: "视频一", KnowledgeBaseID: "kb-1"}, {CandidateRef: "2", Title: "视频一", KnowledgeBaseID: "kb-2"}}
	if got, status := MatchCandidateTitle("视频一", "kb-2", otherKB); status != CandidateTitleMatched || got.CandidateRef != "2" {
		t.Fatalf("scoped duplicate title = (%+v, %s)", got, status)
	}
}

func TestSummarizeCandidateRetrievals(t *testing.T) {
	cases := []struct {
		name   string
		states []CandidateRetrieval
		want   string
	}{
		{"complete", []CandidateRetrieval{{"1", RetrievalEvidenceFound}, {"2", RetrievalEvidenceFound}}, "complete"},
		{"partial", []CandidateRetrieval{{"1", RetrievalEvidenceFound}, {"2", RetrievalSearchedNoResult}}, "partial"},
		{"incomplete", []CandidateRetrieval{{"1", RetrievalEvidenceFound}, {"2", RetrievalUnresolved}}, "incomplete"},
		{"retrieval failed", []CandidateRetrieval{{"1", RetrievalFailed}}, "incomplete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SummarizeCandidateRetrievals(tc.states); got != tc.want {
				t.Fatalf("summary = %s, want %s", got, tc.want)
			}
		})
	}
}
