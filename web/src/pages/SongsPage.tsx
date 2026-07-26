import { useEffect, useState } from "react"
import { api } from "../api/client"
import { useLibrary } from "../stores/library"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { Clock, Play, CheckSquare, Plus, ListPlus, Heart } from "lucide-react"
import { formatDuration } from "../lib/utils"

export default function SongsPage() {
  const { activeId, libraries } = useLibrary()
  const player = usePlayer()
  const [tracks, setTracks] = useState<PlayerTrack[]>([])
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])
  const perPage = 50

  const load = async () => {
    if (!activeId) return
    const ids = activeId === "__all__" ? libraries.map(l => l.id) : [activeId]
    const results = await Promise.all(ids.map(id => api.data.tracks(id, 1, 9999)))
    const merged = results.flatMap(r => r.items || [])
    const totalItems = results.reduce((sum, r) => sum + (r.total || 0), 0)
    const start = (page - 1) * perPage
    const pageItems = merged.slice(start, start + perPage)
    setTracks(pageItems)
    setTotal(totalItems)
    if (pageItems.length > 0) {
      const fav = await api.user.checkFavorites(pageItems.map(t => t.id))
      setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
    }
  }
  useEffect(() => { load() }, [activeId, page, libraries.length])

  const totalPages = Math.ceil(total / perPage)

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  const selectAll = () => {
    if (selected.size === tracks.length) setSelected(new Set())
    else setSelected(new Set(tracks.map(t => t.id)))
  }

  const playTrack = (t: PlayerTrack, idx: number) => player.setQueue(tracks, idx)

  return (
    <div className="p-6 space-y-4 pb-24">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Songs</h1>
        <span className="text-sm text-zinc-400">{total} tracks</span>
      </div>

      <div className="flex items-center gap-3">
        <button onClick={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm cursor-pointer transition-colors ${multi ? "bg-green-600/20 text-green-500" : "bg-zinc-800 text-zinc-400 hover:text-white"}`}>
          <CheckSquare className="w-4 h-4" />
          {multi && selected.size > 0 ? `${selected.size} selected` : "Select"}
        </button>
        {multi && selected.size > 0 && (
          <div className="flex items-center gap-2">
            <button onClick={() => player.addToQueue(tracks.filter(t => selected.has(t.id)))}
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
                    <button key={p.id} onClick={async () => { await api.user.addTracksToPlaylist(p.id, tracks.filter(t => selected.has(t.id)).map(t => t.id)); setPlOpen(false) }}
                      className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">{p.name}</button>
                  ))}
                  {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No playlists</p>}
                </div>
              )}
            </div>
            <button onClick={async () => {
                const sel = tracks.filter(t => selected.has(t.id))
                const allFav = sel.every(t => favoriteIds.has(t.id))
                if (allFav) {
                  await api.user.removeFavorites("track", sel.map(t => t.id))
                  setFavoriteIds(prev => { const n = new Set(prev); sel.forEach(t => n.delete(t.id)); return n })
                } else {
                  await api.user.addFavorites("track", sel.map(t => t.id))
                  setFavoriteIds(prev => { const n = new Set(prev); sel.forEach(t => n.add(t.id)); return n })
                }
              }}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Heart className={`w-4 h-4 ${tracks.filter(t => selected.has(t.id)).every(t => favoriteIds.has(t.id)) ? "fill-red-500 text-red-500" : ""}`} />
              {tracks.filter(t => selected.has(t.id)).every(t => favoriteIds.has(t.id)) ? "Unfavorite" : "Favorite"}
            </button>
          </div>
        )}
      </div>

      <div className="space-y-1">
        <div className="flex items-center gap-2 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          {multi ? (
            <label className="flex items-center cursor-pointer shrink-0 w-4 h-4" onClick={selectAll}>
              <input type="checkbox" checked={selected.size === tracks.length && tracks.length > 0}
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
        </div>
        {tracks.map((t, i) => {
          const displayIdx = (page - 1) * perPage + i
          const isCurrent = player.track?.id === t.id
          return (
            <div key={t.id}
              className={`flex items-center gap-2 px-4 py-1.5 rounded-lg group transition-colors ${isCurrent ? "bg-green-600/10" : "hover:bg-zinc-800/50"}`}>
              {multi ? (
                <input type="checkbox" checked={selected.has(t.id)}
                  onChange={() => toggleSelect(t.id)}
                  onClick={e => e.stopPropagation()}
                  className="accent-green-500 cursor-pointer shrink-0 w-4 h-4" />
              ) : (
                <span className="inline-block w-4 h-4 shrink-0" />
              )}
              <div className="w-7 shrink-0 justify-end cursor-pointer inline-flex items-center" onClick={() => playTrack(t, i)}>
                <span className={`text-sm ${isCurrent ? "text-green-500" : "text-zinc-500"} group-hover:hidden`}>{displayIdx + 1}</span>
                <Play className={`w-3.5 h-3.5 hidden group-hover:inline ${isCurrent ? "text-green-500" : "text-green-500"}`} />
              </div>
              <span className={`w-1/2 min-w-0 text-sm truncate ml-3 cursor-pointer ${isCurrent ? "text-green-500" : ""}`}
                onClick={() => playTrack(t, i)}>{t.title}</span>
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={t} />
                <AddBtn trackId={t.id} />
                <FavBtn trackId={t.id} initiallyFav={favoriteIds.has(t.id)}
                  onToggle={(id, nowFav) => { setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n }) }} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="w-36 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block">{t.artist || ""}</span>
              <span className="w-36 shrink-0 text-sm text-zinc-500 truncate text-center hidden sm:block">{t.album || ""}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{formatDuration(t.duration)}</span>
            </div>
          )
        })}
        {tracks.length === 0 && <p className="text-zinc-500 text-center py-12">No songs found</p>}
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 pt-4">
          <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}
            className="px-3 py-1.5 rounded-lg text-sm bg-zinc-800 hover:bg-zinc-700 disabled:opacity-30 cursor-pointer">Prev</button>
          <span className="text-sm text-zinc-400">{page} / {totalPages}</span>
          <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}
            className="px-3 py-1.5 rounded-lg text-sm bg-zinc-800 hover:bg-zinc-700 disabled:opacity-30 cursor-pointer">Next</button>
        </div>
      )}
    </div>
  )
}
