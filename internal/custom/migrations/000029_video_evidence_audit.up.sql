ALTER TABLE chat_source_audits
    ADD COLUMN IF NOT EXISTS capability_version VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS route_mode VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS coverage VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS linkable_evidence INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS invalid_references INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS degradation_code VARCHAR(48) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_chat_source_audits_capability_version
    ON chat_source_audits(capability_version);

CREATE INDEX IF NOT EXISTS idx_chat_source_audits_coverage
    ON chat_source_audits(coverage);

CREATE INDEX IF NOT EXISTS idx_chat_source_audits_degradation_code
    ON chat_source_audits(degradation_code);
