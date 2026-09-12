// 转写校对详情页：采访员按时间轴分段编辑，档案员逐段确认/整篇退回。
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import StatusBadge from '../../components/StatusBadge'
import {
  ROLE_ADMIN,
  ROLE_ARCHIVIST,
  ROLE_INTERVIEWER,
  TRANSCRIPT_STATUS_APPROVED,
  TRANSCRIPT_STATUS_DRAFT,
  TRANSCRIPT_STATUS_REJECTED,
  TRANSCRIPT_STATUS_SUBMITTED,
} from '../../constants'
import { getRecording } from '../../api/recording'
import { downloadTranscript } from '../../api/transcript'
import type { Recording } from '../../api/types'
import type { TranscriptSegmentInput } from '../../api/transcriptTypes'
import { useAuthStore } from '../../stores/authStore'
import { useTranscriptStore } from '../../stores/transcriptStore'
import { formatDuration } from '../../utils/format'

interface EditableSegment extends TranscriptSegmentInput {
  key: number
}

let keySeq = 1

export default function TranscriptDetailPage() {
  const { id } = useParams()
  const transcriptId = Number(id)
  const navigate = useNavigate()
  const { hasRole } = useAuthStore()
  const { current, fetchDetail, saveSegments, submit, confirm, approve, reject, create } = useTranscriptStore()
  const [recording, setRecording] = useState<Recording | null>(null)
  const [rows, setRows] = useState<EditableSegment[]>([])
  const [rejectReason, setRejectReason] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const canEdit = hasRole(ROLE_INTERVIEWER, ROLE_ADMIN)
  const canReview = hasRole(ROLE_ARCHIVIST, ROLE_ADMIN)

  const reload = useCallback(async () => {
    const t = await fetchDetail(transcriptId)
    const rec = await getRecording(t.recording_id)
    setRecording(rec)
    setRows(
      (t.segments || []).map((s) => ({
        key: keySeq++,
        start_second: s.start_second,
        end_second: s.end_second,
        speaker: s.speaker,
        content: s.content,
      })),
    )
  }, [fetchDetail, transcriptId])

  useEffect(() => {
    reload().catch((e) => setError(e.message))
  }, [reload])

  const toast = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 3000)
  }
  const run = async (fn: () => Promise<void>, ok: string) => {
    setError('')
    try {
      await fn()
      toast(ok)
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败')
    }
  }

  const editable = useMemo(
    () => !!current && canEdit && (current.status === TRANSCRIPT_STATUS_DRAFT || current.status === TRANSCRIPT_STATUS_REJECTED),
    [current, canEdit],
  )
  const reviewing = useMemo(() => !!current && canReview && current.status === TRANSCRIPT_STATUS_SUBMITTED, [current, canReview])
  const allConfirmed = useMemo(
    () => !!current && (current.segments || []).length > 0 && (current.segments || []).every((s) => s.status === 'confirmed'),
    [current],
  )

  if (!current) {
    return <div className="page">{error ? <div className="toast error">{error}</div> : '加载中…'}</div>
  }

  const updateRow = (key: number, patch: Partial<EditableSegment>) => {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)))
  }

  return (
    <div className="page">
      {message && <div className="toast success">{message}</div>}
      {error && <div className="toast error">{error}</div>}
      <div className="page-header">
        <button className="btn btn-plain" onClick={() => navigate(-1)}>
          ← 返回
        </button>
        <h2>
          转写稿 #{current.id} <span className="muted">v{current.version}</span>
        </h2>
        <StatusBadge status={current.status} type="transcript" />
      </div>

      <section className="card">
        <div className="card-title">基本信息</div>
        <div className="detail-grid">
          <div>
            <div className="detail-label">关联录音</div>
            <div className="detail-value">
              #{current.recording_id}
              {recording ? `（时长 ${formatDuration(recording.duration_seconds)}）` : ''}
            </div>
          </div>
          <div>
            <div className="detail-label">所属项目</div>
            <div className="detail-value">#{current.project_id}</div>
          </div>
          <div>
            <div className="detail-label">提交时间</div>
            <div className="detail-value">{current.submitted_at ? new Date(current.submitted_at).toLocaleString() : '-'}</div>
          </div>
          <div>
            <div className="detail-label">审核时间</div>
            <div className="detail-value">{current.reviewed_at ? new Date(current.reviewed_at).toLocaleString() : '-'}</div>
          </div>
        </div>
        {current.status === TRANSCRIPT_STATUS_REJECTED && current.review_comment && (
          <div className="reject-comment">退回原因：{current.review_comment}</div>
        )}
        <div className="row-actions" style={{ marginTop: 12 }}>
          {current.status === TRANSCRIPT_STATUS_APPROVED && (
            <>
              <button className="btn btn-primary btn-small" onClick={() => downloadTranscript(current.id, current.version)}>
                导出全文
              </button>
              {canEdit && (
                <button
                  className="btn btn-plain btn-small"
                  onClick={() =>
                    run(async () => {
                      const next = await create({ recording_id: current.recording_id, project_id: current.project_id })
                      navigate(`/transcripts/${next.id}`, { replace: true })
                      window.location.reload()
                    }, '已基于通过版本创建新版本')
                  }
                >
                  修改并生成新版本
                </button>
              )}
            </>
          )}
        </div>
      </section>

      <section className="card">
        <div className="card-title">
          分段内容（按时间轴）
          {editable && <span className="muted"> · 编辑后可保存草稿并提交审核</span>}
          {reviewing && <span className="muted"> · 请逐段确认，或整篇退回</span>}
        </div>

        {editable ? (
          <>
            {rows.map((row, idx) => (
              <div className="segment-row" key={row.key}>
                <span className="segment-index">{idx + 1}</span>
                <input
                  className="segment-time"
                  type="number"
                  min={0}
                  value={row.start_second}
                  onChange={(e) => updateRow(row.key, { start_second: Number(e.target.value) })}
                  placeholder="开始(秒)"
                />
                <span className="muted">-</span>
                <input
                  className="segment-time"
                  type="number"
                  min={0}
                  value={row.end_second}
                  onChange={(e) => updateRow(row.key, { end_second: Number(e.target.value) })}
                  placeholder="结束(秒)"
                />
                <input
                  className="segment-speaker"
                  value={row.speaker}
                  maxLength={64}
                  onChange={(e) => updateRow(row.key, { speaker: e.target.value })}
                  placeholder="说话人"
                />
                <textarea
                  className="segment-content"
                  value={row.content}
                  onChange={(e) => updateRow(row.key, { content: e.target.value })}
                  placeholder="转写内容"
                  rows={2}
                />
                <button className="btn btn-plain btn-small" onClick={() => setRows((rs) => rs.filter((r) => r.key !== row.key))}>
                  删除
                </button>
              </div>
            ))}
            <div className="row-actions">
              <button
                className="btn btn-plain btn-small"
                onClick={() => {
                  const last = rows[rows.length - 1]
                  setRows((rs) => [
                    ...rs,
                    { key: keySeq++, start_second: last ? last.end_second : 0, end_second: last ? last.end_second : 0, speaker: '', content: '' },
                  ])
                }}
              >
                ＋ 添加分段
              </button>
              <button
                className="btn btn-primary btn-small"
                disabled={rows.length === 0}
                onClick={() =>
                  run(async () => {
                    await saveSegments(current.id, rows.map(({ start_second, end_second, speaker, content }) => ({ start_second, end_second, speaker, content })))
                  }, '草稿已保存')
                }
              >
                保存草稿
              </button>
              <button
                className="btn btn-primary btn-small"
                disabled={rows.length === 0}
                onClick={() =>
                  run(async () => {
                    await saveSegments(current.id, rows.map(({ start_second, end_second, speaker, content }) => ({ start_second, end_second, speaker, content })))
                    await submit(current.id)
                  }, '已提交审核')
                }
              >
                保存并提交审核
              </button>
            </div>
          </>
        ) : (
          <>
            {(current.segments || []).length === 0 && <div className="muted">暂无分段内容</div>}
            {(current.segments || []).map((seg, idx) => (
              <div className="segment-row" key={seg.id}>
                <span className="segment-index">{idx + 1}</span>
                <span className="segment-range">
                  {formatDuration(seg.start_second)} - {formatDuration(seg.end_second)}
                </span>
                <span className="segment-speaker-view">{seg.speaker}</span>
                <span className="segment-content-view">{seg.content}</span>
                <StatusBadge status={seg.status} type="segment" />
                {reviewing && seg.status !== 'confirmed' && (
                  <button className="btn btn-plain btn-small" onClick={() => run(async () => confirm(current.id, seg.id), '分段已确认')}>
                    确认
                  </button>
                )}
              </div>
            ))}
            {reviewing && (
              <div className="row-actions" style={{ marginTop: 12 }}>
                <button
                  className="btn btn-primary btn-small"
                  disabled={!allConfirmed}
                  title={allConfirmed ? '' : '需先确认全部分段'}
                  onClick={() => run(async () => approve(current.id), '审核已通过')}
                >
                  整篇通过
                </button>
                <input
                  value={rejectReason}
                  onChange={(e) => setRejectReason(e.target.value)}
                  placeholder="退回原因（必填）"
                  style={{ flex: 1 }}
                />
                <button
                  className="btn btn-danger btn-small"
                  disabled={!rejectReason.trim()}
                  onClick={() =>
                    run(async () => {
                      await reject(current.id, rejectReason.trim())
                      setRejectReason('')
                    }, '已退回')
                  }
                >
                  整篇退回
                </button>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}
