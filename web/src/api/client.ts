const BASE = ""

async function request(path: string, opts: RequestInit = {}): Promise<any> {
  const token = localStorage.getItem("token")
  const headers: Record<string, string> = {}
  if (token) headers["Authorization"] = `Bearer ${token}`
  if (!(opts.body instanceof FormData)) headers["Content-Type"] = "application/json"

  const res = await fetch(BASE + path, { ...opts, headers })

  if (res.status === 401 && path !== "/api/auth/login" && path !== "/api/auth/register") {
    const ok = await tryRefresh()
    if (ok) return request(path, opts)
    localStorage.clear(); window.location.href = "/login"
    throw new Error("session expired")
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    const err: any = new Error(body.error || res.statusText)
    err.status = res.status
    err.error = body.error
    err.code = body.code
    throw err
  }

  if (res.status === 204) return null
  return res.json()
}

async function tryRefresh(): Promise<boolean> {
  const rt = localStorage.getItem("refresh_token")
  if (!rt) return false
  try {
    const data = await fetch(BASE + "/api/auth/refresh", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: rt }),
    }).then(r => r.json())
    if (data.token) {
      localStorage.setItem("token", data.token)
      localStorage.setItem("refresh_token", data.refresh_token)
      if (data.session_token) localStorage.setItem("session_token", data.session_token)
      if (data.role) localStorage.setItem("role", data.role)
      return true
    }
  } catch {}
  return false
}

export const api = {
  auth: {
    login: (d: any) => request("/api/auth/login", { method: "POST", body: JSON.stringify(d) }),
    register: (d: any) => request("/api/auth/register", { method: "POST", body: JSON.stringify(d) }),
    logout: () => request("/api/auth/logout", { method: "POST" }),
    me: () => request("/api/user/me"),
    renewSession: (sessionToken?: string) =>
      request("/api/user/me", { method: "POST", body: JSON.stringify({ session_token: sessionToken || "" }) }),
    changePassword: (oldPw: string, newPw: string) =>
      request("/api/user/password", { method: "PUT", body: JSON.stringify({ old_password: oldPw, new_password: newPw }) }),
    registrationStatus: () => request("/api/auth/registration-status"),
  },
  admin: {
    users: () => request("/api/admin/users"),
    updateRole: (id: string, role: string) =>
      request(`/api/admin/users/${id}/role`, { method: "PUT", body: JSON.stringify({ role }) }),
    getSettings: () => request("/api/admin/settings"),
    updateSettings: (s: any) => request("/api/admin/settings", { method: "PUT", body: JSON.stringify(s) }),
    dirs: (dir: string) => request(`/api/admin/dirs?path=${encodeURIComponent(dir)}`),
  },
  libraries: {
    list: () => request("/api/libraries"),
    get: (id: string) => request(`/api/libraries/${id}`),
    create: (d: any) => request("/api/libraries", { method: "POST", body: JSON.stringify(d) }),
    delete: (id: string) => request(`/api/libraries/${id}`, { method: "DELETE" }),
    scan: (id: string, mode?: string) => request(`/api/libraries/${id}/scan${mode ? `?mode=${mode}` : ""}`, { method: "POST" }),
    scanStatus: (id: string) => request(`/api/libraries/${id}/scan/status`),
  },
	data: {
		tracks: (libId?: string, page = 1, perPage = 50) =>
			request(`/api/data/tracks?page=${page}&per_page=${perPage}${libId ? `&libId=${encodeURIComponent(libId)}` : ""}`),
		tracksByIds: (ids: string[]) =>
			request("/api/data/tracks/byids", { method: "POST", body: JSON.stringify({ ids }) }),
		artists: (page = 1, perPage = 9999) =>
			request(`/api/data/artists?page=${page}&per_page=${perPage}`),
		artist: (artistId: string, page = 1, perPage = 30) =>
			request(`/api/data/artists/${artistId}?page=${page}&per_page=${perPage}`),
		albums: (page = 1, perPage = 9999) =>
			request(`/api/data/albums?page=${page}&per_page=${perPage}`),
		album: (albumId: string, page = 1, perPage = 30) =>
			request(`/api/data/albums/${albumId}?page=${page}&per_page=${perPage}`),
		lyrics: (trackId: string) =>
			request(`/api/data/tracks/lyrics?trackid=${encodeURIComponent(trackId)}`),
		updateLyricsOffset: (trackId: string, offset: number) =>
			request("/api/data/tracks/lyrics", { method: "POST", body: JSON.stringify({ trackid: trackId, offset }) }),
		search: (q: string) => request(`/api/data/search?q=${encodeURIComponent(q)}`),
	},
  user: {
    favorites: (type?: string, page = 1, perPage = 30) => request(`/api/user/favorites/list?type=${type || "track"}&page=${page}&per_page=${perPage}`),
    addFavorites: (itemType: string, itemIds: string[]) =>
      request("/api/user/favorites/add", { method: "POST", body: JSON.stringify({ item_type: itemType, item_ids: itemIds }) }),
    removeFavorites: (itemType: string, itemIds: string[]) =>
      request("/api/user/favorites/remove", { method: "POST", body: JSON.stringify({ item_type: itemType, item_ids: itemIds }) }),
    checkFavorites: (ids: string[]) =>
      request("/api/user/favorites/check", { method: "POST", body: JSON.stringify({ ids }) }),
    history: (page = 1, perPage = 30) => request(`/api/user/history/list?page=${page}&per_page=${perPage}`),
    addHistory: (trackId: string) => request("/api/user/history/add", { method: "POST", body: JSON.stringify({ track_id: trackId }) }),
    deleteHistoryItems: (ids: string[]) => request("/api/user/history/remove", { method: "POST", body: JSON.stringify({ ids }) }),
    playlists: () => request("/api/user/playlists"),
    getPlaylist: (id: string, all?: boolean) => request(`/api/user/playlists/${id}${all ? "?all=1" : ""}`),
    createPlaylist: (name: string) => request("/api/user/playlists", { method: "POST", body: JSON.stringify({ name }) }),
    deletePlaylist: (id: string) => request(`/api/user/playlists/${id}`, { method: "DELETE" }),
    addTracksToPlaylist: (plId: string, trackIds: string[]) =>
      request(`/api/user/playlists/${plId}/tracks/add`, { method: "POST", body: JSON.stringify({ track_ids: trackIds }) }),
    removeTracksFromPlaylist: (plId: string, trackIds: string[]) =>
      request(`/api/user/playlists/${plId}/tracks/remove`, { method: "POST", body: JSON.stringify({ track_ids: trackIds }) }),
    getSettings: () => request("/api/user/settings"),
    updateSettings: (s: any) => request("/api/user/settings", { method: "PUT", body: JSON.stringify(s) }),
    saveQueue: (q: { track_ids: string[], queue_idx: number, shuffle_order: number[], shuffle_idx: number, mode: string }) =>
      request("/api/user/queue", { method: "PUT", body: JSON.stringify(q) }),
    getQueue: () => request("/api/user/queue"),
  },
  jukebox: {
    list: () => request("/api/jukeboxes"),
    create: (d: { name: string; device_id?: string; device_config_id?: string }) =>
      request("/api/jukeboxes", { method: "POST", body: JSON.stringify(d) }),
    get: (id: string) => request(`/api/jukeboxes/${id}`),
    update: (id: string, d: { name?: string; device_id?: string }) =>
      request(`/api/jukeboxes/${id}`, { method: "PUT", body: JSON.stringify(d) }),
    delete: (id: string) => request(`/api/jukeboxes/${id}`, { method: "DELETE" }),
    status: (id: string) => request(`/api/jukeboxes/${id}/status`),
    play: (id: string, trackId: string) => request(`/api/jukeboxes/${id}/play/${trackId}`, { method: "POST" }),
    stop: (id: string) => request(`/api/jukeboxes/${id}/stop`, { method: "POST" }),
    next: (id: string) => request(`/api/jukeboxes/${id}/next`, { method: "POST" }),
    prev: (id: string) => request(`/api/jukeboxes/${id}/prev`, { method: "POST" }),
    volume: (id: string, v: number) => request(`/api/jukeboxes/${id}/volume`, { method: "PUT", body: JSON.stringify({ volume: v }) }),
    mode: (id: string, m: string) => request(`/api/jukeboxes/${id}/mode`, { method: "PUT", body: JSON.stringify({ mode: m }) }),
    queue: (id: string, ids?: string[]) =>
      ids
        ? request(`/api/jukeboxes/${id}/queue`, { method: "POST", body: JSON.stringify({ track_ids: ids }) })
        : request(`/api/jukeboxes/${id}/queue`),
    clearQueue: (id: string) => request(`/api/jukeboxes/${id}/queue`, { method: "DELETE" }),
    removeFromQueue: (id: string, idx: number) =>
      request(`/api/jukeboxes/${id}/queue/${idx}`, { method: "DELETE" }),
    shuffle: (id: string) => request(`/api/jukeboxes/${id}/shuffle`, { method: "POST" }),
    setQueue: (id: string, trackIds: string[]) =>
      request(`/api/jukeboxes/${id}/queue/set`, { method: "PUT", body: JSON.stringify({ track_ids: trackIds }) }),
    updateSettings: (id: string, s: { path_mapping: Record<string, string> }) =>
      request(`/api/jukeboxes/${id}/settings`, { method: "PUT", body: JSON.stringify(s) }),
    audioDevices: () => request("/api/audio/devices"),
    // device configs
    deviceConfigs: () => request("/api/audio/device/configs"),
    availableDeviceConfigs: () => request("/api/audio/device/configs/available"),
    createDeviceConfig: (d: { name: string; device_type: string; device_id: string; driver: string; config?: Record<string, string> }) =>
      request("/api/audio/device/configs", { method: "POST", body: JSON.stringify(d) }),
    updateDeviceConfig: (id: string, d: any) =>
      request(`/api/audio/device/configs/${id}`, { method: "PUT", body: JSON.stringify(d) }),
    deleteDeviceConfig: (id: string) =>
      request(`/api/audio/device/configs/${id}`, { method: "DELETE" }),
  },
}
