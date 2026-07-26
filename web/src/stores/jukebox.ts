import { create } from "zustand"
import { api } from "../api/client"

export interface JukeboxInfo {
  id: string
  name: string
  device_id: string
  device_name: string
  volume: number
  play_mode: string
  queue: string[]
  queue_idx: number
  shuffle_order: number[]
  shuffle_idx: number
  is_playing: boolean
  current_track_id: string
  path_mapping: Record<string, string>
  created_at: string
  updated_at: string
}

export interface JukeboxStatus {
  state: string
  track?: { id: string; title: string; artist: string; duration: number }
  duration: number
  volume: number
  play_mode: string
  queue: string[]
  queue_idx: number
  shuffle_order: number[]
  shuffle_idx: number
}

interface JukeboxState {
  list: JukeboxInfo[]
  loading: boolean
  loadList: () => Promise<void>
  create: (name: string, configId: string) => Promise<JukeboxInfo>
  delete: (id: string) => Promise<void>
  updatePlaying: (id: string, isPlaying: boolean) => void
}

export const useJukebox = create<JukeboxState>((set, get) => ({
  list: [],
  loading: false,

  loadList: async () => {
    set({ loading: true })
    try {
      const data = await api.jukebox.list()
      set({ list: data.jukeboxes || [], loading: false })
    } catch {
      set({ loading: false })
    }
  },

  create: async (name, configId) => {
    const j = await api.jukebox.create({ name, device_config_id: configId })
    set(s => ({ list: [...s.list, j] }))
    return j
  },

  delete: async (id) => {
    await api.jukebox.delete(id)
    set(s => ({ list: s.list.filter(j => j.id !== id) }))
  },

  updatePlaying: (id, isPlaying) => {
    set(s => ({ list: s.list.map(j => j.id === id ? { ...j, is_playing: isPlaying } : j) }))
  },
}))
