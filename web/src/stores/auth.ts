import { create } from "zustand"
import type { User, Role } from "../types"
import { api } from "../api/client"

function clearAuthStorage() {
  localStorage.removeItem("token")
  localStorage.removeItem("refresh_token")
  localStorage.removeItem("session_token")
  localStorage.removeItem("role")
  localStorage.removeItem("playerState")
  localStorage.removeItem("playerQueue")
}

interface AuthState {
  user: User | null; token: string | null; loading: boolean
  allowRegistration: boolean
  login: (username: string, password: string) => Promise<void>
  register: (username: string, email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  loadUser: () => Promise<void>
  loadRegistrationStatus: () => Promise<void>
}

export const useAuth = create<AuthState>((set) => ({
  user: null, token: localStorage.getItem("token"), loading: false,
  allowRegistration: true,

  login: async (username, password) => {
    const data = await api.auth.login({ username, password })
    localStorage.setItem("token", data.token)
    localStorage.setItem("refresh_token", data.refresh_token)
    if (data.session_token) localStorage.setItem("session_token", data.session_token)
    if (data.role) localStorage.setItem("role", data.role)
    set({ token: data.token, user: { id: data.user_id, username: data.username, email: "", role: data.role, created_at: "" } })
  },

  register: async (username, email, password) => {
    const data = await api.auth.register({ username, email, password })
    localStorage.setItem("token", data.token)
    localStorage.setItem("refresh_token", data.refresh_token)
    if (data.session_token) localStorage.setItem("session_token", data.session_token)
    if (data.role) localStorage.setItem("role", data.role)
    set({ token: data.token, user: { id: data.user_id, username: data.username, email, role: data.role, created_at: "" } })
  },

  logout: async () => {
    await api.auth.logout().catch(() => {})
    clearAuthStorage()
    set({ user: null, token: null })
  },

  loadUser: async () => {
    try {
      const u = await api.auth.me()
      localStorage.setItem("role", u.role)
      set({ user: u })
    } catch {
      clearAuthStorage()
      set({ user: null, token: null })
    }
  },

  loadRegistrationStatus: async () => {
    try {
      const data = await api.auth.registrationStatus()
      set({ allowRegistration: data.allow_registration })
    } catch {}
  },
}))

export function getRole(): Role | null {
  return (localStorage.getItem("role") as Role) || null
}

export function isAdmin(): boolean {
  const role = getRole()
  return role === "admin" || role === "super_admin"
}
