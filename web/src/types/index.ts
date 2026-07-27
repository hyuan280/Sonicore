export type Role = "super_admin" | "admin" | "user"

export interface User {
  id: string; username: string; email: string; role: Role; created_at: string
}

export interface AuthResponse {
  token: string; refresh_token: string; session_token: string
  user_id: string; username: string; role: Role
}

export interface Library {
  id: string; name: string; path: string; owner_id: string
  metadata_storage_mode: string; track_count: number; duration: number
  last_scanned_at: string | null; created_at: string
}

export interface Artist {
  id: string; name: string; album_count: number; cover_image_id?: string
}

export interface AlbumDetail {
  id: string; title: string; artist: string; artist_id: string
  year: number; genre: string; duration: number; cover_image_id?: string
}

export interface Album {
  id: string; name: string; title: string; artist: string; artistId: string
  year: number; genre: string; song_count: number; duration: number
  cover_image_id?: string
}

export interface Track {
  id: string; title: string; artist: string; artistId: string
  album: string; albumId: string; track: number; discNumber: number
  duration: number; bitRate: number; suffix: string; size: number
  cover_image_id?: string
}

export interface PlayerStatus {
  state: "stopped" | "playing"
  track: Track | null; duration: number; volume: number
  loop_mode: "none" | "all" | "one"
  queue: string[]; queue_idx: number
}

export interface ScanStatus {
  library_id: string; status: string; total_files: number; scanned: number
  new_tracks: number; updated_tracks: number; deleted_tracks: number; errors: number
}
