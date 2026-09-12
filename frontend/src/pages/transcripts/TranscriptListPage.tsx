// 转写校对列表页：录音版本列表 / 待审核队列 / 已通过全文检索与导出。
import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import EmptyState from '../../components/EmptyState'
import StatusBadge from '../../components/StatusBadge'
import { ROLE_ADMIN, ROLE_ARCHIVIST, ROLE_INTERVIEWER, TRANSCRIPT_STATUS_SUBMITTED } from '../../constants'
import { getRecording } from '../../api/recording'
import { downloadTranscript, listTranscripts, searchTranscripts } from '../../api/transcript'
import type { Recording } from '../../api/types'
import type { Transcript, TranscriptSegment } from '../../api/transcriptTypes'
import { useAuthStore } from '../../stores/authStore'
import { useTranscriptStore } from '../../stores/transcriptStore'
import { formatDateTime, formatDuration } from '../../utils/format'

export default function TranscriptListPage() {
  const [params] = useSearchParams()
  const recordingId = Number(params.get('recording_id') || 0)
  const navigate = useNavigate()
  const { hasRole } = useAuthStore()
  const { versions, fetchByRecording, create } = useTranscriptStore()
  const [recording, setRecording] = useState<Recording | null>(null)
  const [queue, setQueue] = useState<Transcript[]>([])
  const [keyword, setKeyword] = useState('')
  const [results, setResults] = useState<TranscriptSegment[] | null>(null)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const canEdit = hasRole(ROLE_INTERVIEWER, ROLE_ADMIN)
  const canReview = hasRole(ROLE_ARCHIVIST, ROLE_ADMIN)

  const toast = (msg: string) => {
    setMessage(msg)
    setTimeout(() => setMessage(''), 3000)
  }

  const loadQueue = useCallback(async () => {
    const res = await listTranscripts({ status: TRANSCRIPT_STATUS_SUBMITTED })
    setQueue(res.list || [])
  }, [])

  useEffect(() => {
    if (recordingId) {
      fetchByRecording(recordingId).catch((e) => setError(e.message))
      getRecording(recordingId).then(setRecording).catch(() => {})
    } else if (canReview) {
      loadQueue().catch((e) => setError(e.message))
    }
  }, [recordingId, canReview, fetchByRecording, loadQueue])

  const handleCreate = async () => {
    if (!recording) return
    setError('')
    try {
      const t = await create({ recording_id: recording.id, project_id: recording.project_id })
      toast(t.version > 1 ? `已创建新版本 v${t.version}` : '转写草稿已创建')
      navigate(`/transcripts/${t.id}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建失败')
    }
  }

  const handleSearch = async () => {
    setError('')
    try {
      const res = await searchTranscripts({ q: keyword.trim() })
      setResults(res.list || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : '检索失败')
    }
  }

  const activeVersion = versions.find((v) => v.status !== 'approved')

  return (
    <div className="page">
      {message && <div className="toast success">{message}</div>}
      {error && <div className="toast error">{error}</div>}
      <div className="page-header">
        <h2>转写校对</h2>
      </div>

      {recordingId > 0 && (
        <section className="card">
          <div className="card-title">
            录音 #{recordingId} 的转写版本
            {recording && <span className="muted">（时长 {formatDuration(recording.duration_seconds)}，状态 {recording.status}）</span>}
          </div>
          {versions.length === 0 ? (
            <EmptyState title="还没有转写稿" description="为已就绪的录音创建转写草稿，按时间轴分段校对" />
          ) : (
            <ul className="question-list">
              {versions.map((v) => (
                <li key={v.id} className="question-item">
                  <span className="question-index">v{v.version}</span>
                  <StatusBadge status={v.status} type="transcript" />
                  <span className="question-content muted">更新于 {formatDateTime(v.updated_at)}</span>
                  {v.status === 'rejected' && v.review_comment && <span className="muted">退回：{v.review_comment}</span>}
                  <button className="btn btn-plain btn-small" onClick={() => navigate(`/transcripts/${v.id}`)}>
                    打开
                  </button>
                </li>
              ))}
            </ul>
          )}
          {canEdit && recording && !activeVersion && (
            <div className="row-actions" style={{ marginTop: 12 }}>
              <button className="btn btn-primary btn-small" onClick={handleCreate}>
                {versions.length > 0 ? '修改并生成新版本' : '创建转写草稿'}
              </button>
            </div>
          )}
        </section>
      )}

      {canReview && recordingId === 0 && (
        <section className="card">
          <div className="card-title">待审核队列</div>
          {queue.length === 0 ? (
            <EmptyState title="没有待审核的转写稿" description="采访员提交后会出现在这里" />
          ) : (
            <ul className="question-list">
              {queue.map((t) => (
                <li key={t.id} className="question-item">
                  <span className="question-index">#{t.id}</span>
                  <span className="question-content">
                    录音 #{t.recording_id} · v{t.version} · 项目 #{t.project_id}
                  </span>
                  <span className="muted">提交于 {t.submitted_at ? formatDateTime(t.submitted_at) : '-'}</span>
                  <button className="btn btn-primary btn-small" onClick={() => navigate(`/transcripts/${t.id}`)}>
                    审核
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      <section className="card">
        <div className="card-title">已通过转写全文检索</div>
        <div className="inline-form">
          <input value={keyword} onChange={(e) => setKeyword(e.target.value)} placeholder="输入关键词检索已通过的转写内容或说话人" />
          <button className="btn btn-primary" disabled={!keyword.trim()} onClick={handleSearch}>
            检索
          </button>
        </div>
        {results && (
          <div style={{ marginTop: 12 }}>
            {results.length === 0 ? (
              <div className="muted">未检索到已通过的转写内容</div>
            ) : (
              <ul className="question-list">
                {results.map((seg) => (
                  <li key={seg.id} className="question-item">
                    <span className="question-index">{formatDuration(seg.start_second)}</span>
                    <span className="question-content">
                      <strong>{seg.speaker}</strong>：{seg.content}
                    </span>
                    <button className="btn btn-plain btn-small" onClick={() => navigate(`/transcripts/${seg.transcript_id}`)}>
                      查看全文
                    </button>
                    <button className="btn btn-plain btn-small" onClick={() => downloadTranscript(seg.transcript_id, 0)}>
                      导出
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </section>
    </div>
  )
}
