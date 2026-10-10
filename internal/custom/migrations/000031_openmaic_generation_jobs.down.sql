-- Rollback requires removing incomplete jobs, preserving published classrooms.
DELETE FROM open_maic_classrooms WHERE classroom_id IS NULL OR classroom_id = '';
DROP INDEX IF EXISTS idx_openmaic_job;
DROP INDEX IF EXISTS idx_openmaic_classroom;
ALTER TABLE open_maic_classrooms DROP COLUMN IF EXISTS generation_status;
ALTER TABLE open_maic_classrooms DROP COLUMN IF EXISTS job_id;
ALTER TABLE open_maic_classrooms ALTER COLUMN classroom_id SET NOT NULL;
ALTER TABLE open_maic_classrooms ALTER COLUMN classroom_url SET NOT NULL;
ALTER TABLE open_maic_classrooms ADD CONSTRAINT open_maic_classrooms_classroom_id_key UNIQUE(classroom_id);
ALTER TABLE open_maic_classrooms DROP COLUMN IF EXISTS source_references;
ALTER TABLE open_maic_classrooms DROP COLUMN IF EXISTS memory_read;
ALTER TABLE open_maic_classrooms DROP COLUMN IF EXISTS memory_count;
