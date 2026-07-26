import { create } from "zustand"
import { api } from "../api/client"
import type { Library } from "../types"

export const ALL_LIBS = "__all__"

export function isAllLibs(id: string | null) { return id === ALL_LIBS }

export function useActiveLibIds(): { ids: string[]; isAll: boolean } {
  const { libraries, activeId } = useLibrary()
  if (!activeId) return { ids: [], isAll: false }
  if (isAllLibs(activeId)) return { ids: libraries.map(l => l.id), isAll: true }
  return { ids: [activeId], isAll: false }
}

interface LibraryState {
  libraries: Library[]
  activeId: string | null
  loading: boolean
  load: () => Promise<void>
  setActive: (id: string) => void
}

export const useLibrary = create<LibraryState>((set) => ({
  libraries: [],
  activeId: localStorage.getItem("activeLib") || ALL_LIBS,
  loading: false,

  load: async () => {
    set({ loading: true })
    const libs = await api.libraries.list()
    const stored = localStorage.getItem("activeLib")
    const activeId = stored || ALL_LIBS
    set({ libraries: libs, activeId, loading: false })
  },

  setActive: (id) => {
    localStorage.setItem("activeLib", id)
    set({ activeId: id })
  },
}))
