package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TranscriptRepository 转写稿数据访问接口。
// 状态流转方法内部开启事务并对转写稿行加锁，保证并发安全。
type TranscriptRepository interface {
	Create(transcript *model.Transcript) error
	FindByID(id uint) (*model.Transcript, error)
	FindByIDWithSegments(id uint) (*model.Transcript, error)
	ListByRecording(recordingID uint) ([]model.Transcript, error)
	List(projectID uint, status string) ([]model.Transcript, error)
	MaxVersion(recordingID uint) (int, error)
	HasActiveDraft(recordingID uint) (bool, error)
	ReplaceSegments(transcriptID uint, segments []model.TranscriptSegment) error
	Submit(id, actorID uint) error
	ConfirmSegment(transcriptID, segmentID, reviewerID uint) error
	Approve(id, reviewerID uint) error
	Reject(id, reviewerID uint, reason string) error
	SearchApproved(keyword string, projectID uint) ([]model.TranscriptSegment, error)
}

type transcriptRepository struct {
	db *gorm.DB
}

// NewTranscriptRepository 构造转写稿仓储。
func NewTranscriptRepository(db *gorm.DB) TranscriptRepository {
	return &transcriptRepository{db: db}
}

func (r *transcriptRepository) Create(transcript *model.Transcript) error {
	if err := r.db.Create(transcript).Error; err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("create transcript of recording %d: %w", transcript.RecordingID, ErrConflict)
		}
		return fmt.Errorf("create transcript of recording %d: %w", transcript.RecordingID, err)
	}
	return nil
}

func (r *transcriptRepository) FindByID(id uint) (*model.Transcript, error) {
	var transcript model.Transcript
	if err := r.db.First(&transcript, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find transcript by id %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("find transcript by id: %w", err)
	}
	return &transcript, nil
}

func (r *transcriptRepository) FindByIDWithSegments(id uint) (*model.Transcript, error) {
	var transcript model.Transcript
	if err := r.db.Preload("Segments", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).First(&transcript, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find transcript with segments by id %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("find transcript with segments: %w", err)
	}
	return &transcript, nil
}

func (r *transcriptRepository) ListByRecording(recordingID uint) ([]model.Transcript, error) {
	var transcripts []model.Transcript
	if err := r.db.Where("recording_id = ?", recordingID).Order("version DESC").Find(&transcripts).Error; err != nil {
		return nil, fmt.Errorf("list transcripts of recording %d: %w", recordingID, err)
	}
	return transcripts, nil
}

func (r *transcriptRepository) List(projectID uint, status string) ([]model.Transcript, error) {
	var transcripts []model.Transcript
	query := r.db.Model(&model.Transcript{}).Order("updated_at DESC")
	if projectID > 0 {
		query = query.Where("project_id = ?", projectID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&transcripts).Error; err != nil {
		return nil, fmt.Errorf("list transcripts: %w", err)
	}
	return transcripts, nil
}

func (r *transcriptRepository) MaxVersion(recordingID uint) (int, error) {
	var maxVersion *int
	if err := r.db.Model(&model.Transcript{}).
		Where("recording_id = ?", recordingID).
		Select("MAX(version)").Scan(&maxVersion).Error; err != nil {
		return 0, fmt.Errorf("query max transcript version of recording %d: %w", recordingID, err)
	}
	if maxVersion == nil {
		return 0, nil
	}
	return *maxVersion, nil
}

// HasActiveDraft 判断录音是否已有草稿/待审核/已退回的「活跃」转写稿（此时不允许再建新版本）。
func (r *transcriptRepository) HasActiveDraft(recordingID uint) (bool, error) {
	var count int64
	if err := r.db.Model(&model.Transcript{}).
		Where("recording_id = ? AND status IN ?", recordingID,
			[]string{constants.TranscriptStatusDraft, constants.TranscriptStatusSubmitted, constants.TranscriptStatusRejected}).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("count active transcripts of recording %d: %w", recordingID, err)
	}
	return count > 0, nil
}

// lockForUpdate 在事务内锁定转写稿行。
func lockForUpdate(tx *gorm.DB, id uint) (*model.Transcript, error) {
	var transcript model.Transcript
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transcript, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("lock transcript %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("lock transcript %d: %w", id, err)
	}
	return &transcript, nil
}

// ReplaceSegments 整体替换草稿分段：仅当转写稿处于可编辑状态时生效，否则返回 ErrConflict。
func (r *transcriptRepository) ReplaceSegments(transcriptID uint, segments []model.TranscriptSegment) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		transcript, err := lockForUpdate(tx, transcriptID)
		if err != nil {
			return err
		}
		if !constants.EditableTranscript(transcript.Status) {
			return fmt.Errorf("replace segments of transcript %d in status %s: %w", transcriptID, transcript.Status, ErrConflict)
		}
		if err := tx.Where("transcript_id = ?", transcriptID).Delete(&model.TranscriptSegment{}).Error; err != nil {
			return fmt.Errorf("delete old segments of transcript %d: %w", transcriptID, err)
		}
		if len(segments) > 0 {
			if err := tx.Create(&segments).Error; err != nil {
				return fmt.Errorf("insert segments of transcript %d: %w", transcriptID, err)
			}
		}
		// 退回后重新编辑时回到草稿态，并清空上次的退回原因。
		if transcript.Status == constants.TranscriptStatusRejected {
			if err := tx.Model(&model.Transcript{}).Where("id = ?", transcriptID).
				Updates(map[string]any{"status": constants.TranscriptStatusDraft, "review_comment": ""}).Error; err != nil {
				return fmt.Errorf("reset transcript %d to draft: %w", transcriptID, err)
			}
		}
		return nil
	})
}

// Submit 提交审核：要求处于草稿/已退回且至少有一条分段。
func (r *transcriptRepository) Submit(id, actorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		transcript, err := lockForUpdate(tx, id)
		if err != nil {
			return err
		}
		if !constants.CanTransitionTranscript(transcript.Status, constants.TranscriptStatusSubmitted) {
			return fmt.Errorf("submit transcript %d in status %s: %w", id, transcript.Status, ErrConflict)
		}
		var count int64
		if err := tx.Model(&model.TranscriptSegment{}).Where("transcript_id = ?", id).Count(&count).Error; err != nil {
			return fmt.Errorf("count segments of transcript %d: %w", id, err)
		}
		if count == 0 {
			return fmt.Errorf("submit transcript %d without segments: %w", id, ErrConflict)
		}
		now := time.Now()
		updates := map[string]any{
			"status":       constants.TranscriptStatusSubmitted,
			"submitted_by": actorID,
			"submitted_at": &now,
		}
		if err := tx.Model(&model.Transcript{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return fmt.Errorf("submit transcript %d: %w", id, err)
		}
		// 重新提交时重置所有分段为待确认。
		if err := tx.Model(&model.TranscriptSegment{}).Where("transcript_id = ?", id).
			Updates(map[string]any{"status": constants.TranscriptSegmentPending, "reviewed_by": 0, "reviewed_at": nil}).Error; err != nil {
			return fmt.Errorf("reset segments of transcript %d: %w", id, err)
		}
		return nil
	})
}

// ConfirmSegment 档案员逐段确认：仅当转写稿处于待审核状态。
func (r *transcriptRepository) ConfirmSegment(transcriptID, segmentID, reviewerID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		transcript, err := lockForUpdate(tx, transcriptID)
		if err != nil {
			return err
		}
		if transcript.Status != constants.TranscriptStatusSubmitted {
			return fmt.Errorf("confirm segment of transcript %d in status %s: %w", transcriptID, transcript.Status, ErrConflict)
		}
		now := time.Now()
		result := tx.Model(&model.TranscriptSegment{}).
			Where("id = ? AND transcript_id = ?", segmentID, transcriptID).
			Updates(map[string]any{
				"status":      constants.TranscriptSegmentConfirmed,
				"reviewed_by": reviewerID,
				"reviewed_at": &now,
			})
		if result.Error != nil {
			return fmt.Errorf("confirm segment %d: %w", segmentID, result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("confirm segment %d of transcript %d: %w", segmentID, transcriptID, ErrNotFound)
		}
		return nil
	})
}

// Approve 整篇通过：要求全部分段均已确认。
func (r *transcriptRepository) Approve(id, reviewerID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		transcript, err := lockForUpdate(tx, id)
		if err != nil {
			return err
		}
		if !constants.CanTransitionTranscript(transcript.Status, constants.TranscriptStatusApproved) {
			return fmt.Errorf("approve transcript %d in status %s: %w", id, transcript.Status, ErrConflict)
		}
		var pending int64
		if err := tx.Model(&model.TranscriptSegment{}).
			Where("transcript_id = ? AND status <> ?", id, constants.TranscriptSegmentConfirmed).
			Count(&pending).Error; err != nil {
			return fmt.Errorf("count pending segments of transcript %d: %w", id, err)
		}
		if pending > 0 {
			return fmt.Errorf("approve transcript %d with %d unconfirmed segments: %w", id, pending, ErrConflict)
		}
		now := time.Now()
		if err := tx.Model(&model.Transcript{}).Where("id = ?", id).Updates(map[string]any{
			"status":         constants.TranscriptStatusApproved,
			"reviewed_by":    reviewerID,
			"reviewed_at":    &now,
			"review_comment": "",
		}).Error; err != nil {
			return fmt.Errorf("approve transcript %d: %w", id, err)
		}
		return nil
	})
}

// Reject 整篇退回：必须写明问题。
func (r *transcriptRepository) Reject(id, reviewerID uint, reason string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		transcript, err := lockForUpdate(tx, id)
		if err != nil {
			return err
		}
		if !constants.CanTransitionTranscript(transcript.Status, constants.TranscriptStatusRejected) {
			return fmt.Errorf("reject transcript %d in status %s: %w", id, transcript.Status, ErrConflict)
		}
		now := time.Now()
		if err := tx.Model(&model.Transcript{}).Where("id = ?", id).Updates(map[string]any{
			"status":         constants.TranscriptStatusRejected,
			"review_comment": reason,
			"reviewed_by":    reviewerID,
			"reviewed_at":    &now,
		}).Error; err != nil {
			return fmt.Errorf("reject transcript %d: %w", id, err)
		}
		return nil
	})
}

// SearchApproved 在每个录音「最新已通过版本」的分段中检索关键词。
func (r *transcriptRepository) SearchApproved(keyword string, projectID uint) ([]model.TranscriptSegment, error) {
	var segments []model.TranscriptSegment
	latestApproved := r.db.Model(&model.Transcript{}).
		Select("recording_id, MAX(version) AS version").
		Where("status = ?", constants.TranscriptStatusApproved).
		Group("recording_id")
	query := r.db.Model(&model.TranscriptSegment{}).
		Joins("JOIN transcripts ON transcripts.id = transcript_segments.transcript_id").
		Joins("JOIN (?) AS latest ON latest.recording_id = transcripts.recording_id AND latest.version = transcripts.version", latestApproved).
		Where("transcripts.status = ?", constants.TranscriptStatusApproved).
		Where("transcript_segments.content LIKE ? OR transcript_segments.speaker LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	if projectID > 0 {
		query = query.Where("transcripts.project_id = ?", projectID)
	}
	if err := query.Order("transcript_segments.transcript_id ASC, transcript_segments.sort_order ASC").
		Find(&segments).Error; err != nil {
		return nil, fmt.Errorf("search approved transcripts: %w", err)
	}
	return segments, nil
}
