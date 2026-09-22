import type { ReactNode } from "react";

export type Role = "super_admin" | "admin" | "user";

export interface PluginsOutletContext {
  setToolbar: (node: ReactNode | null) => void;
}

export interface ScheduledTask {
  id: string;
  source: string;
  provider: string;
  name: string;
  status: "idle" | "running" | "disabled";
  enabled: boolean;
  interval_seconds: number;
  cron?: string;
  next_run: string | null;
  last_run?: string | null;
  last_error?: string;
}

export interface User {
  id: string;
  username: string;
  email: string;
  role: Role;
  avatar_format?: string;
  created_at: string;
}

export interface AuthResponse {
  token: string;
  refresh_token: string;
  session_token: string;
  user_id: string;
  username: string;
  role: Role;
}

export interface Library {
  id: string;
  name: string;
  path: string;
  owner_id: string;
  metadata_storage_mode: string;
  track_count: number;
  duration: number;
  last_scanned_at: string | null;
  last_scan_errors?: number;
  created_at: string;
}

export interface Artist {
  id: string;
  name: string;
  album_count: number;
  cover_image_id?: string;
  country?: string;
}

export interface AlbumDetail {
  id: string;
  title: string;
  artist: string;
  artist_id: string;
  year: number;
  genre: string;
  duration: number;
  cover_image_id?: string;
  country?: string;
}

export interface Album {
  id: string;
  name: string;
  title: string;
  artist: string;
  artistId: string;
  year: number;
  genre: string;
  song_count: number;
  duration: number;
  cover_image_id?: string;
  country?: string;
}

// TrackArtist describes an artist entry attached to a track. The list/detail
// APIs return a nested `artist` object while some endpoints flatten `name` to
// the top level, so both are optional here; consumers should read
// `name || artist?.name`.
export interface TrackArtist {
  artist_id: string;
  name?: string;
  role?: string;
  artist?: { name?: string };
}

export interface Track {
  id: string;
  title: string;
  artist: string;
  artists?: TrackArtist[];
  album: string;
  albumId: string;
  track: number;
  discNumber: number;
  duration: number;
  bitRate: number;
  suffix: string;
  size: number;
  cover_image_id?: string;
  heat?: number;
}

export interface PlayerStatus {
  state: "stopped" | "playing";
  track: Track | null;
  duration: number;
  volume: number;
  loop_mode: "none" | "all" | "one";
  queue: string[];
  queue_idx: number;
}

export interface ScanStatus {
  library_id: string;
  status: string;
  total_files: number;
  scanned: number;
  new_tracks: number;
  updated_tracks: number;
  deleted_tracks: number;
  errors: number;
}

export interface NotifTestOptions {
  smtp_host: string;
  smtp_port: string;
  username: string;
  password?: string;
  from_address: string;
  from_name: string;
  tls: boolean;
}

export type PluginStatus = "ok" | "disabled" | "error";

export interface PluginInstance {
  id: string;
  name: string;
  description?: string;
  version: string;
  source: string;
  author?: string;
  enabled?: boolean;
  status: PluginStatus;
  status_msg?: string;
  updated_at?: string;
  // Detected by the host after each plugin load (no per-plugin get_page
  // requests needed in the frontend).
  has_page?: boolean;
  // Version history (changelog) from the manifest [[plugin.history]].
  history?: PluginHistoryEntry[];
  // Set when the host detects a newer version (needs the marketplace;
  // always absent for now).
  update_available?: boolean;
  // Download count from the marketplace cache (absent for local/manual
  // plugins whose source is not a configured repo).
  downloads?: number;
}

export interface PluginHistoryEntry {
  version: string;
  date?: string;
  description: string;
}

export interface PluginCatalogEntry {
  name: string;
  description?: string;
  author?: string;
  tags?: string[];
  repo: string;
  official?: boolean;
  version: string;
  downloads?: number;
  updated_at?: string;
  installed?: boolean;
  update_available?: boolean;
  history?: PluginHistoryEntry[];
}

export interface PluginRepo {
  name: string;
  url: string;
  official: boolean;
  enabled?: boolean;
  added_at?: string;
  last_sync?: string;
  last_error?: string;
}

export interface PluginConfigResponse {
  config: Record<string, unknown>;
}

// UINode is one node of the plugin-provided UI assembly tree. The
// vocabulary is Vuetify's (component names from the Vuetify docs: VForm,
// VRow, VCol, VSwitch, VTextField, VTextarea, VSelect, VCheckbox, VAlert,
// VCard, VCardTitle, VCardText, VTable, VDivider, ...); form fields bind
// to the plugin's config keys via props.model. VSelect uses the official
// items format [{title, value}]; VTable is a simplified columns/rows table.
export interface UINode {
  component: string;
  props?: Record<string, unknown>;
  content?: UINode[];
}
