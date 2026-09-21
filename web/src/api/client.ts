import type { PluginConfigResponse } from "../types";

const BASE = "";

// fetchWithAuth performs a fetch with the stored bearer token and, on 401,
// attempts one token refresh before retrying. skipRefresh is used by the
// login/register endpoints where a 401 is a normal outcome.
async function fetchWithAuth(
  path: string,
  init: RequestInit = {},
  opts: { skipRefresh?: boolean } = {},
): Promise<Response> {
  const doFetch = () => {
    const headers: Record<string, string> = { ...(init.headers as Record<string, string>) };
    const token = localStorage.getItem("token");
    if (token) headers["Authorization"] = `Bearer ${token}`;
    return fetch(BASE + path, { ...init, headers });
  };
  let res = await doFetch();
  if (res.status === 401 && !opts.skipRefresh) {
    const ok = await tryRefresh();
    if (ok) res = await doFetch();
  }
  return res;
}

// handleSessionExpiry applies the shared policy for a failed token refresh:
// the session is considered dead — clear the session keys (only) and bounce
// to the login page.
function handleSessionExpiry() {
  localStorage.removeItem("token");
  localStorage.removeItem("refresh_token");
  localStorage.removeItem("session_token");
  localStorage.removeItem("role");
  window.location.href = "/login";
}

async function request(path: string, opts: RequestInit = {}): Promise<any> {
  const headers: Record<string, string> = {};
  if (!(opts.body instanceof FormData) && !(opts.body instanceof Blob)) {
    headers["Content-Type"] = "application/json";
  }

  const skipRefresh = path === "/api/auth/login" || path === "/api/auth/register";
  const res = await fetchWithAuth(path, { ...opts, headers }, { skipRefresh });

  if (res.status === 401 && !skipRefresh) {
    handleSessionExpiry();
    throw new Error("session expired");
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const err: any = new Error(body.error || statusText(res));
    err.status = res.status;
    err.error = body.error;
    err.code = body.code;
    throw err;
  }

  if (res.status === 204) return null;
  return res.json();
}

// statusText gives a readable label for HTTP failures. Browsers derive
// res.statusText from the status line, but dev proxies (Vite) often send an
// empty one, which previously surfaced as "Unknown" — so gateway errors get
// their standard names explicitly.
function statusText(res: Response): string {
  const gateway: Record<number, string> = {
    502: "Bad Gateway",
    503: "Service Unavailable",
    504: "Gateway Timeout",
  };
  return gateway[res.status] || res.statusText || `HTTP ${res.status}`;
}

// pluginLogsStream opens the SSE log stream of one plugin. onOpen fires
// when the stream is established (even with zero log lines), onLine for
// every log line (tail 50 first, then new lines), onGap when the resume
// cursor could not be located (some logs were skipped), onError when the
// server reports a stream error. cursor (the timestamp of the last record
// the client saw) resumes the stream right after that line. It uses
// fetchWithAuth because the browser's EventSource API cannot send the
// Authorization header; the caller cancels via the passed AbortSignal.
export async function pluginLogsStream(
  id: string,
  handlers: {
    onOpen?: () => void;
    onLine: (line: string) => void;
    onGap?: () => void;
    onError?: (message: string) => void;
  },
  signal: AbortSignal,
  cursor?: string,
): Promise<void> {
  const qs = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  const res = await fetchWithAuth(`/api/plugins/${encodeURIComponent(id)}/logs/stream${qs}`, {
    headers: { Accept: "text/event-stream" },
    signal,
  });
  if (!res.ok || !res.body) {
    throw new Error(`log stream failed: ${res.status}`);
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  let opened = false;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    if (!opened) {
      opened = true;
      handlers.onOpen?.();
    }
    let idx: number;
    while ((idx = buf.indexOf("\n\n")) !== -1) {
      const raw = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      let eventType = "";
      const dataLines: string[] = [];
      for (const l of raw.split("\n")) {
        if (l.startsWith("event:")) {
          eventType = l.slice(6).trim();
        } else if (l.startsWith("data:")) {
          // Multiple data: lines in one event are joined with \n per the
          // SSE spec — that's how multi-line log records arrive.
          dataLines.push(l.slice(5).replace(/^ /, ""));
        }
        // ": ping" keepalive comments are skipped.
      }
      if (eventType === "error") {
        handlers.onError?.(dataLines.join("\n"));
        continue;
      }
      if (eventType === "gap") {
        handlers.onGap?.();
        continue;
      }
      // Every event with at least one data: line is a log line (an empty
      // line value is preserved so blank lines can be skipped by the
      // assembler, not here).
      if (dataLines.length > 0) {
        handlers.onLine(dataLines.join("\n"));
      }
    }
  }
}

// refreshInFlight merges concurrent refresh attempts into one request. The
// backend rotates refresh tokens (validate, revoke the old one, issue a new
// one), so parallel refreshes would race each other: the losers would fail
// with the revoked token and wrongly trigger handleSessionExpiry.
let refreshInFlight: Promise<boolean> | null = null;

async function tryRefresh(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight;
  refreshInFlight = (async () => {
    const rt = localStorage.getItem("refresh_token");
    if (!rt) return false;
    try {
      const data = await fetch(BASE + "/api/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: rt }),
      }).then((r) => r.json());
      if (data.token) {
        localStorage.setItem("token", data.token);
        localStorage.setItem("refresh_token", data.refresh_token);
        if (data.session_token) localStorage.setItem("session_token", data.session_token);
        if (data.role) localStorage.setItem("role", data.role);
        return true;
      }
    } catch {}
    return false;
  })().finally(() => {
    refreshInFlight = null;
  });
  return refreshInFlight;
}

// requestBlob fetches a protected binary resource (e.g. avatar images).
// Returns null for any non-2xx status or network error so callers can fall
// back gracefully.
async function requestBlob(path: string): Promise<Blob | null> {
  try {
    const res = await fetchWithAuth(path, {});
    if (res.status === 401) {
      handleSessionExpiry();
      return null;
    }
    if (!res.ok) return null;
    return await res.blob();
  } catch {
    return null;
  }
}

export const api = {
  auth: {
    login: (d: any) => request("/api/auth/login", { method: "POST", body: JSON.stringify(d) }),
    register: (d: any) =>
      request("/api/auth/register", { method: "POST", body: JSON.stringify(d) }),
    logout: () => request("/api/auth/logout", { method: "POST" }),
    me: () => request("/api/user/me"),
    renewSession: (sessionToken?: string) =>
      request("/api/user/me", {
        method: "POST",
        body: JSON.stringify({ session_token: sessionToken || "" }),
      }),
    changePassword: (oldPw: string, newPw: string) =>
      request("/api/user/password", {
        method: "PUT",
        body: JSON.stringify({ old_password: oldPw, new_password: newPw }),
      }),
    registrationStatus: () => request("/api/auth/registration-status"),
  },
  admin: {
    users: () => request("/api/admin/users"),
    updateRole: (id: string, role: string) =>
      request(`/api/admin/users/${id}/role`, { method: "PUT", body: JSON.stringify({ role }) }),
    getUserAvatar: (id: string) => requestBlob(`/api/admin/users/${encodeURIComponent(id)}/avatar`),
    getSettings: () => request("/api/admin/settings"),
    updateSettings: (s: any) =>
      request("/api/admin/settings", { method: "PUT", body: JSON.stringify(s) }),
    dirs: (dir: string) => request(`/api/admin/dirs?path=${encodeURIComponent(dir)}`),
  },
  libraries: {
    list: () => request("/api/libraries"),
    get: (id: string) => request(`/api/libraries/${id}`),
    create: (d: any) => request("/api/libraries", { method: "POST", body: JSON.stringify(d) }),
    delete: (id: string) => request(`/api/libraries/${id}`, { method: "DELETE" }),
    scan: (id: string, mode?: string) =>
      request(`/api/libraries/${id}/scan${mode ? `?mode=${mode}` : ""}`, { method: "POST" }),
    scanStatus: (id: string) => request(`/api/libraries/${id}/scan/status`),
  },
  data: {
    tracks: (libId?: string, page = 1, perPage = 50) =>
      request(
        `/api/data/tracks?page=${page}&per_page=${perPage}${libId ? `&libId=${encodeURIComponent(libId)}` : ""}`,
      ),
    tracksQuery: (params: Record<string, string>) =>
      request(`/api/data/tracks?${new URLSearchParams(params)}`),
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
      request("/api/data/tracks/lyrics", {
        method: "POST",
        body: JSON.stringify({ trackid: trackId, offset }),
      }),
    search: (q: string) => request(`/api/data/search?q=${encodeURIComponent(q)}`),
  },
  user: {
    favorites: (type?: string, page = 1, perPage = 30) =>
      request(`/api/user/favorites/list?type=${type || "track"}&page=${page}&per_page=${perPage}`),
    addFavorites: (itemType: string, itemIds: string[]) =>
      request("/api/user/favorites/add", {
        method: "POST",
        body: JSON.stringify({ item_type: itemType, item_ids: itemIds }),
      }),
    removeFavorites: (itemType: string, itemIds: string[]) =>
      request("/api/user/favorites/remove", {
        method: "POST",
        body: JSON.stringify({ item_type: itemType, item_ids: itemIds }),
      }),
    checkFavorites: (ids: string[]) =>
      request("/api/user/favorites/check", { method: "POST", body: JSON.stringify({ ids }) }),
    history: (page = 1, perPage = 30) =>
      request(`/api/user/history/list?page=${page}&per_page=${perPage}`),
    addHistory: (trackId: string) =>
      request("/api/user/history/add", {
        method: "POST",
        body: JSON.stringify({ track_id: trackId }),
      }),
    deleteHistoryItems: (ids: string[]) =>
      request("/api/user/history/remove", { method: "POST", body: JSON.stringify({ ids }) }),
    playlists: () => request("/api/user/playlists"),
    getPlaylist: (id: string, all?: boolean) =>
      request(`/api/user/playlists/${id}${all ? "?all=1" : ""}`),
    createPlaylist: (name: string) =>
      request("/api/user/playlists", { method: "POST", body: JSON.stringify({ name }) }),
    deletePlaylist: (id: string) => request(`/api/user/playlists/${id}`, { method: "DELETE" }),
    addTracksToPlaylist: (plId: string, trackIds: string[]) =>
      request(`/api/user/playlists/${plId}/tracks/add`, {
        method: "POST",
        body: JSON.stringify({ track_ids: trackIds }),
      }),
    removeTracksFromPlaylist: (plId: string, trackIds: string[]) =>
      request(`/api/user/playlists/${plId}/tracks/remove`, {
        method: "POST",
        body: JSON.stringify({ track_ids: trackIds }),
      }),
    getSettings: () => request("/api/user/settings"),
    updateSettings: (s: any) =>
      request("/api/user/settings", { method: "PUT", body: JSON.stringify(s) }),
    saveQueue: (q: {
      track_ids: string[];
      queue_idx: number;
      shuffle_order: number[];
      shuffle_idx: number;
      mode: string;
    }) => request("/api/user/queue", { method: "PUT", body: JSON.stringify(q) }),
    getQueue: () => request("/api/user/queue"),
    getAvatar: () => requestBlob("/api/user/avatar"),
    updateAvatar: (file: Blob) => request("/api/user/avatar", { method: "PUT", body: file }),
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
    play: (id: string, trackId: string) =>
      request(`/api/jukeboxes/${id}/play/${trackId}`, { method: "POST" }),
    stop: (id: string) => request(`/api/jukeboxes/${id}/stop`, { method: "POST" }),
    next: (id: string) => request(`/api/jukeboxes/${id}/next`, { method: "POST" }),
    prev: (id: string) => request(`/api/jukeboxes/${id}/prev`, { method: "POST" }),
    volume: (id: string, v: number) =>
      request(`/api/jukeboxes/${id}/volume`, {
        method: "PUT",
        body: JSON.stringify({ volume: v }),
      }),
    mode: (id: string, m: string) =>
      request(`/api/jukeboxes/${id}/mode`, { method: "PUT", body: JSON.stringify({ mode: m }) }),
    queue: (id: string, ids?: string[]) =>
      ids
        ? request(`/api/jukeboxes/${id}/queue`, {
            method: "POST",
            body: JSON.stringify({ track_ids: ids }),
          })
        : request(`/api/jukeboxes/${id}/queue`),
    clearQueue: (id: string) => request(`/api/jukeboxes/${id}/queue`, { method: "DELETE" }),
    removeFromQueue: (id: string, idx: number) =>
      request(`/api/jukeboxes/${id}/queue/${idx}`, { method: "DELETE" }),
    shuffle: (id: string) => request(`/api/jukeboxes/${id}/shuffle`, { method: "POST" }),
    setQueue: (id: string, trackIds: string[]) =>
      request(`/api/jukeboxes/${id}/queue/set`, {
        method: "PUT",
        body: JSON.stringify({ track_ids: trackIds }),
      }),
    updateSettings: (id: string, s: { path_mapping: Record<string, string> }) =>
      request(`/api/jukeboxes/${id}/settings`, { method: "PUT", body: JSON.stringify(s) }),
    audioDevices: () => request("/api/audio/devices"),
    // device configs
    deviceConfigs: () => request("/api/audio/device/configs"),
    availableDeviceConfigs: () => request("/api/audio/device/configs/available"),
    createDeviceConfig: (d: {
      name: string;
      device_type: string;
      device_id: string;
      driver: string;
      config?: Record<string, string>;
    }) => request("/api/audio/device/configs", { method: "POST", body: JSON.stringify(d) }),
    updateDeviceConfig: (id: string, d: any) =>
      request(`/api/audio/device/configs/${id}`, { method: "PUT", body: JSON.stringify(d) }),
    deleteDeviceConfig: (id: string) =>
      request(`/api/audio/device/configs/${id}`, { method: "DELETE" }),
  },
  notifications: {
    channels: () => request("/api/notifications/channels"),
    setChannelEnabled: (type: string, enabled: boolean) =>
      request(`/api/notifications/channels/${encodeURIComponent(type)}/enabled`, {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      }),
    test: (channel: string, to: string[], config?: Record<string, unknown>) =>
      request("/api/notifications/test", {
        method: "POST",
        body: JSON.stringify({ channel, to, config }),
      }),
    getPreferences: () => request("/api/notifications/preferences"),
    updatePreferences: (prefs: Record<string, { roles?: string[]; channels?: string[] }>) =>
      request("/api/notifications/preferences", {
        method: "PUT",
        body: JSON.stringify({ preferences: prefs }),
      }),
    getUserPrefs: () => request("/api/notifications/user-prefs"),
    updateUserPref: (prefs: { category: string; enabled: boolean }[]) =>
      request("/api/notifications/user-prefs", {
        method: "PUT",
        body: JSON.stringify({ prefs }),
      }),
  },
  metadata: {
    sources: () => request("/api/metadata/sources"),
    searchTrack: (body: Record<string, unknown>) =>
      request("/api/metadata/search/track", { method: "POST", body: JSON.stringify(body) }),
    searchAlbum: (body: Record<string, unknown>) =>
      request("/api/metadata/search/album", { method: "POST", body: JSON.stringify(body) }),
    reidentify: (body: Record<string, unknown>) =>
      request("/api/metadata/reidentify", { method: "POST", body: JSON.stringify(body) }),
    save: (body: Record<string, unknown>) =>
      request("/api/metadata/save", { method: "POST", body: JSON.stringify(body) }),
  },
  platform: {
    list: () => request("/api/plat/list"),
    charts: (name: string) => request(`/api/plat/${encodeURIComponent(name)}/charts`),
    chart: (name: string, id: string, page = 1, limit = 30) =>
      request(
        `/api/plat/${encodeURIComponent(name)}/charts/${encodeURIComponent(id)}?page=${page}&limit=${limit}`,
      ),
    search: (name: string, q: string, type = "track", page = 1, limit = 30) =>
      request(
        `/api/plat/${encodeURIComponent(name)}/search?q=${encodeURIComponent(q)}&type=${encodeURIComponent(type)}&page=${page}&limit=${limit}`,
      ),
    track: (name: string, id: string) =>
      request(`/api/plat/${encodeURIComponent(name)}/tracks/${encodeURIComponent(id)}`),
    artist: (name: string, id: string) =>
      request(`/api/plat/${encodeURIComponent(name)}/artists/${encodeURIComponent(id)}`),
    artistTracks: (name: string, id: string, page = 1, limit = 30) =>
      request(
        `/api/plat/${encodeURIComponent(name)}/artists/${encodeURIComponent(id)}/tracks?page=${page}&limit=${limit}`,
      ),
  },
  plugins: {
    installed: () => request("/api/plugins"),
    uninstalled: () => request("/api/plugins/uninstalled"),
    installPlugin: (id: string) =>
      request(`/api/plugins/${encodeURIComponent(id)}/install`, { method: "POST" }),
    uninstall: (id: string) =>
      request(`/api/plugins/${encodeURIComponent(id)}/uninstall`, { method: "POST" }),
    remove: (id: string) => request(`/api/plugins/${encodeURIComponent(id)}`, { method: "DELETE" }),
    setEnabled: (id: string, enabled: boolean) =>
      request(`/api/plugins/${encodeURIComponent(id)}/enabled`, {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      }),
    updateConfig: (id: string, config: Record<string, unknown>) =>
      request(`/api/plugins/${encodeURIComponent(id)}/config`, {
        method: "PUT",
        body: JSON.stringify({ config }),
      }),
    getConfig: (id: string) =>
      request(`/api/plugins/${encodeURIComponent(id)}/config`) as Promise<PluginConfigResponse>,
    getForm: (id: string) => request(`/api/plugins/${encodeURIComponent(id)}/form`),
    getPage: (id: string) => request(`/api/plugins/${encodeURIComponent(id)}/page`),
    update: (id: string) =>
      request(`/api/plugins/${encodeURIComponent(id)}/update`, { method: "POST" }),
    market: () => request("/api/plugins/market"),
    // Reserved for the plugin detail page (not wired yet).
    marketDetail: (name: string) => request(`/api/plugins/market/${encodeURIComponent(name)}`),
    install: (name: string, repo: string) =>
      request("/api/plugins/install", {
        method: "POST",
        body: JSON.stringify({ name, repo }),
      }),
    repos: () => request("/api/plugins/repos"),
    addRepo: (url: string) =>
      request("/api/plugins/repos", { method: "POST", body: JSON.stringify({ url }) }),
    removeRepo: (url: string) =>
      request(`/api/plugins/repos?url=${encodeURIComponent(url)}`, { method: "DELETE" }),
  },
  tasks: {
    list: () => request("/api/tasks"),
    get: (id: string) => request(`/api/tasks/${encodeURIComponent(id)}`),
    run: (id: string) => request(`/api/tasks/${encodeURIComponent(id)}/run`, { method: "POST" }),
    setEnabled: (id: string, enabled: boolean) =>
      request(`/api/tasks/${encodeURIComponent(id)}/enabled`, {
        method: "PUT",
        body: JSON.stringify({ enabled }),
      }),
  },
};
