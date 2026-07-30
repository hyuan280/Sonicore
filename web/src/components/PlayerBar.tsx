import { useRef, useEffect, useState, useCallback } from "react"
import { usePlayer, savePlayerState } from "../stores/player"
import { useAuth } from "../stores/auth"
import { api } from "../api/client"
import { Play, Pause, SkipBack, SkipForward, Volume2, VolumeX, ListMusic, Repeat, Shuffle, Trash2, Repeat1, Heart, Music, FileText } from "lucide-react"
import { Link } from "react-router-dom"
import { formatDuration, coverUrl, performerNames } from "../lib/utils"
import ArtistLink from "../components/ArtistLink"

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
  const audioRef = useRef<HTMLAudioElement>(null)
  const progressRef = useRef<HTMLDivElement>(null)
  const currentTrackRef = useRef<string | null>(null)
  const [showQueue, setShowQueue] = useState(false)
  const [showLyrics, setShowLyrics] = useState(false)
  const [showQuality, setShowQuality] = useState(false)
  const [fav, setFav] = useState(false)
  const [quality, setQuality] = useState(loadQuality)
  const lastHistoryRef = useRef(0)
  const session = localStorage.getItem("session_token")

  useEffect(() => {
    if (ps.track) {
      api.user.checkFavorites([ps.track.id]).then(d => setFav(!!d.favorites?.[ps.track!.id])).catch(() => {})
    }
  }, [ps.track?.id])

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

    const session = localStorage.getItem("session_token")
    const q = quality === "standard" ? "" : `?quality=${quality}`
    el.src = session ? `/api/s/${session}/${ps.track.id}${q}` : ""
    el.volume = ps.volume

    const onEnded = () => {
      if (currentTrackRef.current !== trackId) return
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
      s.setPlaying(true)
      savePlayerState()
      lastHistoryRef.current = 0
      if (s.position > 1 && el.currentTime < 1) {
        el.currentTime = s.position
      }
    }

    const onPause = () => {
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
      usePlayer.getState().setPlaying(false)
    }

    if (ps.playing) {
      if (el.ended) el.currentTime = 0
      el.play().catch(() => usePlayer.getState().setPlaying(false))
    }

    el.addEventListener("ended", onEnded)
    el.addEventListener("timeupdate", onTimeUpdate)
    el.addEventListener("play", onPlay)
    el.addEventListener("pause", onPause)
    el.addEventListener("error", onError)

    return () => {
      el.removeEventListener("ended", onEnded)
      el.removeEventListener("timeupdate", onTimeUpdate)
      el.removeEventListener("play", onPlay)
      el.removeEventListener("pause", onPause)
      el.removeEventListener("error", onError)
    }
  }, [ps.track?.id, ps.playEpoch, quality])

  useEffect(() => {
    if (audioRef.current) audioRef.current.volume = ps.volume
  }, [ps.volume])

  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    if (!ps.track) { el.pause(); el.src = ""; return }
    if (ps.playing) {
      if (el.ended) el.currentTime = 0
      el.play().catch(() => usePlayer.getState().setPlaying(false))
    } else {
      el.pause()
    }
  }, [ps.track?.id, ps.playing])

  const seek = useCallback((e: React.MouseEvent) => {
    const el = audioRef.current
    const bar = progressRef.current
    if (!el || !bar || !ps.track) return
    const rect = bar.getBoundingClientRect()
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    const target = pct * ps.track.duration
    el.currentTime = Math.min(target, ps.track.duration - 0.5)
  }, [ps.track])

  const qualityRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!showQuality) return
    const handle = (e: MouseEvent) => {
      if (qualityRef.current && !qualityRef.current.contains(e.target as Node)) {
        setShowQuality(false)
      }
    }
    document.addEventListener("mousedown", handle)
    return () => document.removeEventListener("mousedown", handle)
  }, [showQuality])

  const currentQ = qualityOptions.find(o => o.key === quality)!

  const track = ps.track

  return (
    <>
      <audio ref={audioRef} preload="auto" />
      <div className="fixed bottom-0 left-0 right-0 bg-zinc-900 border-t border-zinc-800 px-4 py-2 z-50 h-16">
        <div className="absolute top-0 left-0 right-0 h-2" />
        <div ref={progressRef} className="absolute top-2 left-0 right-0 h-1 bg-zinc-800 cursor-pointer group"
          onClick={seek}>
          {track && (
            <div className="h-full bg-green-500 transition-all"
              style={{ width: `${(ps.position / (track.duration || 1)) * 100}%` }} />
          )}
        </div>

        <div className="flex items-center gap-4 max-w-screen-2xl mx-auto pt-2">
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
              <div className="text-sm text-zinc-500">No track playing</div>
            )}
          </div>

          <div className="flex-1 flex items-center justify-center gap-3">
            <div className="relative" ref={qualityRef}>
              <button onClick={() => setShowQuality(!showQuality)}
                className="text-[11px] font-semibold px-1.5 py-0.5 rounded border border-zinc-700 text-zinc-400 hover:text-white hover:border-zinc-500 cursor-pointer"
                title={currentQ.desc}>
                {currentQ.label}
              </button>
              {showQuality && (
                <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 w-28 bg-zinc-800 border border-zinc-700 rounded-lg shadow-xl overflow-hidden">
                  {qualityOptions.map(o => (
                    <button key={o.key}
                      onClick={() => { setShowQuality(false); setQuality(o.key); saveQuality(o.key) }}
                      className={`w-full text-left px-3 py-1.5 text-xs hover:bg-zinc-700 cursor-pointer ${quality === o.key ? "text-green-500" : "text-zinc-400"}`}>
                      {o.label} <span className="text-zinc-600 ml-1">{o.title}</span>
                    </button>
                  ))}
                </div>
              )}
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
            {ps.track && (
              <button onClick={toggleFav}
                className={`p-1 cursor-pointer text-zinc-500 hover:text-red-400 ${fav ? "text-red-500" : ""}`}
                title={fav ? "Remove from favorites" : "Add to favorites"}>
                <Heart className={`w-5 h-5 ${fav ? "fill-red-500" : ""}`} />
              </button>
            )}
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
          </div>

          <div className="flex items-center gap-3 w-80 justify-end">
            <span className="text-xs text-zinc-400 w-24 text-right tabular-nums">
              {formatDuration(ps.position)} / {formatDuration(track?.duration || 0)}
            </span>
            <div className="flex items-center gap-1">
              <button onClick={() => ps.setVolume(ps.volume === 0 ? 0.8 : 0)}
                className="p-1 text-zinc-400 hover:text-white cursor-pointer">
                {ps.volume === 0 ? <VolumeX className="w-4 h-4" /> : <Volume2 className="w-4 h-4" />}
              </button>
              <input type="range" min="0" max="1" step="0.05" value={ps.volume}
                onChange={e => ps.setVolume(parseFloat(e.target.value))}
                className="w-20 accent-green-500" />
            </div>
            {track && (
              <button onClick={() => setShowLyrics(!showLyrics)}
                className={`p-1 cursor-pointer ${showLyrics ? "text-green-500" : "text-zinc-400 hover:text-white"}`}
                title="Lyrics">
                <FileText className="w-4 h-4" />
              </button>
            )}
            <button onClick={() => setShowQueue(!showQueue)}
              className={`p-1 cursor-pointer ${showQueue ? "text-green-500" : "text-zinc-400 hover:text-white"}`}>
              <ListMusic className="w-4 h-4" />
            </button>
          </div>
        </div>

        {showLyrics && (
          <div className="absolute bottom-full left-0 w-96 max-h-96 bg-zinc-900 border border-zinc-800 rounded-t-xl shadow-xl flex flex-col">
            <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-800">
              <span className="text-xs text-zinc-500">Lyrics</span>
              <button onClick={() => setShowLyrics(false)}
                className="p-1 rounded text-zinc-500 hover:text-white cursor-pointer" title="Close">
                <span className="text-sm">&times;</span>
              </button>
            </div>
            <div className="overflow-y-auto max-h-80 p-4 text-sm text-zinc-300 leading-relaxed whitespace-pre-wrap font-mono">
              {ps.lyrics || <span className="text-zinc-600 italic">No lyrics available</span>}
            </div>
          </div>
        )}
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
      </div>
    </>
  )
}
