import { useCallback, useEffect, useState, useRef } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { useJukebox, type JukeboxStatus } from "../stores/jukebox"
import { usePlayer } from "../stores/player"
import { useLibrary } from "../stores/library"
import { api } from "../api/client"
import { Button } from "../components/ui/button"
import { Input } from "../components/ui/input"
import {
  Play, Pause, SkipBack, SkipForward, Shuffle, Trash2, Repeat, Repeat1,
  Turntable, Settings, ArrowUpFromLine, Loader2,
} from "lucide-react"
import { formatDuration } from "../lib/utils"

export default function JukeboxDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { delete: delJbx, updatePlaying } = useJukebox()
  const ps = usePlayer()

  const [status, _setStatus] = useState<JukeboxStatus | null>(null)
  const setStatus = useCallback((s: JukeboxStatus | null) => {
    _setStatus(s)
    if (id) updatePlaying(id, s?.state === "playing")
  }, [id, updatePlaying])
  const [jbxName, setJbxName] = useState("Jukebox")
  const [volume, setVolume] = useState(0.8)
  const [showSettings, setShowSettings] = useState(false)
  const [pushing, setPushing] = useState(false)
  const [queueTracks, setQueueTracks] = useState<Record<string, { title: string; artist: string; album: string }>>({})
  const wsRef = useRef<WebSocket | null>(null)
  const queueIdsRef = useRef("")

  const isPlaying = status?.state === "playing"
  const track = status?.track
  const playMode = status?.play_mode || "normal"
  const queue = status?.queue || []
  const queueIdx = status?.queue_idx ?? 0
  const hasTracks = queue.length > 0

  useEffect(() => {
    const key = queue.join(",")
    if (!key || key === queueIdsRef.current) return
    queueIdsRef.current = key
    api.data.tracksByIds(queue).then(d => {
      const map: Record<string, { title: string; artist: string; album: string }> = {}
      for (const t of d.tracks || []) {
        map[t.id] = {
          title: t.title,
          artist: t.artist?.name || "",
          album: t.album?.title || "",
        }
      }
      setQueueTracks(map)
    }).catch(() => {})
  }, [queue.join(",")])

  useEffect(() => {
    if (!id) return
    const sessionToken = localStorage.getItem("session_token") || ""
    const wsUrl = `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/${encodeURIComponent(sessionToken)}?channel=jukebox:${id}`
    console.log("[ws] connecting:", wsUrl)
    const ws = new WebSocket(wsUrl)
    wsRef.current = ws
    ws.onopen = () => console.log("[ws] connected:", id)
    ws.onerror = (e) => console.error("[ws] error:", id, e)
    ws.onclose = (e) => console.log("[ws] closed:", id, e.code, e.reason)
    ws.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data)
        if (data.event === "state_change" && data.status) {
          setStatus(data.status)
        }
      } catch {}
    }

    api.jukebox.status(id).then(setStatus).catch(() => {})

    return () => { ws.close() }
  }, [id])

  useEffect(() => {
    if (status?.volume !== undefined) setVolume(status.volume)
  }, [status?.volume])

  useEffect(() => {
    if (!id) return
    api.jukebox.get(id).then(j => {
      setJbxName(j.name || "Jukebox")
    }).catch(() => {})
  }, [id])

  const handlePlay = async () => {
    if (!id || !queue.length) return
    const idx = queueIdx >= 0 && queueIdx < queue.length ? queueIdx : 0
    const res = await api.jukebox.play(id, queue[idx])
    if (res) setStatus(res)
  }
  const handleStop = async () => {
    if (!id) return
    const res = await api.jukebox.stop(id)
    if (res) setStatus(res)
  }
  const handlePrev = async () => {
    if (!id) return
    const res = await api.jukebox.prev(id)
    if (res) setStatus(res)
  }
  const handleNext = async () => {
    if (!id) return
    const res = await api.jukebox.next(id)
    if (res) setStatus(res)
  }
  const handleMode = async () => {
    const modes = ["normal", "repeat_all", "repeat_one", "shuffle"]
    const next = modes[(modes.indexOf(playMode) + 1) % modes.length]
    if (!id) return
    const res = await api.jukebox.mode(id, next)
    if (res) setStatus(res)
  }
  const handleVol = (v: number) => {
    setVolume(v)
  }
  const handleVolCommit = async (v: number) => {
    if (!id) return
    const res = await api.jukebox.volume(id, v)
    if (res) setStatus(res)
  }
  const handleClear = async () => {
    if (!id) return
    const res = await api.jukebox.clearQueue(id)
    if (res) setStatus(res)
  }
  const handleDelete = async () => {
    if (!id) return
    await delJbx(id)
    navigate("/jukebox")
  }

  const handlePushQueue = async () => {
    if (!id || ps.queue.length === 0) return
    setPushing(true)
    try {
      await api.jukebox.setQueue(id, ps.queue.map(t => t.id))
      const s = await api.jukebox.status(id)
      if (s) setStatus(s)
    } finally {
      setPushing(false)
    }
  }


  const modeLabel = playMode === "normal" ? "Normal" : playMode === "repeat_all" ? "Repeat all" : playMode === "repeat_one" ? "Repeat one" : "Shuffle"
  const ModeIcon = playMode === "shuffle" ? Shuffle : playMode === "repeat_one" ? Repeat1 : Repeat

  const [delConfirm, setDelConfirm] = useState(false)
  const role = localStorage.getItem("role")
  const isAdmin = role === "admin" || role === "super_admin"

  return (
    <div className="p-6 space-y-6 pb-24">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Turntable className={`w-8 h-8 ${isPlaying ? "text-green-500" : "text-zinc-600"}`} />
          <h1 className="text-xl font-bold">{jbxName}</h1>
        </div>
        <div className="flex gap-1">
          {isAdmin && (
            <Button variant="ghost" size="sm" onClick={() => setShowSettings(!showSettings)}>
              <Settings className="w-4 h-4" />
            </Button>
          )}
          {isAdmin && (
            delConfirm ? (
              <div className="flex gap-1 items-center">
                <button onClick={() => { handleDelete(); setDelConfirm(false) }}
                  className="text-xs px-2 py-1 rounded bg-red-600/20 text-red-400 hover:bg-red-600/30 cursor-pointer">Delete?</button>
                <button onClick={() => setDelConfirm(false)}
                  className="text-xs px-2 py-1 rounded text-zinc-500 hover:text-white cursor-pointer">No</button>
              </div>
            ) : (
              <Button variant="ghost" size="sm" onClick={() => setDelConfirm(true)}>
                <Trash2 className="w-4 h-4" />
              </Button>
            )
          )}
        </div>
      </div>
      {/* Now Playing */}
      <div className="flex items-center gap-4 p-4 border border-zinc-800 rounded-xl bg-zinc-900/50 cursor-pointer" onClick={isPlaying ? handleStop : handlePlay}>
        <div className="w-14 h-14 bg-zinc-800 rounded-lg flex items-center justify-center flex-shrink-0">
          {isPlaying && hasTracks ? <Pause className="w-6 h-6 text-green-500" /> : <Play className="w-6 h-6 text-zinc-600" />}
        </div>
        <div className="flex-1 min-w-0">
          {track && hasTracks ? (
            <>
              <div className="font-medium truncate">{track.title}</div>
              <div className="text-sm text-zinc-400 truncate">{track.artist || "Unknown"}</div>
              <div className="text-xs text-zinc-500">{formatDuration(track.duration)}</div>
            </>
          ) : (
            <div className="text-zinc-500">Nothing playing</div>
          )}
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center gap-2 justify-center">
        <button onClick={handlePrev} className="p-2 text-zinc-400 hover:text-white cursor-pointer">
          <SkipBack className="w-5 h-5" />
        </button>
        <button onClick={handleNext} className="p-2 text-zinc-400 hover:text-white cursor-pointer">
          <SkipForward className="w-5 h-5" />
        </button>
        <button onClick={handleMode} className="p-2 cursor-pointer" title={modeLabel}>
          <ModeIcon className={`w-5 h-5 ${playMode === "normal" ? "text-zinc-500" : "text-green-500"}`} />
        </button>
        <div className="flex items-center gap-1 ml-4">
          <input type="range" min="0" max="1" step="0.01" value={volume}
            onChange={e => handleVol(parseFloat(e.target.value))}
            onMouseUp={e => handleVolCommit(parseFloat((e.target as HTMLInputElement).value))}
            onTouchEnd={e => handleVolCommit(parseFloat((e.target as HTMLInputElement).value))}
            className="w-24 accent-green-500" />
          <span className="text-xs text-zinc-500 w-8">{Math.round(volume * 100)}%</span>
        </div>
      </div>

      {showSettings && id && <PathMappingPanel jukeboxId={id} onClose={() => setShowSettings(false)} />}

      {/* Queue */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm text-zinc-400">Queue ({queue.length})</span>
          <div className="flex items-center gap-3">
            <button onClick={handlePushQueue} disabled={pushing || ps.queue.length === 0}
              className="text-xs text-zinc-500 hover:text-green-400 flex items-center gap-1 cursor-pointer disabled:opacity-30">
              {pushing ? <Loader2 className="w-3 h-3 animate-spin" /> : <ArrowUpFromLine className="w-3 h-3" />}
              Push
            </button>
            <button onClick={handleClear} className="text-xs text-zinc-500 hover:text-red-400 cursor-pointer">
              Clear
            </button>
          </div>
        </div>
        <div className="space-y-0.5 border border-zinc-800 rounded-lg">
          {queue.length === 0 && (
            <div className="text-center py-12 text-sm text-zinc-600">
              Queue is empty — push browser queue or add from songs
            </div>
          )}
          {queue.map((tid: string, i: number) => {
            const isCurrent = i === queueIdx && isPlaying
            const qt = queueTracks[tid]
            const displayTitle = isCurrent && track ? track.title : qt?.title || tid.substring(0, 12) + "..."
            const displayArtist = qt?.artist || ""
            const displayAlbum = qt?.album || ""
            return (
              <div key={tid + i} onClick={() => { if (!id) return; api.jukebox.play(id, tid).then(r => r && setStatus(r)) }}
                className={`flex items-center gap-3 px-3 py-2 text-sm cursor-pointer transition-colors ${
                  isCurrent ? "bg-green-600/10 text-green-400" : "text-zinc-400 hover:bg-zinc-800/50"
                }`}>
                <span className="w-6 text-zinc-600 text-right text-xs">{i + 1}</span>
                <div className="flex-1 min-w-0">
                  <div className={`truncate ${isCurrent ? "text-green-400" : "text-white"}`}>
                    {displayTitle}
                  </div>
                  {(displayArtist || displayAlbum) && (
                    <div className="text-xs text-zinc-500 truncate">
                      {displayArtist}{displayArtist && displayAlbum ? " · " : ""}{displayAlbum}
                    </div>
                  )}
                </div>
                {isCurrent && (
                  <Play className="w-3 h-3 text-green-500 flex-shrink-0" />
                )}
                <button onClick={e => { e.stopPropagation(); if (!id) return; api.jukebox.removeFromQueue(id, i).then(r => r && setStatus(r)) }}
                  className="text-zinc-600 hover:text-red-400 cursor-pointer flex-shrink-0">
                  <Trash2 className="w-3 h-3" />
                </button>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function PathMappingPanel({ jukeboxId, onClose }: { jukeboxId: string; onClose: () => void }) {
  const { libraries } = useLibrary()
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)

  useEffect(() => {
    api.jukebox.get(jukeboxId).then((j: any) => {
      const m: Record<string, string> = j.path_mapping || {}
      const clean: Record<string, string> = {}
      for (const [k, v] of Object.entries(m)) {
        if (typeof v === 'string' && v.trim()) clean[k] = v.trim()
      }
      setMapping(clean)
      setLoaded(true)
    }).catch(() => {})
  }, [jukeboxId])

  const handleSave = async () => {
    setSaving(true)
    try {
      const clean: Record<string, string> = {}
      for (const [k, v] of Object.entries(mapping)) {
        if (v.trim()) clean[k] = v.trim()
      }
      await api.jukebox.updateSettings(jukeboxId, { path_mapping: clean })
    } finally { setSaving(false) }
  }

  const paths = libraries.map(l => ({
    name: l.name,
    id: l.id,
    src: l.path,
    dest: mapping[l.id] || "",
  }))

  return (
    <div className="border border-zinc-800 rounded-xl p-4 space-y-3 bg-zinc-900/50 mb-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-zinc-300">Path Mapping</h3>
        <div className="flex gap-2">
          <button onClick={handleSave} disabled={saving}
            className="text-xs text-zinc-400 hover:text-green-400 cursor-pointer disabled:opacity-30">
            {saving ? "Saving..." : "Save"}
          </button>
          <button onClick={onClose}
            className="text-xs text-zinc-500 hover:text-white cursor-pointer">Close</button>
        </div>
      </div>

      <div className="space-y-2 max-h-48 overflow-y-auto">
        {!loaded ? (
          <div className="text-center py-4 text-sm text-zinc-500">Loading...</div>
        ) : paths.length === 0 ? (
          <div className="text-center py-4 text-sm text-zinc-500">No libraries found</div>
        ) : paths.map(p => (
          <div key={p.id} className="border border-zinc-800 rounded-lg px-3 py-2">
            <div className="text-sm font-bold text-zinc-300 mb-1">{p.name}</div>
            <div className="flex items-center gap-1 text-sm flex-wrap">
              {editing === p.id ? (
                <>
                  <span className="text-zinc-300">{p.src}</span>
                  <span className="text-zinc-600">⇒</span>
                  <input
                    value={mapping[p.id] || ""}
                    onChange={e => {
                      const v = e.target.value
                      setMapping(prev => {
                        const next = { ...prev }
                        if (v) next[p.id] = v
                        else delete next[p.id]
                        return next
                      })
                    }}
                    onBlur={() => setEditing(null)}
                    onKeyDown={e => e.key === "Enter" && setEditing(null)}
                    placeholder="/mnt/remote/path"
                    className="bg-zinc-800 border border-zinc-600 rounded px-2 py-0.5 text-sm text-white flex-1 min-w-0 focus:outline-none focus:border-green-500"
                    autoFocus
                  />
                </>
              ) : p.dest ? (
                <button onClick={() => setEditing(p.id)}
                  className="text-left text-zinc-300 hover:text-green-400 transition-colors cursor-pointer w-full">
                  <span>{p.src}</span>
                  <span className="text-zinc-600 mx-1">⇒</span>
                  <span className="text-green-400">{p.dest}</span>
                </button>
              ) : (
                <button onClick={() => setEditing(p.id)}
                  className="text-left text-zinc-500 hover:text-zinc-300 transition-colors cursor-pointer w-full">
                  {p.src}
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
