package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/dto"
	"github.com/oralhistory/oralhistory/internal/middleware"
	"github.com/oralhistory/oralhistory/internal/service"
	"github.com/oralhistory/oralhistory/internal/util"
)

// TranscriptHandler 转写校对接口处理器。
type TranscriptHandler struct {
	transcriptSvc service.TranscriptService
	auditSvc      service.AuditService
	logger        *slog.Logger
}

// NewTranscriptHandler 构造转写校对处理器。
func NewTranscriptHandler(transcriptSvc service.TranscriptService, auditSvc service.AuditService, logger *slog.Logger) *TranscriptHandler {
	return &TranscriptHandler{transcriptSvc: transcriptSvc, auditSvc: auditSvc, logger: logger}
}

// Create 为已就绪录音创建转写草稿（采访员）。
func (h *TranscriptHandler) Create(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	var req dto.CreateTranscriptRequest
	if !bindJSON(c, &req) {
		return
	}
	transcript, err := h.transcriptSvc.CreateDraft(actor, &req)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.create", "transcript", transcript.ID,
		fmt.Sprintf("创建转写草稿 录音#%d 版本v%d", transcript.RecordingID, transcript.Version), c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgTranscriptCreated, transcript)
}

// Get 查询转写稿详情（含分段）。
func (h *TranscriptHandler) Get(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	transcript, err := h.transcriptSvc.Get(id)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, transcript)
}

// List 转写稿列表（支持 project_id / recording_id / status 过滤）。
func (h *TranscriptHandler) List(c *gin.Context) {
	var query dto.ListTranscriptQuery
	if !bindQuery(c, &query) {
		return
	}
	transcripts, err := h.transcriptSvc.List(query.ProjectID, query.RecordingID, query.Status)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, gin.H{"list": transcripts})
}

// SaveSegments 批量保存草稿分段（采访员，整体替换）。
func (h *TranscriptHandler) SaveSegments(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req dto.SaveTranscriptSegmentsRequest
	if !bindJSON(c, &req) {
		return
	}
	transcript, err := h.transcriptSvc.SaveSegments(actor, id, req.Segments)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.save", "transcript", transcript.ID,
		fmt.Sprintf("保存转写分段 %d 条", len(req.Segments)), c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgTranscriptSaved, transcript)
}

// Submit 提交转写稿进入审核（采访员）。
func (h *TranscriptHandler) Submit(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	transcript, err := h.transcriptSvc.Submit(actor, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.submit", "transcript", transcript.ID,
		"提交转写稿审核", c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgTranscriptSubmitted, transcript)
}

// ConfirmSegment 档案员逐段确认。
func (h *TranscriptHandler) ConfirmSegment(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	segmentID, ok := parseID(c, "segmentId")
	if !ok {
		return
	}
	transcript, err := h.transcriptSvc.ConfirmSegment(actor, id, segmentID)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.confirm_segment", "transcript", transcript.ID,
		fmt.Sprintf("确认分段 %d", segmentID), c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgSegmentConfirmed, transcript)
}

// Approve 档案员整篇通过。
func (h *TranscriptHandler) Approve(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	transcript, err := h.transcriptSvc.Approve(actor, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.approve", "transcript", transcript.ID,
		"转写稿审核通过", c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgTranscriptApproved, transcript)
}

// Reject 档案员整篇退回（必须写明问题）。
func (h *TranscriptHandler) Reject(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req dto.RejectTranscriptRequest
	if !bindJSON(c, &req) {
		return
	}
	transcript, err := h.transcriptSvc.Reject(actor, id, req.Reason)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.reject", "transcript", transcript.ID,
		"退回转写稿: "+req.Reason, c.ClientIP(), middleware.RequestID(c))
	util.OKMessage(c, constants.MsgTranscriptRejected, transcript)
}

// Search 检索已通过转写全文。
func (h *TranscriptHandler) Search(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	var query dto.SearchTranscriptQuery
	if !bindQuery(c, &query) {
		return
	}
	segments, err := h.transcriptSvc.Search(actor, query.Keyword, query.ProjectID)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, gin.H{"list": segments})
}

// Export 导出已通过转写稿全文（纯文本）。
func (h *TranscriptHandler) Export(c *gin.Context) {
	actor, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(err)
		return
	}
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	content, transcript, err := h.transcriptSvc.Export(actor, id)
	if err != nil {
		c.Error(err)
		return
	}
	h.auditSvc.Record(actor.ID, actor.Username, actor.Role, "transcript.export", "transcript", transcript.ID,
		"导出转写稿", c.ClientIP(), middleware.RequestID(c))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=transcript_%d_v%d.txt", transcript.ID, transcript.Version))
	c.String(http.StatusOK, content)
}
