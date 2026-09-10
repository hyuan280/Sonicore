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

export interface TrackArtist {
  artist_id: string;
  name: string;
  role: string;
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
  kind: string;
  source: string;
  status: PluginStatus;
  status_msg?: string;
  updated_at?: string;
}

export interface PluginCatalogEntry {
  name: string;
  description?: string;
  author?: string;
  tags?: string[];
  repo: string;
  official?: boolean;
  version: string;
  downloads: number;
  updated_at?: string;
  installed?: boolean;
}

export interface PluginRepo {
  name: string;
  url: string;
  official: boolean;
}
