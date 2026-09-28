import { create } from "zustand";
import { api } from "../api/client";
import type { Library } from "../types";

interface LibraryState {
  libraries: Library[];
  loading: boolean;
  // error holds the last load failure so the UI can surface "could not load
  // libraries" instead of showing an empty list as if there were none.
  error: unknown;
  load: () => Promise<void>;
}

export const useLibrary = create<LibraryState>((set) => ({
  libraries: [],
  loading: false,
  error: null,

  load: async () => {
    set({ loading: true });
    try {
      const libs = await api.libraries.list();
      set({ libraries: libs, error: null });
    } catch (err) {
      // Keep the previous list on a transient failure instead of leaving the
      // store loading forever or throwing an unhandled rejection at callers.
      console.error("failed to load libraries", err);
      set({ error: err });
    } finally {
      set({ loading: false });
    }
  },
}));
