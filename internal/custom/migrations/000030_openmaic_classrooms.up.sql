CREATE TABLE IF NOT EXISTS open_maic_classrooms (
  id VARCHAR(36) PRIMARY KEY,
  request_id VARCHAR(128) NOT NULL UNIQUE,
  learner_id VARCHAR(255) NOT NULL,
  classroom_id VARCHAR(128) NOT NULL UNIQUE,
  classroom_url TEXT NOT NULL,
  status VARCHAR(32) NOT NULL,
  completion_source VARCHAR(64),
  evidence_version VARCHAR(128),
  completed_at TIMESTAMP WITH TIME ZONE,
  last_error_code VARCHAR(64),
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_open_maic_classrooms_learner_id
  ON open_maic_classrooms(learner_id);
CREATE INDEX IF NOT EXISTS idx_open_maic_classrooms_status
  ON open_maic_classrooms(status);
