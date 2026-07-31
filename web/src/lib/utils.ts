import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${s.toString().padStart(2, "0")}`
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

export function coverUrl(type: "track" | "album" | "artist", id: string, size?: number): string {
  const session = localStorage.getItem("session_token") || ""
  let url = `/api/c/${session}/${type}/${id}`
  if (size) url += `?size=${size}`
  return url
}

interface TrackArtist {
  artist_id: string; name?: string; role: string; artist?: { name?: string }
}

export function performerNames(artists?: TrackArtist[]): string {
  if (!artists || artists.length === 0) return ""
  return artists.filter(a => a.role === "performer").map(a => a.name || a.artist?.name || "").filter(n => n).join("/")
}

export interface LRCLine {
  time: number
  text: string
}

export function parseLRC(lrc: string): LRCLine[] {
  const lines = lrc.split("\n")
  const result: LRCLine[] = []
  const regex = /\[(\d{2}):(\d{2})(?:[.:](\d{2,3}))?\](.*)/
  for (const line of lines) {
    const match = line.match(regex)
    if (match) {
      const minutes = parseInt(match[1], 10)
      const seconds = parseInt(match[2], 10)
      const frac = match[3] ? parseInt(match[3].padEnd(3, "0"), 10) : 0
      const time = minutes * 60 + seconds + frac / 1000
      const text = match[4].trim()
      if (text) {
        result.push({ time, text })
      }
    }
  }
  return result.sort((a, b) => a.time - b.time)
}

export function findCurrentLine(lines: LRCLine[], position: number): number {
  if (lines.length === 0) return -1
  for (let i = lines.length - 1; i >= 0; i--) {
    if (position >= lines[i].time) return i
  }
  return -1
}
