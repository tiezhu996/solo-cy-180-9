package dto

// CreateTranscriptRequest 创建转写草稿请求。
type CreateTranscriptRequest struct {
	RecordingID uint `json:"recording_id" binding:"required"`
	ProjectID   uint `json:"project_id" binding:"required"`
}

// TranscriptSegmentInput 分段输入（保存草稿时整体替换）。
type TranscriptSegmentInput struct {
	StartSecond int    `json:"start_second" binding:"min=0"`
	EndSecond   int    `json:"end_second" binding:"min=0"`
	Speaker     string `json:"speaker" binding:"required,max=64"`
	Content     string `json:"content" binding:"required"`
}

// SaveTranscriptSegmentsRequest 批量保存转写分段请求。
type SaveTranscriptSegmentsRequest struct {
	Segments []TranscriptSegmentInput `json:"segments" binding:"required"`
}

// RejectTranscriptRequest 退回转写稿请求，必须写明问题。
type RejectTranscriptRequest struct {
	Reason string `json:"reason" binding:"required,max=512"`
}

// ListTranscriptQuery 转写稿列表查询。
type ListTranscriptQuery struct {
	ProjectID  uint   `form:"project_id"`
	RecordingID uint  `form:"recording_id"`
	Status     string `form:"status"`
}

// SearchTranscriptQuery 已通过转写全文检索。
type SearchTranscriptQuery struct {
	Keyword   string `form:"q" binding:"required,max=128"`
	ProjectID uint   `form:"project_id"`
}
