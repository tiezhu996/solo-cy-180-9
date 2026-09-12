package model

import "time"

// Transcript 转写稿版本实体：同一录音每次「修改已通过内容」都会产生新版本，旧版本保留。
type Transcript struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	RecordingID   uint       `gorm:"index;not null;uniqueIndex:uk_recording_version" json:"recording_id"`
	ProjectID     uint       `gorm:"index;not null" json:"project_id"`
	Version       int        `gorm:"not null;default:1;uniqueIndex:uk_recording_version" json:"version"`
	Status        string     `gorm:"size:32;not null;default:draft" json:"status"`
	ReviewComment string     `gorm:"size:512" json:"review_comment"` // 退回原因（退回时必填）
	CreatedBy     uint       `gorm:"not null" json:"created_by"`
	SubmittedBy   uint       `json:"submitted_by"`
	SubmittedAt   *time.Time `json:"submitted_at,omitempty"`
	ReviewedBy    uint       `json:"reviewed_by"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	Segments []TranscriptSegment `gorm:"foreignKey:TranscriptID" json:"segments,omitempty"`
}

// TableName 指定表名。
func (Transcript) TableName() string { return "transcripts" }

// TranscriptSegment 转写分段实体：按时间轴记录说话人与内容，档案员逐段确认。
type TranscriptSegment struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TranscriptID uint       `gorm:"index;not null" json:"transcript_id"`
	StartSecond  int        `gorm:"not null;default:0" json:"start_second"`
	EndSecond    int        `gorm:"not null;default:0" json:"end_second"`
	Speaker      string     `gorm:"size:64;not null" json:"speaker"`
	Content      string     `gorm:"type:text;not null" json:"content"`
	SortOrder    int        `gorm:"not null;default:0" json:"sort_order"`
	Status       string     `gorm:"size:32;not null;default:pending" json:"status"`
	ReviewedBy   uint       `json:"reviewed_by"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TableName 指定表名。
func (TranscriptSegment) TableName() string { return "transcript_segments" }
