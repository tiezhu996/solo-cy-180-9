// 转写校对模块 API。
import { fetchBlob, get, post, put } from '../utils/request'
import type { Transcript, TranscriptSegment, TranscriptSegmentInput } from './transcriptTypes'

export function createTranscript(payload: { recording_id: number; project_id: number }): Promise<Transcript> {
  return post<Transcript>('/transcripts', payload)
}

export function listTranscripts(params: { project_id?: number; recording_id?: number; status?: string }): Promise<{ list: Transcript[] }> {
  return get<{ list: Transcript[] }>('/transcripts', params as Record<string, unknown>)
}

export function getTranscript(id: number): Promise<Transcript> {
  return get<Transcript>(`/transcripts/${id}`)
}

export function saveTranscriptSegments(id: number, segments: TranscriptSegmentInput[]): Promise<Transcript> {
  return put<Transcript>(`/transcripts/${id}/segments`, { segments })
}

export function submitTranscript(id: number): Promise<Transcript> {
  return post<Transcript>(`/transcripts/${id}/submit`)
}

export function confirmSegment(id: number, segmentId: number): Promise<Transcript> {
  return post<Transcript>(`/transcripts/${id}/segments/${segmentId}/confirm`)
}

export function approveTranscript(id: number): Promise<Transcript> {
  return post<Transcript>(`/transcripts/${id}/approve`)
}

export function rejectTranscript(id: number, reason: string): Promise<Transcript> {
  return post<Transcript>(`/transcripts/${id}/reject`, { reason })
}

export function searchTranscripts(params: { q: string; project_id?: number }): Promise<{ list: TranscriptSegment[] }> {
  return get<{ list: TranscriptSegment[] }>('/transcripts/search', params as Record<string, unknown>)
}

export async function downloadTranscript(id: number, version: number): Promise<void> {
  const blob = await fetchBlob(`/transcripts/${id}/export`)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `transcript_${id}_v${version}.txt`
  a.click()
  URL.revokeObjectURL(url)
}
