export interface LyricsSettings {
  opacity: number
  activeColor: string
  fontSize: number
}

export const DEFAULT_LYRICS_SETTINGS: LyricsSettings = { opacity: 0, activeColor: "#4ade80", fontSize: 28 }

export const LYRICS_SETTINGS_KEY = "lyrics_widget_settings"

export function loadLyricsSettings(): LyricsSettings {
  try {
    const raw = localStorage.getItem(LYRICS_SETTINGS_KEY)
    if (raw) return { ...DEFAULT_LYRICS_SETTINGS, ...JSON.parse(raw) }
  } catch {}
  return DEFAULT_LYRICS_SETTINGS
}

export function saveLyricsSettings(settings: LyricsSettings): void {
  try {
    localStorage.setItem(LYRICS_SETTINGS_KEY, JSON.stringify(settings))
  } catch {}
}

export const COLOR_PRESETS = [
  { name: "绿", value: "#4ade80" },
  { name: "白", value: "#ffffff" },
  { name: "黄", value: "#facc15" },
  { name: "青", value: "#22d3ee" },
  { name: "粉", value: "#f472b6" },
  { name: "橙", value: "#fb923c" },
]

export const DIM_COLOR = "#8a8a93"
