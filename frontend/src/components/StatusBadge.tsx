// 通用状态徽标组件，跨页面复用。
import { PROJECT_STATUS_TEXT, RECORDING_STATUS_TEXT, TRANSCRIPT_SEGMENT_STATUS_TEXT, TRANSCRIPT_STATUS_TEXT } from '../constants'

interface StatusBadgeProps {
  status: string
  type?: 'project' | 'recording' | 'transcript' | 'segment'
}

const STYLES: Record<string, string> = {
  draft: 'badge-draft',
  in_progress: 'badge-progress',
  completed: 'badge-completed',
  archived: 'badge-archived',
  recording: 'badge-recording',
  processing: 'badge-processing',
  ready: 'badge-ready',
  failed: 'badge-failed',
  submitted: 'badge-progress',
  approved: 'badge-completed',
  rejected: 'badge-recording',
  pending: 'badge-processing',
  confirmed: 'badge-ready',
}

const TEXTS: Record<string, Record<string, string>> = {
  project: PROJECT_STATUS_TEXT,
  recording: RECORDING_STATUS_TEXT,
  transcript: TRANSCRIPT_STATUS_TEXT,
  segment: TRANSCRIPT_SEGMENT_STATUS_TEXT,
}

export default function StatusBadge({ status, type = 'project' }: StatusBadgeProps) {
  const text = (TEXTS[type] || PROJECT_STATUS_TEXT)[status]
  return <span className={`status-badge ${STYLES[status] || 'badge-default'}`}>{text || status}</span>
}
