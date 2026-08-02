import { useEffect, useMemo, useState } from "react"
import { createRoot, type Root } from "react-dom/client"
import { X, Settings } from "lucide-react"
import { usePlayer } from "../stores/player"
import { parseLRC, findCurrentLine } from "./utils"
import {
  type LyricsSettings,
  loadLyricsSettings,
  saveLyricsSettings,
  COLOR_PRESETS,
  DIM_COLOR,
} from "./lyricsSettings"

let pipWindow: Window | null = null
let pipRoot: Root | null = null
const listeners = new Set<(open: boolean) => void>()

function notify(open: boolean) {
  listeners.forEach((cb) => cb(open))
}

function copyStyles(pip: Window) {
  document.querySelectorAll('style, link[rel="stylesheet"]').forEach((s) => {
    try {
      pip.document.head.appendChild(s.cloneNode(true))
    } catch {}
  })
}

export function isDesktopLyricsSupported(): boolean {
  return typeof window !== "undefined" && "documentPictureInPicture" in window
}

export function isDesktopLyricsOpen(): boolean {
  return pipWindow !== null && !pipWindow.closed
}

export function subscribeDesktopLyrics(cb: (open: boolean) => void): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

function cleanup() {
  if (pipRoot) {
    try {
      pipRoot.unmount()
    } catch {}
    pipRoot = null
  }
  pipWindow = null
  notify(false)
}

export async function openDesktopLyrics(): Promise<boolean> {
  const anyWindow = window as any
  if (!anyWindow.documentPictureInPicture) return false
  if (isDesktopLyricsOpen()) {
    pipWindow?.focus()
    return true
  }

  try {
    const pip = (await anyWindow.documentPictureInPicture.requestWindow({ width: 460, height: 240 })) as Window
    pipWindow = pip
    copyStyles(pip)

    const rootEl = pip.document.createElement("div")
    rootEl.id = "desktop-lyrics-root"
    pip.document.body.appendChild(rootEl)
    pipRoot = createRoot(rootEl)
    pipRoot.render(<DesktopLyrics />)

    pip.addEventListener("pagehide", cleanup)
    notify(true)
    return true
  } catch {
    cleanup()
    return false
  }
}

export function closeDesktopLyrics(): void {
  if (pipWindow && !pipWindow.closed) {
    pipWindow.close()
  }
  cleanup()
}

function DesktopLyrics() {
  const { track, lyrics, lyricsFormat, position } = usePlayer()
  const [settings, setSettings] = useState<LyricsSettings>(loadLyricsSettings)
  const [showSettings, setShowSettings] = useState(false)

  const updateSettings = (patch: Partial<LyricsSettings>) => {
    setSettings((s) => {
      const next = { ...s, ...patch }
      saveLyricsSettings(next)
      return next
    })
  }

  const lines = useMemo(() => {
    if (lyricsFormat === "lrc") return parseLRC(lyrics)
    return []
  }, [lyrics, lyricsFormat])

  const currentIdx = useMemo(() => {
    if (lines.length === 0) return -1
    return findCurrentLine(lines, position)
  }, [lines, position])

  const current = currentIdx >= 0 ? lines[currentIdx] : null
  const next = currentIdx >= 0 && currentIdx + 1 < lines.length ? lines[currentIdx + 1] : null
  const currentOnLeft = currentIdx < 0 || currentIdx % 2 === 0
  const leftLine = currentOnLeft ? current : next
  const rightLine = currentOnLeft ? next : current

  const bgColor = `rgba(24, 24, 27, ${settings.opacity / 100})`

  return (
    <div
      className="group fixed inset-0 flex flex-col overflow-hidden select-none"
      style={{ backgroundColor: bgColor }}
    >
      {/* hover toolbar */}
      <div className="absolute top-1 right-2 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity duration-200 z-20">
        <button onClick={() => updateSettings({ fontSize: Math.max(16, settings.fontSize - 4) })}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer" title="Smaller">
          <span className="text-sm font-bold">A-</span>
        </button>
        <button onClick={() => updateSettings({ fontSize: Math.min(60, settings.fontSize + 4) })}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer" title="Larger">
          <span className="text-base font-bold">A+</span>
        </button>
        <button onClick={() => setShowSettings(!showSettings)}
          className={`p-1 rounded cursor-pointer ${showSettings ? "text-green-400" : "text-zinc-400 hover:text-white hover:bg-zinc-800/80"}`}
          title="Settings">
          <Settings className="w-4 h-4" />
        </button>
        <button onClick={closeDesktopLyrics}
          className="p-1 rounded text-zinc-400 hover:text-white hover:bg-zinc-800/80 cursor-pointer" title="Close">
          <X className="w-4 h-4" />
        </button>
      </div>

      {showSettings && (
        <div className="absolute right-2 top-9 w-56 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl p-4 space-y-3 z-30">
          <div>
            <div className="text-xs text-zinc-400 mb-1.5">背景透明度：{settings.opacity}%</div>
            <input type="range" min="0" max="100" value={settings.opacity}
              onChange={(e) => updateSettings({ opacity: parseInt(e.target.value, 10) })}
              className="w-full accent-green-500 cursor-pointer" />
          </div>
          <div>
            <div className="text-xs text-zinc-400 mb-1.5">歌词颜色</div>
            <div className="flex gap-2 flex-wrap">
              {COLOR_PRESETS.map((c) => (
                <button key={c.value}
                  onClick={() => updateSettings({ activeColor: c.value })}
                  className={`w-7 h-7 rounded-full cursor-pointer border transition-transform hover:scale-110 ${
                    settings.activeColor === c.value
                      ? "border-white ring-2 ring-white/30"
                      : "border-transparent"
                  }`}
                  style={{ backgroundColor: c.value }}
                  title={c.name} />
              ))}
            </div>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-4 pt-8 pb-3">
        {lyricsFormat === "lrc" && lines.length > 0 ? (
          <div className="flex flex-col gap-1">
            <div
              className="leading-tight break-words"
              style={{
                fontSize: settings.fontSize,
                textAlign: "left",
                color: currentOnLeft ? settings.activeColor : DIM_COLOR,
                fontWeight: currentOnLeft ? 600 : 400,
              }}
            >
              {leftLine ? leftLine.text : "\u00A0"}
            </div>
            <div
              className="leading-tight break-words"
              style={{
                fontSize: settings.fontSize,
                textAlign: "right",
                color: currentOnLeft ? DIM_COLOR : settings.activeColor,
                fontWeight: currentOnLeft ? 400 : 600,
              }}
            >
              {rightLine ? rightLine.text : "\u00A0"}
            </div>
          </div>
        ) : lyrics ? (
          <div
            className="leading-relaxed break-words"
            style={{ fontSize: settings.fontSize, color: settings.activeColor }}
          >
            {lyrics}
          </div>
        ) : (
          <div className="text-zinc-600 italic text-center pt-5" style={{ fontSize: Math.round(settings.fontSize * 0.6) }}>
            {track ? "No lyrics" : "Not playing"}
          </div>
        )}
      </div>
    </div>
  )
}
