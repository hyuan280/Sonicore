import { create } from "zustand";
import { api } from "../api/client";

export interface Playlist {
  id: string;
  name: string;
  track_ids?: string[];
  is_public?: boolean;
  created_at?: string;
}

interface PlaylistsState {
  list: Playlist[];
  loading: boolean;
  loaded: boolean;
  // These never reject; they report failure via their return value so callers
  // can react without try/catch and without unhandled rejections.
  load: (force?: boolean) => Promise<boolean>;
  create: (name: string) => Promise<Playlist | null>;
  remove: (id: string) => Promise<boolean>;
}

export const usePlaylists = create<PlaylistsState>((set, get) => ({
  list: [],
  loading: false,
  loaded: false,

  load: async (force = false) => {
    if (get().loaded && !force) return true;
    set({ loading: true });
    try {
      const data = await api.user.playlists();
      set({ list: data.items || [], loading: false, loaded: true });
      return true;
    } catch {
      set({ loading: false });
      return false;
    }
  },

  create: async (name) => {
    let res;
    try {
      res = await api.user.createPlaylist(name);
    } catch (err) {
      console.warn("[playlists] create failed", err);
      return null;
    }
    const playlist: Playlist = {
      id: res.id,
      name,
      track_ids: [],
      is_public: false,
      created_at: new Date().toISOString(),
    };
    set((s) => ({ list: [...s.list, playlist] }));
    return playlist;
  },

  remove: async (id) => {
    try {
      await api.user.deletePlaylist(id);
    } catch (err) {
      console.warn("[playlists] remove failed", err);
      return false;
    }
    set((s) => ({ list: s.list.filter((p) => p.id !== id) }));
    return true;
  },
}));
