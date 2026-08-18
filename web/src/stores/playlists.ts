import { create } from "zustand";
import { api } from "../api/client";

interface PlaylistsState {
  list: any[];
  loading: boolean;
  loaded: boolean;
  load: (force?: boolean) => Promise<void>;
  create: (name: string) => Promise<any>;
  remove: (id: string) => Promise<void>;
}

export const usePlaylists = create<PlaylistsState>((set, get) => ({
  list: [],
  loading: false,
  loaded: false,

  load: async (force = false) => {
    if (get().loaded && !force) return;
    set({ loading: true });
    try {
      const data = await api.user.playlists();
      set({ list: data.items || [], loading: false, loaded: true });
    } catch {
      set({ loading: false });
    }
  },

  create: async (name) => {
    const res = await api.user.createPlaylist(name);
    set((s) => ({
      list: [
        ...s.list,
        { id: res.id, name, track_ids: [], is_public: false, created_at: new Date().toISOString() },
      ],
    }));
    return res;
  },

  remove: async (id) => {
    await api.user.deletePlaylist(id);
    set((s) => ({ list: s.list.filter((p) => p.id !== id) }));
  },
}));
