import { useRef, useEffect, useState, useCallback } from "react"
import { usePlayer, savePlayerState } from "../stores/player"
import { useAuth } from "../stores/auth"
import { api } from "../api/client"
import { Play, Pause, SkipBack, SkipForward, Volume2, VolumeX, ListMusic, Repeat, Shuffle, Trash2, Repeat1, Heart, Music, FileText, PictureInPicture2, FileMusic, Music2, Music3 } from "lucide-react"
import { Link } from "react-router-dom"
import { formatDuration, coverUrl, performerNames } from "../lib/utils"
import ArtistLink from "../components/ArtistLink"
import LyricsPanel from "../components/LyricsPanel"
import { isDesktopLyricsSupported, isDesktopLyricsOpen, openDesktopLyrics, closeDesktopLyrics, subscribeDesktopLyrics } from "../lib/desktopLyrics"
import { setMediaSessionMetadata, setMediaSessionPlaybackState, setMediaSessionPositionState, bindMediaSessionActions, clearMediaSessionActions } from "../lib/mediaSession"
import { useMseAudio } from "../hooks/useMseAudio"

const qualityOptions = [
  { key: "standard", label: "SQ", title: "AAC 256k", desc: "Standard" },
  { key: "high", label: "HQ", title: "AAC 320k", desc: "High" },
  { key: "lossless", label: "LOS", title: "FLAC", desc: "Lossless" },
] as const

function loadQuality(): string {
  try { return localStorage.getItem("playback_quality") || "standard" } catch { return "standard" }
}

function saveQuality(q: string) {
  try { localStorage.setItem("playback_quality", q) } catch {}
}

export default function PlayerBar() {
  const ps = usePlayer()
  const { logout } = useAuth()
  const mse = useMseAudio()
  const audioRef = mse.audioRef
  const progressRef = useRef<HTMLDivElement>(null)
  const currentTrackRef = useRef<string | null>(null)
  const prevVolume = useRef(0.8)
  if (prevVolume.current === 0.8) {
    try { const v = localStorage.getItem("prevVolume"); if (v) prevVolume.current = parseFloat(v) } catch {}
  }
  const [showQueue, setShowQueue] = useState(false)
  const [showLyrics, setShowLyrics] = useState(false)
  const [desktopLyricsOpen, setDesktopLyricsOpen] = useState(isDesktopLyricsOpen)
  const [showQuality, setShowQuality] = useState(false)
  const [qualityPos, setQualityPos] = useState<{ left: number }>({ left: 0 })
  const [showVersions, setShowVersions] = useState(false)
  const [fav, setFav] = useState(false)
  const [quality, setQuality] = useState(loadQuality)
  const lastHistoryRef = useRef(0)
  const switchingRef = useRef(false)
  const session = localStorage.getItem("session_token")

  useEffect(() => {
    if (ps.track) {
      api.user.checkFavorites([ps.track.id]).then(d => setFav(!!d.favorites?.[ps.track!.id])).catch(() => {})
    }
  }, [ps.track?.id])

  useEffect(() => {
    return subscribeDesktopLyrics(setDesktopLyricsOpen)
  }, [])

  const toggleDesktopLyrics = () => {
    if (isDesktopLyricsOpen()) {
      closeDesktopLyrics()
    } else {
      openDesktopLyrics()
    }
  }

  useEffect(() => {
    if (ps.queue.length === 0 && ps.track) {
      usePlayer.setState({ track: null, playing: false, position: 0 })
      savePlayerState()
      api.user.saveQueue({ track_ids: [], queue_idx: 0, shuffle_order: [], shuffle_idx: 0, mode: "normal" }).catch(() => {})
    }
  }, [ps.queue.length])

  const toggleFav = async () => {
    if (!ps.track) return
    if (fav) {
      await api.user.removeFavorites("track", [ps.track.id]).catch(() => {})
      setFav(false)
    } else {
      await api.user.addFavorites("track", [ps.track.id]).catch(() => {})
      setFav(true)
    }
  }

  useEffect(() => {
    if (!session && ps.track) {
      const tok = localStorage.getItem("token")
      if (tok) {
        const msg = "Server updated: please sign in again to continue playing."
        if (confirm(msg)) { logout(); window.location.href = "/login" }
      }
    }
  }, [ps.track?.id])

  useEffect(() => {
    const el = audioRef.current
    if (!el || !ps.track) return

    const trackId = ps.track.id
    currentTrackRef.current = trackId
    lastHistoryRef.current = 0
    el.volume = ps.volume

    const onEnded = () => {
      if (currentTrackRef.current !== trackId) return
      switchingRef.current = false
      usePlayer.getState().advanceTrack()
    }

    const onTimeUpdate = () => {
      const s = usePlayer.getState()
      if (currentTrackRef.current !== s.track?.id) return

      s.setPosition(el.currentTime)
      if (el.currentTime - lastHistoryRef.current >= 15 && s.track) {
        lastHistoryRef.current = el.currentTime
        api.user.addHistory(s.track.id).catch(() => {})
      }
      if (lastHistoryRef.current === 0 && el.currentTime >= 3) {
        lastHistoryRef.current = el.currentTime
        api.user.addHistory(s.track.id).catch(() => {})
      }

      if (el.ended && s.playing) {
        s.advanceTrack()
      }
    }

    const onPlay = () => {
      const s = usePlayer.getState()
      switchingRef.current = false
      s.setPlaying(true)
      savePlayerState()
      lastHistoryRef.current = 0
      if (s.position > 1 && el.currentTime < 1) {
        mse.seek(s.position)
      }
    }

    const onPause = () => {
      if (switchingRef.current) return
      const s = usePlayer.getState()

      if (s.playing && s.mode !== "normal" && currentTrackRef.current === s.track?.id) {
        s.advanceTrack()
        return
      }

      if (currentTrackRef.current === s.track?.id) {
        s.setPlaying(false)
        savePlayerState()
      }
    }

    const onError = () => {
      console.warn("[audio] stream error | code:", el.error?.code, "| message:", el.error?.message)
      switchingRef.current = false
      usePlayer.getState().setPlaying(false)
    }

    switchingRef.current = true
    const swTimer = setTimeout(() => { switchingRef.current = false }, 5000)
    mse.start({ id: trackId, duration: ps.track.duration || 0 }, quality, ps.position, ps.playing)

    el.addEventListener("ended", onEnded)
    el.addEventListener("timeupdate", onTimeUpdate)
    el.addEventListener("play", onPlay)
    el.addEventListener("pause", onPause)
    el.addEventListener("error", onError)

    return () => {
      clearTimeout(swTimer)
      el.removeEventListener("ended", onEnded)
      el.removeEventListener("timeupdate", onTimeUpdate)
      el.removeEventListener("play", onPlay)
      el.removeEventListener("pause", onPause)
      el.removeEventListener("error", onError)
    }
  }, [ps.track?.id, ps.playEpoch, quality, mse.start, mse.seek])

  useEffect(() => {
    if (audioRef.current) audioRef.current.volume = ps.volume
  }, [ps.volume])

  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    if (!ps.track) { el.pause(); return }
    if (ps.playing) {
      if (el.ended) { try { el.currentTime = 0 } catch {} }
      el.play().catch(() => usePlayer.getState().setPlaying(false))
    } else {
      el.pause()
    }
  }, [ps.track?.id, ps.playing])

  // Media Session API (OS media controls / system flyout)
  useEffect(() => {
    if (!("mediaSession" in navigator)) return
    bindMediaSessionActions({
      play: () => audioRef.current?.play(),
      pause: () => audioRef.current?.pause(),
      next: () => usePlayer.getState().next(),
      prev: () => usePlayer.getState().prev(),
      seekTo: (time) => mse.seek(time),
    })
    return () => {
      setMediaSessionMetadata(null)
      clearMediaSessionActions()
    }
  }, [])

  useEffect(() => {
    setMediaSessionMetadata(ps.track)
  }, [ps.track])

  useEffect(() => {
    setMediaSessionPlaybackState(ps.playing ? "playing" : "paused")
  }, [ps.playing])

  useEffect(() => {
    if (ps.track) {
      setMediaSessionPositionState(ps.position, ps.track.duration)
    }
  }, [ps.position, ps.track])

  const seek = useCallback((e: React.MouseEvent) => {
    const bar = progressRef.current
    if (!bar || !ps.track) return
    const rect = bar.getBoundingClientRect()
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    const target = Math.min(pct * ps.track.duration, Math.max(0, ps.track.duration - 0.5))
    usePlayer.getState().setPosition(target)
    mse.seek(target)
  }, [ps.track, mse.seek])

  const qualityRef = useRef<HTMLDivElement>(null)
  const qualityPanelRef = useRef<HTMLDivElement>(null)
  const versionRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!showQuality) return
    const handle = (e: MouseEvent) => {
      const inBtn = qualityRef.current && qualityRef.current.contains(e.target as Node)
      const inPanel = qualityPanelRef.current && qualityPanelRef.current.contains(e.target as Node)
      if (!inBtn && !inPanel) {
        setShowQuality(false)
      }
    }
    document.addEventListener("mousedown", handle)
    return () => document.removeEventListener("mousedown", handle)
  }, [showQuality])

  useEffect(() => {
    if (!showVersions) return
    const handle = (e: MouseEvent) => {
      if (versionRef.current && !versionRef.current.contains(e.target as Node)) {
        setShowVersions(false)
      }
    }
    document.addEventListener("mousedown", handle)
    return () => document.removeEventListener("mousedown", handle)
  }, [showVersions])

  const currentQ = qualityOptions.find(o => o.key === quality) ?? qualityOptions[0]

  const qIcons: Record<string, React.ReactNode> = {
    standard: <Music3 className="w-4 h-4" />,
    high: <Music2 className="w-4 h-4" />,
    lossless: <Music className="w-4 h-4" />,
  }

  const track = ps.track
  const verCount = track?.versions?.length ?? 0
  const hasVersions = verCount > 0

  const switchVersion = (v: { id: string; version: number; duration: number; suffix: string; version_label: string }) => {
    if (!track) return
    const oldEntry = { id: track.id, version: track.version || 0, version_label: track.version_label || "", suffix: track.suffix, duration: track.duration, library_id: "", bit_rate: 0 }
    const newVersions = track.versions
      ? [...track.versions.filter(x => x.id !== v.id), oldEntry]
      : []
    const newQueue = [...ps.queue]
    newQueue[ps.queueIdx] = {
      ...track,
      id: v.id, duration: v.duration, suffix: v.suffix,
      version: v.version, version_label: v.version_label,
      versions: newVersions,
    }
    ps.setQueue(newQueue, ps.queueIdx)
    setShowVersions(false)
  }

  return (
    <>
      <audio ref={audioRef} preload="auto" />
      <div className="fixed bottom-0 left-0 right-0 bg-zinc-900 border-t border-zinc-800 px-4 py-2 z-50 h-16">
        <div className="absolute top-0 left-0 right-0 h-2" />
        <div ref={progressRef} className="absolute top-2 left-0 right-0 h-1 bg-zinc-800 cursor-pointer group overflow-hidden z-10"
          onClick={seek}>
          {track && ps.playing && mse.waiting && (
            <div className="absolute inset-0 progress-sweep" />
          )}
          {track && mse.buffered.map((r, i) => (
            <div key={i} className="absolute inset-y-0 bg-green-600/40"
              style={{
                left: `${(r.start / (track.duration || 1)) * 100}%`,
                width: `${((r.end - r.start) / (track.duration || 1)) * 100}%`,
              }} />
          ))}
          {track && (
            <div className="absolute inset-y-0 bg-green-500 transition-all"
              style={{ width: `${(ps.position / (track.duration || 1)) * 100}%` }} />
        )}
      </div>

        <div className="flex items-center gap-4 max-w-screen-2xl mx-auto mt-2 relative">
          <div className="flex items-center gap-3 w-72">
            {track ? (
              <>
                <div className="w-10 h-10 rounded bg-zinc-800 flex-shrink-0 flex items-center justify-center text-xs text-zinc-500 overflow-hidden">
                  {track.cover_image_id ? (
                    <img src={coverUrl("track", track.id, 64)} alt={track.title}
                      className="w-full h-full object-cover"
                      onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                  ) : null}
                  <Music className={`w-5 h-5 text-zinc-600 ${track.cover_image_id ? "hidden" : ""}`} />
                </div>
                <div className="truncate min-w-0">
                  <p className="text-sm font-medium truncate">{track.title}</p>
                  <p className="text-xs text-zinc-400 truncate">
                    <ArtistLink artists={track.artists} />
                    {track.albums && track.albums.length > 0 && (
                      <span className="truncate"> — {track.albums.map((a, i) => (
                        <span key={a.id || i}>
                          {i > 0 && <span className="mx-0.5 text-zinc-600">/</span>}
                          {a.id ? <Link to={`/albums/${a.id}`} className="hover:text-white transition-colors" onClick={e => e.stopPropagation()}>{a.title || ""}</Link> : <span>{a.title || ""}</span>}
                        </span>
                      ))}</span>
                    )}
                  </p>
                </div>
              </>
            ) : (
              <>
                <div className="w-10 h-10 rounded bg-zinc-800 flex-shrink-0 flex items-center justify-center">
                  <Music className="w-5 h-5 text-zinc-600" />
                </div>
                <div className="text-sm text-zinc-500">No track playing</div>
              </>
            )}
          </div>

           <div className="flex-1 flex items-center justify-center relative">
            <div className="absolute left-1/2 -translate-x-1/2 flex items-center gap-2">
              <div ref={qualityRef}>
                <button onClick={() => {
                  if (qualityRef.current) {
                    const r = qualityRef.current.getBoundingClientRect()
                    setQualityPos({ left: r.left + r.width / 2 })
                  }
                  setShowQuality(!showQuality)
                }}
                  className="text-[11px] font-semibold px-1.5 py-0.5 rounded text-zinc-400 hover:text-white cursor-pointer flex items-center gap-1 w-14 justify-center"
                  title={currentQ.desc}>
                  {qIcons[quality]}{currentQ.label}
                </button>
              </div>
              <button onClick={ps.prev} className="p-1 text-zinc-400 hover:text-white cursor-pointer">
                <SkipBack className="w-5 h-5" />
              </button>
              <button onClick={ps.togglePlay}
                className="w-8 h-8 rounded-full bg-white text-black flex items-center justify-center hover:scale-105 transition cursor-pointer">
                {ps.playing ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4 ml-0.5" />}
              </button>
              <button onClick={ps.next} className="p-1 text-zinc-400 hover:text-white cursor-pointer">
                <SkipForward className="w-5 h-5" />
              </button>
              <span className="w-9 flex items-center justify-center">
                {ps.track && (
                  <button onClick={toggleFav}
                    className={`p-1 cursor-pointer text-zinc-500 hover:text-red-400 ${fav ? "text-red-500" : ""}`}
                    title={fav ? "Remove from favorites" : "Add to favorites"}>
                    <Heart className={`w-5 h-5 ${fav ? "fill-red-500" : ""}`} />
                  </button>
                )}
              </span>
              <button onClick={ps.cycleMode}
                className="p-1 cursor-pointer relative" title={
                  ps.mode === "normal" ? "Normal" : ps.mode === "all" ? "Repeat all" : ps.mode === "one" ? "Repeat one" : "Shuffle"
                }>
                {ps.mode === "shuffle" ? (
                  <Shuffle className={`w-5 h-5 ${ps.mode === "shuffle" ? "text-green-500" : "text-zinc-500"}`} />
                ) : ps.mode === "one" ? (
                  <Repeat1 className={`w-5 h-5 text-green-500`} />
                ) : (
                  <Repeat className={`w-5 h-5 ${ps.mode === "normal" ? "text-zinc-500" : "text-green-500"}`} />
                )}
              </button>
              <span className="w-11 flex items-center justify-center relative" ref={versionRef}>
                {track && hasVersions && (
                  <>
                    <button onClick={() => setShowVersions(!showVersions)}
                      className="p-1 text-zinc-400 hover:text-white cursor-pointer flex items-center gap-0.5 whitespace-nowrap"
                      title={track.version_label || "Versions"}>
                      <FileMusic className="w-4 h-4" />V{track.version || 1}
                    </button>
                    {showVersions && (
                      <div className="absolute bottom-full right-0 mb-1 w-64 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl z-[60] py-1 max-h-72 overflow-y-auto">
                        <p className="text-xs text-zinc-500 px-3 py-1.5">Select version</p>
                        <div className="border-t border-zinc-700 pt-1">
                          <div className="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 text-green-500">
                            <FileMusic className="w-3.5 h-3.5 flex-shrink-0" />
                            <span className="truncate">{track.version_label || track.suffix?.toUpperCase() + " · current"}</span>
                            <span className="text-xs text-green-500 ml-auto shrink-0">current</span>
                          </div>
                          {track.versions!.filter(v => v.id !== track.id).map(v => (
                            <button key={v.id} onClick={() => switchVersion(v)}
                              className="w-full text-left px-3 py-1.5 text-sm hover:bg-zinc-700 cursor-pointer flex items-center gap-2">
                              <FileMusic className="w-3.5 h-3.5 text-blue-400 flex-shrink-0" />
                              <span className="text-zinc-300 truncate">{v.version_label}</span>
                            </button>
                          ))}
                        </div>
                      </div>
                    )}
                  </>
                )}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-3 w-80 justify-end">
            <span className="text-xs text-zinc-400 w-24 text-right tabular-nums">
              {formatDuration(ps.position)} / {formatDuration(track?.duration || 0)}
            </span>
            <div className="flex items-center gap-1">
              <button onClick={() => {
                if (ps.volume > 0) { prevVolume.current = ps.volume; localStorage.setItem("prevVolume", String(ps.volume)); ps.setVolume(0) }
                else ps.setVolume(prevVolume.current)
              }}
                className="p-1 text-zinc-400 hover:text-white cursor-pointer">
                {ps.volume === 0 ? <VolumeX className="w-4 h-4" /> : <Volume2 className="w-4 h-4" />}
              </button>
              <input type="range" min="0" max="1" step="0.05" value={ps.volume}
                onChange={e => ps.setVolume(parseFloat(e.target.value))}
                className="w-20 accent-green-500" />
            </div>
            <span className="w-9 flex justify-center">
              {track && (
                <button onClick={() => setShowLyrics(!showLyrics)}
                  className={`p-1 cursor-pointer ${showLyrics ? "text-green-500" : "text-zinc-400 hover:text-white"}`}
                  title="Lyrics">
                  <FileText className="w-4 h-4" />
                </button>
              )}
            </span>
            <span className="w-9 flex justify-center">
              {isDesktopLyricsSupported() && (
                <button onClick={toggleDesktopLyrics}
                  className={`p-1 cursor-pointer ${desktopLyricsOpen ? "text-green-500" : "text-zinc-400 hover:text-white"}`}
                  title="Desktop Lyrics">
                  <PictureInPicture2 className="w-4 h-4" />
                </button>
              )}
            </span>
            <button onClick={() => setShowQueue(!showQueue)}
              className={`p-1 cursor-pointer flex items-end w-12 ${showQueue ? "text-green-500" : "text-zinc-400 hover:text-white"}`}>
              <ListMusic className="w-4 h-4" />
              <span className="text-[9px] font-semibold leading-none ml-0.5">{ps.queue.length || ""}</span>
            </button>
          </div>
        </div>

        {showLyrics && <LyricsPanel onClose={() => setShowLyrics(false)} />}
        {showQueue && (
          <div className="absolute bottom-full right-0 w-96 max-h-80 bg-zinc-900 border border-zinc-800 rounded-t-xl shadow-xl flex flex-col">
            <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-800">
              <span className="text-xs text-zinc-500">Queue ({ps.queue.length})</span>
              <button onClick={ps.clearQueue}
                className="p-1 rounded text-zinc-500 hover:text-red-400 cursor-pointer" title="Clear queue">
                <Trash2 className="w-3.5 h-3.5" />
            </button>
            </div>
            <div className="overflow-y-auto max-h-64 p-1">
              {ps.queue.map((t, i) => {
                return (
                  <div key={t.id + i}
                    className={`flex items-center gap-2 px-2 py-1.5 rounded-lg text-sm cursor-pointer hover:bg-zinc-800 group ${i === ps.queueIdx ? "text-green-500 bg-zinc-800" : "text-zinc-300"}`}
                    onClick={() => ps.playIndex(i)}>
                    <span className="text-xs text-zinc-500 w-5 text-right shrink-0">{i + 1}</span>
                    <span className="truncate flex-1">{t.title}</span>
                    {t.version_label && (
                      <span className="text-xs text-zinc-500 shrink-0 max-w-24 truncate group-hover:hidden" title={t.version_label}>{t.version_label}</span>
                    )}
                    {t.artists && t.artists.length > 0 && (
                      <span className="text-xs text-zinc-500 shrink-0 group-hover:hidden"><ArtistLink artists={t.artists} /></span>
                    )}
                    <span className="flex items-center gap-1 shrink-0">
                      <span className="text-xs text-zinc-500 group-hover:hidden">{formatDuration(t.duration)}</span>
                      <button onClick={e => { e.stopPropagation(); ps.removeFromQueue(i) }}
                        className="hidden group-hover:flex items-center p-0.5 rounded text-zinc-500 hover:text-red-400 cursor-pointer">
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </span>
                  </div>
                )
              })}
              {ps.queue.length === 0 && <p className="text-xs text-zinc-600 text-center py-4">Empty</p>}
            </div>
          </div>
        )}
        {showQuality && (
          <div ref={qualityPanelRef} className="fixed z-[60] w-36 bg-zinc-800 border border-zinc-700 shadow-xl overflow-hidden rounded-t-xl"
            style={{ left: qualityPos.left, bottom: 64, transform: "translateX(-50%)" }}>
            {qualityOptions.map(o => (
              <button key={o.key}
                onClick={() => { setShowQuality(false); setQuality(o.key); saveQuality(o.key) }}
                className={`w-full text-left px-3 py-1.5 text-xs hover:bg-zinc-700 cursor-pointer flex items-center gap-1.5 ${quality === o.key ? "text-green-500" : "text-zinc-400"}`}>
                {qIcons[o.key]} {o.label} <span className="text-zinc-600 ml-1">{o.title}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
