import { create } from "zustand"
import { api } from "../api/client"

export interface PlayerTrack {
  id: string; title: string; artist: string; album: string
  album_id: string; duration: number; suffix: string
}

interface PlayerState {
  track: PlayerTrack | null
  queue: PlayerTrack[]
  queueIdx: number
  shuffleOrder: number[]
  shuffleIdx: number
  playing: boolean
  position: number
  volume: number
  mode: "normal" | "all" | "one" | "shuffle"
  playEpoch: number
  currentPlaylistId: string | null

  setQueue: (tracks: PlayerTrack[], startIdx?: number, playlistId?: string) => void
  play: (track: PlayerTrack) => void
  playIndex: (idx: number) => void
  advanceTrack: () => void
  next: () => void
  prev: () => void
  setPosition: (pos: number) => void
  setVolume: (v: number) => void
  setPlaying: (p: boolean) => void
  togglePlay: () => void
  cycleMode: () => void
  addToQueue: (tracks: PlayerTrack[]) => void
  removeFromQueue: (index: number) => void
  clearQueue: () => void
  setCurrentPlaylistId: (id: string | null) => void
}

function shuffleArray<T>(arr: T[]): T[] {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

const SYNC_KEY = "player_state"

function serializeTrack(t: PlayerTrack) {
  return { id: t.id, title: t.title, artist: t.artist, album: t.album, album_id: t.album_id, duration: t.duration, suffix: t.suffix }
}

function unserializeTrack(t: any): PlayerTrack {
  return { id: t.id, title: t.title, artist: t.artist, album: t.album, album_id: t.album_id, duration: t.duration, suffix: t.suffix }
}

export function savePlayerState() {
  try {
    const s = usePlayer.getState()
    const data: any = {}
    data.track = s.track ? serializeTrack(s.track) : null
    data.volume = s.volume
    data.playing = s.playing
    data.position = s.position
    data.mode = s.mode
    data.currentPlaylistId = s.currentPlaylistId
    localStorage.setItem("playerState", JSON.stringify(data))
    api.user.updateSettings({ [SYNC_KEY]: JSON.stringify(data) }).catch(() => {})
  } catch {}
}

export function saveQueue() {
  try {
    const s = usePlayer.getState()
    const tracks = s.queue.map(serializeTrack)
    const mode = s.mode
    const queueIdx = s.queueIdx
    const shuffleOrder = s.shuffleOrder
    const shuffleIdx = s.shuffleIdx

    localStorage.setItem("playerQueue", JSON.stringify({ tracks, queueIdx, shuffleOrder, shuffleIdx, mode }))

    api.user.saveQueue({
      track_ids: s.queue.map(t => t.id),
      queue_idx: queueIdx,
      shuffle_order: shuffleOrder,
      shuffle_idx: shuffleIdx,
      mode,
    }).catch(() => {})
  } catch {}
}

export async function restorePlayerState() {
  // Fast path: restore from localStorage for instant display
  try {
    const local = localStorage.getItem("playerState")
    if (local) {
      const data = JSON.parse(local)
      usePlayer.setState({
        track: data.track || null,
        volume: data.volume ?? 0.8,
        playing: false, // browser blocks autoplay
        position: data.position ?? 0,
        currentPlaylistId: data.currentPlaylistId ?? null,
      })
    }
    const localQueue = localStorage.getItem("playerQueue")
    if (localQueue) {
      const q = JSON.parse(localQueue)
      usePlayer.setState({
        queue: (q.tracks || []).map(unserializeTrack),
        queueIdx: q.queueIdx || 0,
        shuffleOrder: q.shuffleOrder || [],
        shuffleIdx: q.shuffleIdx || 0,
        mode: q.mode || "normal",
      })
    }
  } catch {}

  // Authoritative: load from server
  try {
    const queueRes: any = await api.user.getQueue()
    if (queueRes && queueRes.tracks) {
      usePlayer.setState({
        queue: queueRes.tracks.map(unserializeTrack),
        queueIdx: queueRes.queue_idx || 0,
        shuffleOrder: queueRes.shuffle_order || [],
        shuffleIdx: queueRes.shuffle_idx || 0,
        mode: queueRes.mode || "normal",
      })
    }

    const res: any = await api.user.getSettings()
    const raw = res?.settings?.[SYNC_KEY]
    if (raw) {
      const data = JSON.parse(raw)
      usePlayer.setState({
        track: data.track || data.lastTrack || null,
        volume: data.volume ?? 0.8,
        playing: false, // browser blocks autoplay
        position: data.position ?? 0,
        currentPlaylistId: data.currentPlaylistId ?? null,
      })
    }
  } catch {}
}

export const usePlayer = create<PlayerState>((set, get) => ({
  track: null,
  queue: [],
  queueIdx: 0,
  shuffleOrder: [],
  shuffleIdx: 0,
  playing: false,
  position: 0,
  volume: 0.8,
  mode: "normal",
  playEpoch: 0,
  currentPlaylistId: null,

  setQueue: (tracks, startIdx = 0, playlistId) => {
    const { mode } = get()
    const update: any = {
      queue: [...tracks],
      playing: true,
      position: 0,
      playEpoch: get().playEpoch + 1,
      currentPlaylistId: playlistId ?? null,
    }

    if (mode === "shuffle") {
      const order = shuffleArray([...Array(tracks.length).keys()])
      update.shuffleOrder = order
      update.shuffleIdx = 0
      update.queueIdx = order[0]
      update.track = tracks[order[0]] || null
    } else {
      update.queueIdx = startIdx
      update.track = tracks[startIdx] || null
    }

    set(update)
    savePlayerState()
    saveQueue()
  },

  play: (track) => {
    set({ track, playing: true, position: 0, playEpoch: get().playEpoch + 1 })
    savePlayerState()
  },

  playIndex: (idx) => {
    const { queue, mode, shuffleOrder } = get()
    if (idx < 0 || idx >= queue.length) return
    const update: any = {
      queueIdx: idx,
      track: queue[idx],
      playing: true,
      position: 0,
      playEpoch: get().playEpoch + 1,
    }
    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const pos = shuffleOrder.indexOf(idx)
      update.shuffleIdx = pos >= 0 ? pos : shuffleOrder.length - 1
    }
    set(update)
    savePlayerState()
    saveQueue()
  },

  advanceTrack: () => {
    const { mode, queue, queueIdx, shuffleOrder, shuffleIdx } = get()
    if (queue.length === 0) return

    if (mode === "one") {
      set({ playing: true, position: 0, playEpoch: get().playEpoch + 1 })
      savePlayerState()
      return
    }

    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const nextSi = (shuffleIdx + 1) % shuffleOrder.length
      const nextQi = shuffleOrder[nextSi]
      set({
        shuffleIdx: nextSi,
        queueIdx: nextQi,
        track: queue[nextQi],
        playing: true,
        position: 0,
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      return
    }

    if (mode === "normal") {
      if (queueIdx >= queue.length - 1) {
        set({ playing: false, position: 0 })
        savePlayerState()
        return
      }
      const nextIdx = queueIdx + 1
      set({ queueIdx: nextIdx, track: queue[nextIdx], playing: true, position: 0, playEpoch: get().playEpoch + 1 })
      savePlayerState()
      saveQueue()
      return
    }

    const nextIdx = (queueIdx + 1) % queue.length
    set({ queueIdx: nextIdx, track: queue[nextIdx], playing: true, position: 0, playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
  },

  next: () => {
    const { mode, queue, queueIdx, shuffleOrder, shuffleIdx } = get()
    if (queue.length === 0) return

    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const nextSi = (shuffleIdx + 1) % shuffleOrder.length
      const nextQi = shuffleOrder[nextSi]
      set({
        shuffleIdx: nextSi,
        queueIdx: nextQi,
        track: queue[nextQi],
        playing: true,
        position: 0,
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      return
    }

    const nextIdx = (queueIdx + 1) % queue.length
    set({ queueIdx: nextIdx, track: queue[nextIdx], playing: true, position: 0, playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
  },

  prev: () => {
    const { mode, queue, queueIdx, shuffleOrder, shuffleIdx } = get()
    if (queue.length === 0) return

    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const prevSi = (shuffleIdx - 1 + shuffleOrder.length) % shuffleOrder.length
      const prevQi = shuffleOrder[prevSi]
      set({
        shuffleIdx: prevSi,
        queueIdx: prevQi,
        track: queue[prevQi],
        playing: true,
        position: 0,
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      return
    }

    const prevIdx = (queueIdx - 1 + queue.length) % queue.length
    set({ queueIdx: prevIdx, track: queue[prevIdx], playing: true, position: 0, playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
  },

  setPosition: (pos) => set({ position: pos }),
  setVolume: (v) => { set({ volume: v }); savePlayerState() },
  setPlaying: (p) => { set({ playing: p }) },
  togglePlay: () => set(s => ({ playing: !s.playing })),

  cycleMode: () => {
    const order: ("normal" | "all" | "one" | "shuffle")[] = ["normal", "all", "one", "shuffle"]
    const { mode, queue, queueIdx } = get()
    const current = order.indexOf(mode)
    const nextMode = order[(current + 1) % 4]
    const update: any = { mode: nextMode }

    if (nextMode === "shuffle") {
      if (queue.length === 0) { set(update); savePlayerState(); saveQueue(); return }
      const indices = [...Array(queue.length).keys()]
      const shuffled = shuffleArray(indices)
      const pos = shuffled.indexOf(queueIdx)
      update.shuffleOrder = shuffled
      update.shuffleIdx = pos >= 0 ? pos : 0
    } else if (mode === "shuffle") {
      update.shuffleOrder = []
      update.shuffleIdx = 0
    }

    set(update)
    savePlayerState()
    saveQueue()
  },

  addToQueue: (tracks) => {
    const { queue, mode, shuffleOrder, shuffleIdx, track } = get()
    const existingIds = new Set(queue.map(t => t.id))
    const newQueue = [...queue, ...tracks.filter(t => !existingIds.has(t.id))]
    const update: any = { queue: newQueue }

    if (mode === "shuffle") {
      const newIndices = [...Array(newQueue.length).keys()]
      const shuffled = shuffleArray(newIndices)
      if (track) {
        const curQi = newQueue.findIndex(t => t.id === track.id)
        if (curQi >= 0) {
          const pos = shuffled.indexOf(curQi)
          if (pos >= 0 && pos !== shuffleIdx) {
            ;[shuffled[shuffleIdx], shuffled[pos]] = [shuffled[pos], shuffled[shuffleIdx]]
          }
        }
      }
      update.shuffleOrder = shuffled
      update.shuffleIdx = shuffleIdx < shuffled.length ? shuffleIdx : 0
    }

    set(update)
    saveQueue()
  },

  removeFromQueue: (index) => {
    const { queue, queueIdx, mode, shuffleOrder, shuffleIdx, track, playing, playEpoch } = get()
    if (index < 0 || index >= queue.length) return
    const newQueue = queue.filter((_, i) => i !== index)
    let newIdx = queueIdx
    if (index < queueIdx) newIdx--
    const isCurrent = index === queueIdx
    const isShuffle = mode === "shuffle"
    const update: any = { queue: newQueue, queueIdx: newIdx }

    if (newQueue.length === 0) {
      update.track = null; update.playing = false; update.queueIdx = 0
    } else if (isCurrent) {
      const nextTrack = newQueue[Math.min(newIdx, newQueue.length - 1)]
      update.track = nextTrack; update.position = 0
      update.playEpoch = playEpoch + 1
      if (playing) update.playing = true
    }

    if (isShuffle && shuffleOrder.length > 0) {
      update.shuffleOrder = shuffleOrder.filter(i => i !== index).map(i => i > index ? i - 1 : i)
    }
    set(update)
    saveQueue()
    savePlayerState()
  },

  clearQueue: () => {
    set({ queue: [], queueIdx: 0, shuffleOrder: [], shuffleIdx: 0, track: null, playing: false, position: 0, currentPlaylistId: null })
    savePlayerState()
    saveQueue()
  },

  setCurrentPlaylistId: (id) => set({ currentPlaylistId: id }),
}))
