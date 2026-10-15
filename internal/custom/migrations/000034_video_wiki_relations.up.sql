CREATE TABLE IF NOT EXISTS video_wiki_relations (
    video_id VARCHAR(36) NOT NULL,
    summary_slug VARCHAR(255) NOT NULL,
    target_slug VARCHAR(255) NOT NULL,
    summary_version INTEGER NOT NULL,
    target_version INTEGER NOT NULL,
    relation TEXT NOT NULL,
    evidence_quote TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (video_id, summary_slug, target_slug)
);
