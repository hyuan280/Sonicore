import { useEffect, useState } from "react"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { api } from "../api/client"
import { Button } from "../components/ui/button"
import { Play, Trash2, Clock, Calendar, CheckSquare, Plus, ListPlus, Heart, Music } from "lucide-react"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { formatDuration, coverUrl } from "../lib/utils"

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
  const batchQueue = () => {
    const tracks = items.filter(h => selected.has(h.id)).map(toTrack)
    player.addToQueue(tracks)
  }

  const batchFav = async () => {
    const sel = items.filter(h => selected.has(h.id))
    await api.user.addFavorites("track", sel.map(h => h.track_id))
  }

  const deleteItem = async (id: string) => {
    if (!confirm("Remove this history entry?")) return
    await api.user.deleteHistoryItems([id])
    setItems(prev => prev.filter(h => h.id !== id))
  }

  const batchDelete = async () => {
    if (!confirm(`Remove ${selected.size} history entr${selected.size > 1 ? "ies" : "y"}?`)) return
    await api.user.deleteHistoryItems([...selected])
    setItems(prev => prev.filter(h => !selected.has(h.id)))
    setSelected(new Set())
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
            <button onClick={batchDelete}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-red-400 cursor-pointer">
              <Trash2 className="w-4 h-4" /> Delete
            </button>
          </div>
        )}
      </div>

      <div className="space-y-1">
        <div className="flex items-center gap-1 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          <div className="flex items-center gap-1 w-1/2 shrink-0">
            {multi ? (
              <label className="flex items-center justify-center cursor-pointer shrink-0 w-10"
                onClick={() => {
                  if (selected.size === items.length) setSelected(new Set())
                  else setSelected(new Set(items.map(h => h.id)))
                }}>
                <input type="checkbox" checked={selected.size === items.length && items.length > 0}
                  onChange={() => {}} className="accent-green-500 cursor-pointer" />
              </label>
            ) : (
              <span className="w-10 shrink-0" />
            )}
            <span className="w-7 text-right shrink-0">#</span>
            <span className="flex-1 min-w-0 ml-3">Title</span>
          </div>
          <div className="flex items-center gap-1 flex-1">
            <span className="w-20 shrink-0" />
            <span className="flex-1 min-w-0" />
            <span className="w-24 shrink-0 text-center hidden sm:block">Artist</span>
            <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">Album</span>
            <span className="w-16 shrink-0 text-center"><Clock className="w-3 h-3 inline" /></span>
            <span className="min-w-[80px] max-w-[140px] shrink-0 text-center hidden sm:block"><Calendar className="w-3 h-3 inline" /></span>
          </div>
        </div>
        {items.map((h, i) => (
          <div key={h.id}
            className="flex items-center gap-1 px-4 py-1 rounded-lg hover:bg-zinc-800/50 cursor-pointer group"
            onClick={() => playTrack(h)}>
            <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
              <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                onClick={(e) => { e.stopPropagation(); if (multi) toggleSelect(h.id); else playTrack(h); }}>
                <img src={coverUrl("track", h.track_id, 256)} alt=""
                  className={`w-full h-full object-cover ${multi && selected.has(h.id) ? "opacity-60" : ""}`}
                  onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                <Music className="w-3.5 h-3.5 text-zinc-600 hidden" />
                {!multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/30 rounded opacity-0 group-hover:opacity-100 transition-opacity">
                    <Play className="w-5 h-5 text-white" />
                  </div>
                )}
                {multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/20 rounded">
                    {selected.has(h.id) ? (
                      <CheckSquare className="w-5 h-5 text-green-400" />
                    ) : (
                      <span className="w-5 h-5 rounded border-2 border-zinc-400" />
                    )}
                  </div>
                )}
              </div>
              <div className="w-7 shrink-0 justify-end inline-flex items-center" onClick={(e) => { e.stopPropagation(); playTrack(h); }}>
                <span className={`text-sm ${player.track?.id === h.track_id ? "text-green-500" : "text-zinc-500"}`}>{i + 1}</span>
              </div>
              <span className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${player.track?.id === h.track_id ? "text-green-500" : ""}`}
                onClick={() => playTrack(h)}>{h.title || "Unknown track"}</span>
            </div>
            <div className="flex items-center gap-1 flex-1 min-w-0">
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={{ id: h.track_id, title: h.title || "", artist: h.artist || "", album: h.album || "", album_id: h.album_id || "", duration: h.duration || 0, suffix: h.suffix || "mp3" }} />
                <AddBtn trackId={h.track_id} />
                <FavBtn trackId={h.track_id} initiallyFav={favoriteIds.has(h.track_id)}
                  onToggle={(id, nowFav) => { setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n }) }} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="w-24 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block">{h.artist || ""}</span>
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-sm text-zinc-500 truncate text-center hidden sm:block">{h.album || ""}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{h.duration ? formatDuration(h.duration) : ""}</span>
              <span className="min-w-[80px] max-w-[140px] shrink-0 text-center hidden sm:block leading-tight">
                <span className="group-hover:hidden"><span className="text-zinc-500 text-xs">{h.played_at ? new Date(h.played_at).toLocaleDateString() : ""}</span><br /><span className="text-zinc-500 text-[10px]">{h.played_at ? new Date(h.played_at).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : ""}</span></span>
                <button onClick={e => { e.stopPropagation(); deleteItem(h.id) }}
                  className="hidden group-hover:inline-flex items-center justify-center w-full cursor-pointer text-zinc-500 hover:text-red-400">
                  <Trash2 className="w-4 h-4" />
                </button>
              </span>
            </div>
          </div>
        ))}
        {items.length === 0 && <p className="text-zinc-500 text-center py-12">No listening history yet</p>}
      </div>
    </div>
  )
}
