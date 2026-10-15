package model

import "time"

// OpenMAICClassroom stores only the external classroom reference and the
// server-verified learning state. Classroom content remains in OpenMAIC.
type OpenMAICClassroom struct {
	ID               string `gorm:"type:varchar(36);primaryKey" json:"id"`
	RequestID        string `gorm:"type:varchar(128);uniqueIndex;not null" json:"request_id"`
	LearnerID        string `gorm:"type:varchar(255);index;not null" json:"learner_id"`
	SourceReferences string `gorm:"type:text" json:"source_references,omitempty"`
	MemoryRead       bool   `json:"memory_read"`
	MemoryCount      int    `json:"memory_count"`
	JobID            string `gorm:"type:varchar(128);uniqueIndex:idx_openmaic_job,where:job_id <> ''" json:"job_id,omitempty"`
	// cancelled is terminal; late bridge updates must never publish its result.
	GenerationStatus string     `gorm:"type:varchar(32);not null;default:'queued'" json:"generation_status"`
	Progress         *int       `json:"progress"`
	ClassroomID      string     `gorm:"type:varchar(128);uniqueIndex:idx_openmaic_classroom,where:classroom_id <> ''" json:"classroom_id,omitempty"`
	ClassroomURL     string     `gorm:"type:text" json:"classroom_url,omitempty"`
	Status           string     `gorm:"type:varchar(32);not null;index" json:"status"`
	CompletionSource string     `gorm:"type:varchar(64)" json:"completion_source,omitempty"`
	EvidenceVersion  string     `gorm:"type:varchar(128)" json:"evidence_version,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	LastErrorCode    string     `gorm:"type:varchar(64)" json:"last_error_code,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
