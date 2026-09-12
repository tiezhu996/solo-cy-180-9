// 转写稿与转写分段状态机枚举。
package constants

// 转写稿状态定义。
const (
	TranscriptStatusDraft     = "draft"     // 草稿：采访员编辑中
	TranscriptStatusSubmitted = "submitted" // 已提交：等待档案员审核
	TranscriptStatusApproved  = "approved"  // 审核通过：可检索与导出
	TranscriptStatusRejected  = "rejected"  // 已退回：采访员修改后可重新提交
)

// 转写分段审核状态定义。
const (
	TranscriptSegmentPending   = "pending"   // 待确认
	TranscriptSegmentConfirmed = "confirmed" // 档案员已确认
)

// ValidTranscriptStatus 校验转写稿状态是否合法。
func ValidTranscriptStatus(status string) bool {
	switch status {
	case TranscriptStatusDraft, TranscriptStatusSubmitted, TranscriptStatusApproved, TranscriptStatusRejected:
		return true
	default:
		return false
	}
}

// CanTransitionTranscript 返回转写稿状态流转是否允许。
func CanTransitionTranscript(from, to string) bool {
	switch from {
	case TranscriptStatusDraft:
		return to == TranscriptStatusSubmitted
	case TranscriptStatusRejected:
		return to == TranscriptStatusSubmitted || to == TranscriptStatusDraft
	case TranscriptStatusSubmitted:
		return to == TranscriptStatusApproved || to == TranscriptStatusRejected
	case TranscriptStatusApproved:
		// 已通过内容不可直接流转，修改需创建新版本。
		return false
	default:
		return false
	}
}

// EditableTranscript 返回转写稿当前是否可编辑分段内容。
func EditableTranscript(status string) bool {
	return status == TranscriptStatusDraft || status == TranscriptStatusRejected
}
