import { create } from "zustand";
import { api } from "../api/client";
import type { Library } from "../types";

interface LibraryState {
  libraries: Library[];
  loading: boolean;
  load: () => Promise<void>;
}

export const useLibrary = create<LibraryState>((set) => ({
  libraries: [],
  loading: false,

  load: async () => {
    set({ loading: true });
    const libs = await api.libraries.list();
    set({ libraries: libs, loading: false });
  },
}));
