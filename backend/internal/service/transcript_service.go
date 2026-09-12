package service

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/dto"
	"github.com/oralhistory/oralhistory/internal/model"
	"github.com/oralhistory/oralhistory/internal/repository"
	"github.com/oralhistory/oralhistory/internal/util"
)

// TranscriptService 转写校对业务接口。
type TranscriptService interface {
	CreateDraft(actor *model.User, req *dto.CreateTranscriptRequest) (*model.Transcript, error)
	Get(actor *model.User, id uint) (*model.Transcript, error)
	List(actor *model.User, projectID uint, recordingID uint, status string) ([]model.Transcript, error)
	SaveSegments(actor *model.User, id uint, inputs []dto.TranscriptSegmentInput) (*model.Transcript, error)
	Submit(actor *model.User, id uint) (*model.Transcript, error)
	ConfirmSegment(actor *model.User, id, segmentID uint) (*model.Transcript, error)
	Approve(actor *model.User, id uint) (*model.Transcript, error)
	Reject(actor *model.User, id uint, reason string) (*model.Transcript, error)
	Search(actor *model.User, keyword string, projectID uint) ([]model.TranscriptSegment, error)
	Export(actor *model.User, id uint) (string, *model.Transcript, error)
}

type transcriptService struct {
	transcriptRepo repository.TranscriptRepository
	recordingRepo  repository.RecordingRepository
	projectRepo    repository.ProjectRepository
	logger         *slog.Logger
}

// NewTranscriptService 构造转写校对服务。
func NewTranscriptService(transcriptRepo repository.TranscriptRepository, recordingRepo repository.RecordingRepository, projectRepo repository.ProjectRepository, logger *slog.Logger) TranscriptService {
	return &transcriptService{transcriptRepo: transcriptRepo, recordingRepo: recordingRepo, projectRepo: projectRepo, logger: logger}
}

// checkWritable 校验录音与项目允许写入转写：录音存在且已就绪、项目存在且未归档、归属关系一致（防跨项目）。
func (s *transcriptService) checkWritable(recordingID, projectID uint) (*model.Recording, *model.Project, error) {
	recording, err := s.recordingRepo.FindByID(recordingID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, util.NewAppError(constants.CodeNotFound, fmt.Sprintf("录音 %d 不存在", recordingID), err)
		}
		return nil, nil, util.NewAppError(constants.CodeInternal, fmt.Sprintf("查询录音 %d 失败", recordingID), err)
	}
	if projectID > 0 && recording.ProjectID != projectID {
		return nil, nil, util.NewAppError(constants.CodeForbidden,
			fmt.Sprintf("录音 %d 属于项目 %d，不允许跨项目写入项目 %d", recordingID, recording.ProjectID, projectID), nil)
	}
	project, err := s.projectRepo.FindByID(recording.ProjectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, util.NewAppError(constants.CodeNotFound, fmt.Sprintf("项目 %d 不存在", recording.ProjectID), err)
		}
		return nil, nil, util.NewAppError(constants.CodeInternal, fmt.Sprintf("查询项目 %d 失败", recording.ProjectID), err)
	}
	if project.Status == constants.ProjectStatusArchived {
		return nil, nil, util.NewAppError(constants.CodeProjectStatus,
			fmt.Sprintf("项目 %d 已归档，不能写入转写内容", project.ID), nil)
	}
	if recording.Status != constants.RecordingStatusReady {
		return nil, nil, util.NewAppError(constants.CodeRecordingStatus,
			fmt.Sprintf("录音 %d 状态为 %s，未就绪，不能转写", recordingID, recording.Status), nil)
	}
	return recording, project, nil
}

// checkTranscriptWritable 校验转写稿所属录音与项目允许写入。
func (s *transcriptService) checkTranscriptWritable(transcript *model.Transcript) error {
	_, _, err := s.checkWritable(transcript.RecordingID, transcript.ProjectID)
	return err
}

// checkEditor 在校验可写状态之上，限制采访员只能操作自己负责项目中的录音转写（管理员不受限）。
func (s *transcriptService) checkEditor(actor *model.User, recordingID, projectID uint) error {
	_, project, err := s.checkWritable(recordingID, projectID)
	if err != nil {
		return err
	}
	if actor.Role == constants.RoleInterviewer && project.CreatedBy != actor.ID {
		return util.NewAppError(constants.CodeForbidden,
			fmt.Sprintf("项目 %d 由其他采访员负责，%s 不能操作其中的录音转写", project.ID, actor.Username), nil)
	}
	return nil
}

func (s *transcriptService) CreateDraft(actor *model.User, req *dto.CreateTranscriptRequest) (*model.Transcript, error) {
	if err := s.checkEditor(actor, req.RecordingID, req.ProjectID); err != nil {
		return nil, err
	}
	recording, _, err := s.checkWritable(req.RecordingID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	active, err := s.transcriptRepo.HasActiveDraft(req.RecordingID)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternal, "查询转写稿状态失败", err)
	}
	if active {
		return nil, util.NewAppError(constants.CodeTranscriptState,
			fmt.Sprintf("录音 %d 已存在进行中的转写稿，不能重复创建", req.RecordingID), nil)
	}
	maxVersion, err := s.transcriptRepo.MaxVersion(req.RecordingID)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternal, "查询转写版本失败", err)
	}
	transcript := &model.Transcript{
		RecordingID: recording.ID,
		ProjectID:   recording.ProjectID,
		Version:     maxVersion + 1,
		Status:      constants.TranscriptStatusDraft,
		CreatedBy:   actor.ID,
	}
	if err := s.transcriptRepo.Create(transcript); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState, fmt.Sprintf("录音 %d 转写版本冲突，请刷新后重试", req.RecordingID), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "创建转写草稿失败", err)
	}
	// 修改已通过内容时生成新版本：以上一版本（已通过）分段为底稿，旧版本保留不动。
	if maxVersion > 0 {
		versions, err := s.transcriptRepo.ListByRecording(req.RecordingID)
		if err != nil {
			return nil, util.NewAppError(constants.CodeInternal, "查询历史转写版本失败", err)
		}
		for _, v := range versions {
			if v.ID == transcript.ID {
				continue
			}
			prev, err := s.transcriptRepo.FindByIDWithSegments(v.ID)
			if err != nil {
				return nil, util.NewAppError(constants.CodeInternal, "查询上一版本分段失败", err)
			}
			segments := make([]model.TranscriptSegment, 0, len(prev.Segments))
			for i, seg := range prev.Segments {
				segments = append(segments, model.TranscriptSegment{
					TranscriptID: transcript.ID,
					StartSecond:  seg.StartSecond,
					EndSecond:    seg.EndSecond,
					Speaker:      seg.Speaker,
					Content:      seg.Content,
					SortOrder:    i,
					Status:       constants.TranscriptSegmentPending,
				})
			}
			if len(segments) > 0 {
				if err := s.transcriptRepo.ReplaceSegments(transcript.ID, segments); err != nil {
					return nil, util.NewAppError(constants.CodeInternal, "复制上一版本分段失败", err)
				}
			}
			break
		}
	}
	s.logger.Info("transcript draft created", "username", actor.Username, "recording_id", recording.ID, "version", transcript.Version)
	return s.transcriptRepo.FindByIDWithSegments(transcript.ID)
}

// get 内部查询，不做读取权限校验（写操作与导出另有各自的权限控制）。
func (s *transcriptService) get(id uint) (*model.Transcript, error) {
	transcript, err := s.transcriptRepo.FindByIDWithSegments(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, fmt.Sprintf("转写稿 %d 不存在", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, fmt.Sprintf("查询转写稿 %d 失败", id), err)
	}
	return transcript, nil
}

// checkViewer 校验读取权限：采访员只能查看自己负责项目的转写，档案员与管理员可按职责查看。
func (s *transcriptService) checkViewer(actor *model.User, projectID uint) error {
	if actor.Role != constants.RoleInterviewer {
		return nil
	}
	project, err := s.projectRepo.FindByID(projectID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return util.NewAppError(constants.CodeNotFound, fmt.Sprintf("项目 %d 不存在", projectID), err)
		}
		return util.NewAppError(constants.CodeInternal, fmt.Sprintf("查询项目 %d 失败", projectID), err)
	}
	if project.CreatedBy != actor.ID {
		return util.NewAppError(constants.CodeForbidden,
			fmt.Sprintf("项目 %d 由其他采访员负责，%s 不能查看其中的转写", projectID, actor.Username), nil)
	}
	return nil
}

func (s *transcriptService) Get(actor *model.User, id uint) (*model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkViewer(actor, transcript.ProjectID); err != nil {
		return nil, err
	}
	return transcript, nil
}

func (s *transcriptService) List(actor *model.User, projectID uint, recordingID uint, status string) ([]model.Transcript, error) {
	if status != "" && !constants.ValidTranscriptStatus(status) {
		return nil, util.NewAppError(constants.CodeValidation, fmt.Sprintf("转写稿状态 %s 不合法", status), nil)
	}
	if recordingID > 0 {
		recording, err := s.recordingRepo.FindByID(recordingID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, util.NewAppError(constants.CodeNotFound, fmt.Sprintf("录音 %d 不存在", recordingID), err)
			}
			return nil, util.NewAppError(constants.CodeInternal, fmt.Sprintf("查询录音 %d 失败", recordingID), err)
		}
		if err := s.checkViewer(actor, recording.ProjectID); err != nil {
			return nil, err
		}
		transcripts, err := s.transcriptRepo.ListByRecording(recordingID)
		if err != nil {
			return nil, util.NewAppError(constants.CodeInternal, "转写版本列表查询失败", err)
		}
		return transcripts, nil
	}
	if projectID > 0 {
		if err := s.checkViewer(actor, projectID); err != nil {
			return nil, err
		}
	}
	transcripts, err := s.transcriptRepo.List(projectID, status)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternal, "转写稿列表查询失败", err)
	}
	// 采访员不带项目过滤查询时，仅返回自己负责项目的转写。
	if actor.Role == constants.RoleInterviewer && projectID == 0 {
		ownIDs, err := s.projectRepo.ListIDsByCreator(actor.ID)
		if err != nil {
			return nil, util.NewAppError(constants.CodeInternal, "查询负责项目失败", err)
		}
		own := make(map[uint]bool, len(ownIDs))
		for _, id := range ownIDs {
			own[id] = true
		}
		filtered := make([]model.Transcript, 0, len(transcripts))
		for _, t := range transcripts {
			if own[t.ProjectID] {
				filtered = append(filtered, t)
			}
		}
		transcripts = filtered
	}
	return transcripts, nil
}

func (s *transcriptService) SaveSegments(actor *model.User, id uint, inputs []dto.TranscriptSegmentInput) (*model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkEditor(actor, transcript.RecordingID, transcript.ProjectID); err != nil {
		return nil, err
	}
	recording, err := s.recordingRepo.FindByID(transcript.RecordingID)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternal, "查询录音失败", err)
	}
	sort.SliceStable(inputs, func(i, j int) bool {
		if inputs[i].StartSecond == inputs[j].StartSecond {
			return i < j
		}
		return inputs[i].StartSecond < inputs[j].StartSecond
	})
	segments := make([]model.TranscriptSegment, 0, len(inputs))
	for i, in := range inputs {
		speaker := strings.TrimSpace(in.Speaker)
		content := strings.TrimSpace(in.Content)
		if speaker == "" || content == "" {
			return nil, util.NewAppError(constants.CodeValidation, fmt.Sprintf("第 %d 段说话人与内容不能为空", i+1), nil)
		}
		if in.StartSecond < 0 || in.EndSecond <= in.StartSecond {
			return nil, util.NewAppError(constants.CodeValidation,
				fmt.Sprintf("第 %d 段时间区间不合法：结束时间必须大于开始时间", i+1), nil)
		}
		if i > 0 && in.StartSecond < inputs[i-1].EndSecond {
			return nil, util.NewAppError(constants.CodeValidation,
				fmt.Sprintf("第 %d 段与第 %d 段时间重叠", i+1, i), nil)
		}
		if recording.DurationSeconds > 0 && in.EndSecond > recording.DurationSeconds {
			return nil, util.NewAppError(constants.CodeValidation,
				fmt.Sprintf("第 %d 段结束时间超出录音时长 %d 秒", i+1, recording.DurationSeconds), nil)
		}
		segments = append(segments, model.TranscriptSegment{
			TranscriptID: id,
			StartSecond:  in.StartSecond,
			EndSecond:    in.EndSecond,
			Speaker:      speaker,
			Content:      content,
			SortOrder:    i,
			Status:       constants.TranscriptSegmentPending,
		})
	}
	if err := s.transcriptRepo.ReplaceSegments(id, segments); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState,
				fmt.Sprintf("转写稿 %d 当前状态不允许编辑，可能已被他人提交或审核", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "保存转写分段失败", err)
	}
	s.logger.Info("transcript segments saved", "username", actor.Username, "transcript_id", id, "segments", len(segments))
	return s.transcriptRepo.FindByIDWithSegments(id)
}

func (s *transcriptService) Submit(actor *model.User, id uint) (*model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkEditor(actor, transcript.RecordingID, transcript.ProjectID); err != nil {
		return nil, err
	}
	if err := s.transcriptRepo.Submit(id, actor.ID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState,
				fmt.Sprintf("转写稿 %d 提交失败：状态已变化或没有分段内容", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "提交转写稿失败", err)
	}
	s.logger.Info("transcript submitted", "username", actor.Username, "transcript_id", id)
	return s.transcriptRepo.FindByIDWithSegments(id)
}

func (s *transcriptService) ConfirmSegment(actor *model.User, id, segmentID uint) (*model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkTranscriptWritable(transcript); err != nil {
		return nil, err
	}
	if err := s.transcriptRepo.ConfirmSegment(id, segmentID, actor.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, fmt.Sprintf("分段 %d 不属于转写稿 %d", segmentID, id), err)
		}
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState,
				fmt.Sprintf("转写稿 %d 当前状态不允许确认分段", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "确认分段失败", err)
	}
	s.logger.Info("transcript segment confirmed", "username", actor.Username, "transcript_id", id, "segment_id", segmentID)
	return s.transcriptRepo.FindByIDWithSegments(id)
}

func (s *transcriptService) Approve(actor *model.User, id uint) (*model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkTranscriptWritable(transcript); err != nil {
		return nil, err
	}
	if err := s.transcriptRepo.Approve(id, actor.ID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState,
				fmt.Sprintf("转写稿 %d 通过失败：状态已变化或仍有未确认分段", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "通过转写稿失败", err)
	}
	s.logger.Info("transcript approved", "username", actor.Username, "transcript_id", id)
	return s.transcriptRepo.FindByIDWithSegments(id)
}

func (s *transcriptService) Reject(actor *model.User, id uint, reason string) (*model.Transcript, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, util.NewAppError(constants.CodeValidation, "退回必须写明问题", nil)
	}
	transcript, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkTranscriptWritable(transcript); err != nil {
		return nil, err
	}
	if err := s.transcriptRepo.Reject(id, actor.ID, reason); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, util.NewAppError(constants.CodeTranscriptState,
				fmt.Sprintf("转写稿 %d 当前状态不允许退回", id), err)
		}
		return nil, util.NewAppError(constants.CodeInternal, "退回转写稿失败", err)
	}
	s.logger.Info("transcript rejected", "username", actor.Username, "transcript_id", id, "reason", reason)
	return s.transcriptRepo.FindByIDWithSegments(id)
}

func (s *transcriptService) Search(actor *model.User, keyword string, projectID uint) ([]model.TranscriptSegment, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, util.NewAppError(constants.CodeValidation, "检索关键词不能为空", nil)
	}
	segments, err := s.transcriptRepo.SearchApproved(keyword, projectID)
	if err != nil {
		return nil, util.NewAppError(constants.CodeInternal, "转写全文检索失败", err)
	}
	return segments, nil
}

// Export 导出已通过转写稿全文（纯文本，按时间轴排列）。
func (s *transcriptService) Export(actor *model.User, id uint) (string, *model.Transcript, error) {
	transcript, err := s.get(id)
	if err != nil {
		return "", nil, err
	}
	if transcript.Status != constants.TranscriptStatusApproved {
		return "", nil, util.NewAppError(constants.CodeTranscriptState,
			fmt.Sprintf("转写稿 %d 未审核通过，不能导出", id), nil)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# 转写稿 录音#%d 版本v%d\n\n", transcript.RecordingID, transcript.Version)
	for _, seg := range transcript.Segments {
		fmt.Fprintf(&b, "[%02d:%02d - %02d:%02d] %s：%s\n",
			seg.StartSecond/60, seg.StartSecond%60, seg.EndSecond/60, seg.EndSecond%60, seg.Speaker, seg.Content)
	}
	s.logger.Info("transcript exported", "username", actor.Username, "transcript_id", id)
	return b.String(), transcript, nil
}
