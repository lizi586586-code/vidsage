DROP INDEX IF EXISTS idx_chat_source_audits_degradation_code;
DROP INDEX IF EXISTS idx_chat_source_audits_coverage;
DROP INDEX IF EXISTS idx_chat_source_audits_capability_version;

ALTER TABLE chat_source_audits
    DROP COLUMN IF EXISTS degradation_code,
    DROP COLUMN IF EXISTS invalid_references,
    DROP COLUMN IF EXISTS linkable_evidence,
    DROP COLUMN IF EXISTS coverage,
    DROP COLUMN IF EXISTS route_mode,
    DROP COLUMN IF EXISTS capability_version;
