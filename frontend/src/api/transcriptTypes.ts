// 转写校对模块类型定义。
export type TranscriptStatus = 'draft' | 'submitted' | 'approved' | 'rejected'
export type TranscriptSegmentStatus = 'pending' | 'confirmed'

export interface TranscriptSegment {
  id: number
  transcript_id: number
  start_second: number
  end_second: number
  speaker: string
  content: string
  sort_order: number
  status: TranscriptSegmentStatus
  reviewed_by: number
  reviewed_at?: string
  created_at: string
  updated_at: string
}

export interface Transcript {
  id: number
  recording_id: number
  project_id: number
  version: number
  status: TranscriptStatus
  review_comment: string
  created_by: number
  submitted_by: number
  submitted_at?: string
  reviewed_by: number
  reviewed_at?: string
  created_at: string
  updated_at: string
  segments?: TranscriptSegment[]
}

export interface TranscriptSegmentInput {
  start_second: number
  end_second: number
  speaker: string
  content: string
}
