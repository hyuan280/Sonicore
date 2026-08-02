import { create } from "zustand"
import { api } from "../api/client"

export interface PlayerTrack {
  id: string; title: string; album?: string
  album_id?: string; duration: number; suffix: string
  cover_image_id?: string; track?: number; disc_number?: number
  artists?: { artist_id: string; name: string; role: string }[]
  albums?: { id: string; title?: string; track?: number; disc_number?: number; cover_image_id?: string }[]
  version?: number; version_label?: string
  versions?: { id: string; version: number; version_label: string; suffix: string; bit_rate: number; duration: number; library_id: string }[]
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
  lyrics: string
  lyricsFormat: string

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
  fetchLyrics: (trackId: string) => Promise<void>
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
  return { id: t.id, title: t.title, duration: t.duration, suffix: t.suffix, cover_image_id: t.cover_image_id, artists: t.artists, albums: t.albums, version: t.version, version_label: t.version_label, versions: t.versions }
}

function unserializeTrack(t: any): PlayerTrack {
  return { id: t.id, title: t.title, duration: t.duration, suffix: t.suffix, cover_image_id: t.cover_image_id, artists: t.artists, albums: t.albums, version: t.version ?? 0, version_label: t.version_label ?? "", versions: t.versions }
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

  const st = usePlayer.getState()
  if (st.track) {
    st.fetchLyrics(st.track.id)
  }
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
  lyrics: "",
  lyricsFormat: "",

  setQueue: (tracks, startIdx = 0, playlistId) => {
    const { mode } = get()
    const update: any = {
      queue: [...tracks],
      playing: true,
      position: 0,
      lyrics: "",
      lyricsFormat: "",
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

    const firstTrack = update.track
    if (firstTrack) {
      get().fetchLyrics(firstTrack.id)
    }
  },

  play: (track) => {
    set({ track, playing: true, position: 0, lyrics: "", lyricsFormat: "", playEpoch: get().playEpoch + 1 })
    savePlayerState()
    get().fetchLyrics(track.id)
  },

  playIndex: (idx) => {
    const { queue, mode, shuffleOrder } = get()
    if (idx < 0 || idx >= queue.length) return
    const track = queue[idx]
    const update: any = {
      queueIdx: idx,
      track,
      playing: true,
      position: 0,
      lyrics: "",
      lyricsFormat: "",
      playEpoch: get().playEpoch + 1,
    }
    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const pos = shuffleOrder.indexOf(idx)
      update.shuffleIdx = pos >= 0 ? pos : shuffleOrder.length - 1
    }
    set(update)
    savePlayerState()
    saveQueue()
    get().fetchLyrics(track.id)
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
      const track = queue[nextQi]
      set({
        shuffleIdx: nextSi,
        queueIdx: nextQi,
        track,
        playing: true,
        position: 0,
        lyrics: "",
        lyricsFormat: "",
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      get().fetchLyrics(track.id)
      return
    }

    if (mode === "normal") {
      if (queueIdx >= queue.length - 1) {
        set({ playing: false, position: 0, lyrics: "", lyricsFormat: "" })
        savePlayerState()
        return
      }
      const nextIdx = queueIdx + 1
      const track = queue[nextIdx]
      set({ queueIdx: nextIdx, track, playing: true, position: 0, lyrics: "", lyricsFormat: "", playEpoch: get().playEpoch + 1 })
      savePlayerState()
      saveQueue()
      get().fetchLyrics(track.id)
      return
    }

    const nextIdx = (queueIdx + 1) % queue.length
    const track = queue[nextIdx]
    set({ queueIdx: nextIdx, track, playing: true, position: 0, lyrics: "", lyricsFormat: "", playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
    get().fetchLyrics(track.id)
  },

  next: () => {
    const { mode, queue, queueIdx, shuffleOrder, shuffleIdx } = get()
    if (queue.length === 0) return

    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const nextSi = (shuffleIdx + 1) % shuffleOrder.length
      const nextQi = shuffleOrder[nextSi]
      const track = queue[nextQi]
      set({
        shuffleIdx: nextSi,
        queueIdx: nextQi,
        track,
        playing: true,
        position: 0,
        lyrics: "",
        lyricsFormat: "",
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      get().fetchLyrics(track.id)
      return
    }

    const nextIdx = (queueIdx + 1) % queue.length
    const track = queue[nextIdx]
    set({ queueIdx: nextIdx, track, playing: true, position: 0, lyrics: "", lyricsFormat: "", playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
    get().fetchLyrics(track.id)
  },

  prev: () => {
    const { mode, queue, queueIdx, shuffleOrder, shuffleIdx } = get()
    if (queue.length === 0) return

    if (mode === "shuffle" && shuffleOrder.length > 0) {
      const prevSi = (shuffleIdx - 1 + shuffleOrder.length) % shuffleOrder.length
      const prevQi = shuffleOrder[prevSi]
      const track = queue[prevQi]
      set({
        shuffleIdx: prevSi,
        queueIdx: prevQi,
        track,
        playing: true,
        position: 0,
        lyrics: "",
        lyricsFormat: "",
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      get().fetchLyrics(track.id)
      return
    }

    const prevIdx = (queueIdx - 1 + queue.length) % queue.length
    const track = queue[prevIdx]
    set({ queueIdx: prevIdx, track, playing: true, position: 0, lyrics: "", lyricsFormat: "", playEpoch: get().playEpoch + 1 })
    savePlayerState()
    saveQueue()
    get().fetchLyrics(track.id)
  },

  setPosition: (pos) => set({ position: pos }),
  setVolume: (v) => { set({ volume: v }); savePlayerState() },
  setPlaying: (p) => { set({ playing: p }) },
  togglePlay: () => {
    const s = get()
    if (s.playing) {
      set({ playing: false })
      savePlayerState()
      return
    }
    if (s.track) {
      set({ playing: true })
      savePlayerState()
      return
    }
    if (s.queue.length > 0) {
      const { mode } = s
      const idx = mode === "shuffle" && s.shuffleOrder.length > 0 ? s.shuffleOrder[0] : 0
      const track = s.queue[idx] || null
      set({
        queueIdx: idx,
        shuffleIdx: 0,
        track,
        playing: true,
        position: 0,
        lyrics: "",
        lyricsFormat: "",
        playEpoch: get().playEpoch + 1,
      })
      savePlayerState()
      saveQueue()
      if (track) get().fetchLyrics(track.id)
    }
  },

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
      update.track = null; update.playing = false; update.queueIdx = 0; update.lyrics = ""; update.lyricsFormat = ""
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
    set({ queue: [], queueIdx: 0, shuffleOrder: [], shuffleIdx: 0, track: null, playing: false, position: 0, currentPlaylistId: null, lyrics: "", lyricsFormat: "" })
    savePlayerState()
    saveQueue()
  },

  setCurrentPlaylistId: (id) => set({ currentPlaylistId: id }),

  fetchLyrics: async (trackId) => {
    try {
      const res = await api.data.lyrics(trackId)
      set({ lyrics: res.lyrics || "", lyricsFormat: res.format || "" })
    } catch {
      set({ lyrics: "", lyricsFormat: "" })
    }
  },
}))
