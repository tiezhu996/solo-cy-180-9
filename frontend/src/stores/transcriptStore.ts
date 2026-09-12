// 转写校对状态管理。
import { create } from 'zustand'
import {
  approveTranscript,
  confirmSegment,
  createTranscript,
  getTranscript,
  listTranscripts,
  rejectTranscript,
  saveTranscriptSegments,
  submitTranscript,
} from '../api/transcript'
import type { Transcript, TranscriptSegmentInput } from '../api/transcriptTypes'

interface TranscriptState {
  versions: Transcript[]
  current: Transcript | null
  loading: boolean
  fetchByRecording: (recordingId: number) => Promise<Transcript[]>
  fetchDetail: (id: number) => Promise<Transcript>
  create: (payload: { recording_id: number; project_id: number }) => Promise<Transcript>
  saveSegments: (id: number, segments: TranscriptSegmentInput[]) => Promise<void>
  submit: (id: number) => Promise<void>
  confirm: (id: number, segmentId: number) => Promise<void>
  approve: (id: number) => Promise<void>
  reject: (id: number, reason: string) => Promise<void>
  reset: () => void
}

export const useTranscriptStore = create<TranscriptState>((set) => ({
  versions: [],
  current: null,
  loading: false,

  async fetchByRecording(recordingId) {
    set({ loading: true })
    try {
      const res = await listTranscripts({ recording_id: recordingId })
      set({ versions: res.list || [], loading: false })
      return res.list || []
    } catch (e) {
      set({ loading: false })
      throw e
    }
  },

  async fetchDetail(id) {
    const transcript = await getTranscript(id)
    set({ current: transcript })
    return transcript
  },

  async create(payload) {
    const transcript = await createTranscript(payload)
    set((s) => ({ versions: [transcript, ...s.versions], current: transcript }))
    return transcript
  },

  async saveSegments(id, segments) {
    const transcript = await saveTranscriptSegments(id, segments)
    set({ current: transcript })
  },

  async submit(id) {
    const transcript = await submitTranscript(id)
    set({ current: transcript })
  },

  async confirm(id, segmentId) {
    const transcript = await confirmSegment(id, segmentId)
    set({ current: transcript })
  },

  async approve(id) {
    const transcript = await approveTranscript(id)
    set({ current: transcript })
  },

  async reject(id, reason) {
    const transcript = await rejectTranscript(id, reason)
    set({ current: transcript })
  },

  reset() {
    set({ versions: [], current: null })
  },
}))
