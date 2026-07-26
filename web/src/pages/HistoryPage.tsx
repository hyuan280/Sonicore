import { useEffect, useState } from "react"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { api } from "../api/client"
import { Button } from "../components/ui/button"
import { Play, Trash2, Clock, Calendar, CheckSquare, Plus, ListPlus, Heart } from "lucide-react"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { formatDuration } from "../lib/utils"

export default function HistoryPage() {
  const player = usePlayer()
  const [items, setItems] = useState<any[]>([])
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])

  const load = async () => {
    const d = await api.user.history()
    const items = d.items || []
    setItems(items)
    if (items.length > 0) {
      const fav = await api.user.checkFavorites(items.map((h: any) => h.track_id))
      setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
    }
  }
  useEffect(() => { load() }, [])

  const playAll = () => {
    const tracks: PlayerTrack[] = items.map(h => ({
      id: h.track_id, title: h.title || "Unknown",
      artist: h.artist || "", album: h.album || "",
      album_id: h.album_id || "", duration: h.duration || 0, suffix: h.suffix || "mp3",
    }))
    if (tracks.length > 0) player.setQueue(tracks, 0)
  }

  const clearAll = async () => {
    if (!confirm("Clear all listening history?")) return
    await api.user.clearHistory()
    setItems([])
  }

  const toTrack = (h: any): PlayerTrack => ({
    id: h.track_id, title: h.title || "Unknown",
    artist: h.artist || "", album: h.album || "",
    album_id: h.album_id || "", duration: h.duration || 0, suffix: h.suffix || "",
  })

  const playTrack = (h: any) => {
    const track = toTrack(h)
    const existingIdx = player.queue.findIndex(t => t.id === track.id)
    if (existingIdx >= 0) { player.playIndex(existingIdx); return }
    const newIdx = player.queue.length
    player.addToQueue([track])
    player.playIndex(newIdx)
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  const selectAll = () => {
    if (selected.size === items.length) setSelected(new Set())
    else setSelected(new Set(items.map(h => h.id)))
  }

  const batchQueue = () => {
    const tracks = items.filter(h => selected.has(h.id)).map(toTrack)
    player.addToQueue(tracks)
  }

  const batchFav = async () => {
    const sel = items.filter(h => selected.has(h.id))
    await api.user.addFavorites("track", sel.map(h => h.track_id))
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold">History</h1>
          <p className="text-sm text-zinc-500 mt-1">{items.length} tracks</p>
        </div>
        {items.length > 0 && (
          <Button onClick={playAll}><Play className="w-4 h-4 mr-1" />Play All</Button>
        )}
      </div>

      <div className="flex items-center gap-3">
        <button onClick={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm cursor-pointer transition-colors ${multi ? "bg-green-600/20 text-green-500" : "bg-zinc-800 text-zinc-400 hover:text-white"}`}>
          <CheckSquare className="w-4 h-4" />
          {multi && selected.size > 0 ? `${selected.size} selected` : "Select"}
        </button>
        {multi && selected.size > 0 && (
          <div className="flex items-center gap-2">
            <button onClick={batchQueue}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Plus className="w-4 h-4" /> Queue
            </button>
            <div className="relative">
              <button onClick={async () => { const d = await api.user.playlists(); setPlaylists(d.items || []); setPlOpen(!plOpen) }}
                className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
                <ListPlus className="w-4 h-4" /> Playlist
              </button>
              {plOpen && (
                <div className="absolute left-0 top-8 w-48 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-50 py-1 max-h-48 overflow-y-auto"
                  onClick={e => e.stopPropagation()}>
                  <p className="text-xs text-zinc-500 px-3 py-1.5">Add to playlist</p>
                  {playlists.map((p: any) => (
                    <button key={p.id} onClick={async () => { await api.user.addTracksToPlaylist(p.id, items.filter(h => selected.has(h.id)).map(h => h.track_id)); setPlOpen(false) }}
                      className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">{p.name}</button>
                  ))}
                  {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No playlists</p>}
                </div>
              )}
            </div>
            <button onClick={batchFav}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Heart className="w-4 h-4" /> Favorite
            </button>
          </div>
        )}
      </div>

      <div className="space-y-1">
        <div className="flex items-center gap-2 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          {multi ? (
            <label className="flex items-center cursor-pointer shrink-0 w-4 h-4" onClick={selectAll}>
              <input type="checkbox" checked={selected.size === items.length && items.length > 0}
                onChange={() => {}} className="accent-green-500 cursor-pointer" />
            </label>
          ) : (
            <span className="inline-block w-4 h-4 shrink-0" />
          )}
          <span className="w-7 text-right shrink-0">#</span>
          <span className="w-1/2 min-w-0 ml-3">Title</span>
          <span className="w-20 shrink-0" />
          <span className="flex-1 min-w-0" />
          <span className="w-36 shrink-0 text-center hidden sm:block">Artist</span>
          <span className="w-36 shrink-0 text-center hidden sm:block">Album</span>
          <span className="w-16 shrink-0 text-center"><Clock className="w-3 h-3 inline" /></span>
          <span className="w-44 shrink-0 text-center hidden sm:block"><Calendar className="w-3 h-3 inline" /></span>
        </div>
        {items.map((h, i) => (
          <div key={h.id}
            className="flex items-center gap-2 px-4 py-2 rounded-lg hover:bg-zinc-800/50 cursor-pointer group"
            onClick={() => playTrack(h)}>
            {multi ? (
              <input type="checkbox" checked={selected.has(h.id)}
                onChange={() => toggleSelect(h.id)}
                onClick={e => e.stopPropagation()}
                className="accent-green-500 cursor-pointer shrink-0 w-4 h-4" />
            ) : (
              <span className="inline-block w-4 h-4 shrink-0" />
            )}
              <div className="w-7 shrink-0 justify-end cursor-pointer inline-flex items-center" onClick={() => playTrack(h)}>
                <span className={`text-sm ${player.queue.find(q => q.id === h.track_id) === player.queue[player.queueIdx] ? "text-green-500" : "text-zinc-500"} group-hover:hidden`}>{i + 1}</span>
                <Play className={`w-3.5 h-3.5 hidden group-hover:inline text-green-500`} />
              </div>
              <span className={`w-1/2 min-w-0 text-sm truncate ml-3 cursor-pointer ${player.queue.find(q => q.id === h.track_id) === player.queue[player.queueIdx] ? "text-green-500" : ""}`}
                onClick={() => playTrack(h)}>{h.title || "Unknown track"}</span>
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={{ id: h.track_id, title: h.title || "", artist: h.artist || "", album: h.album || "", album_id: h.album_id || "", duration: h.duration || 0, suffix: h.suffix || "mp3" }} />
                <AddBtn trackId={h.track_id} />
                <FavBtn trackId={h.track_id} initiallyFav={favoriteIds.has(h.track_id)}
                  onToggle={(id, nowFav) => { setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n }) }} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="w-36 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block">{h.artist || ""}</span>
              <span className="w-36 shrink-0 text-sm text-zinc-500 truncate text-center hidden sm:block">{h.album || ""}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{h.duration ? formatDuration(h.duration) : ""}</span>
              <span className="w-44 shrink-0 text-sm text-zinc-500 truncate text-center hidden sm:block">{h.played_at ? new Date(h.played_at).toLocaleString() : ""}</span>
          </div>
        ))}
        {items.length === 0 && <p className="text-zinc-500 text-center py-12">No listening history yet</p>}
      </div>
    </div>
  )
}
