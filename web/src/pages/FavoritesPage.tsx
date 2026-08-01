import { useEffect, useState } from "react"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { Button } from "../components/ui/button"
import { Play, Clock, CheckSquare, Plus, ListPlus, Heart, Music } from "lucide-react"
import { AddBtn, AddQueueBtn, FavBtn } from "../components/AddToPlaylist"
import { Link } from "react-router-dom"
import { formatDuration, coverUrl, performerNames } from "../lib/utils"
import ArtistLink from "../components/ArtistLink"

export default function FavoritesPage() {
  const player = usePlayer()
  const [items, setItems] = useState<any[]>([])
  const [removed, setRemoved] = useState<Set<string>>(new Set())
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])

  useEffect(() => { api.user.favorites("track").then(d => setItems(d.items || [])) }, [])

  const playAll = () => {
    const tracks: PlayerTrack[] = items.map(h => ({
      id: h.item_id, title: h.title || "Unknown",
      duration: h.duration || 0, suffix: h.suffix || "mp3",
      cover_image_id: h.cover_image_id, artists: h.artists,
      albums: h.albums,
    }))
    if (tracks.length > 0) player.setQueue(tracks, 0)
  }

  const removeAll = async () => {
    const ids = items.map(h => h.item_id)
    await api.user.removeFavorites("track", ids)
    setItems([])
  }

  const playTrack = (h: any) => {
    const track: PlayerTrack = {
      id: h.item_id, title: h.title || "Unknown",
      duration: h.duration || 0, suffix: h.suffix || "mp3",
      cover_image_id: h.cover_image_id, artists: h.artists,
      albums: h.albums,
    }
    const existingIdx = player.queue.findIndex(t => t.id === track.id)
    if (existingIdx >= 0) { player.playIndex(existingIdx); return }
    const newIdx = player.queue.length
    player.addToQueue([track])
    player.playIndex(newIdx)
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  return (
    <div className="p-6 space-y-6">
        <div className="flex items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold">Favorites</h1>
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
            <button onClick={() => player.addToQueue(items.filter(h => selected.has(h.item_id)).map(h => ({
              id: h.item_id, title: h.title || "Unknown", artist: performerNames(h.artists) || "",
              duration: h.duration || 0, suffix: h.suffix || "mp3",
              cover_image_id: h.cover_image_id, albums: h.albums,
            })))}
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
                    <button key={p.id} onClick={async () => { await api.user.addTracksToPlaylist(p.id, items.filter(h => selected.has(h.item_id)).map(h => h.item_id)); setPlOpen(false) }}
                      className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">{p.name}</button>
                  ))}
                  {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No playlists</p>}
                </div>
              )}
            </div>
            <button onClick={async () => {
                const sel = items.filter(h => selected.has(h.item_id))
                const allFav = sel.every(h => !removed.has(h.item_id))
                if (allFav) {
                  await api.user.removeFavorites("track", sel.map(h => h.item_id))
                  setRemoved(prev => { const n = new Set(prev); sel.forEach(h => n.add(h.item_id)); return n })
                } else {
                  await api.user.addFavorites("track", sel.map(h => h.item_id))
                  setRemoved(prev => { const n = new Set(prev); sel.filter(h => removed.has(h.item_id)).forEach(h => n.delete(h.item_id)); return n })
                }
              }}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Heart className={`w-4 h-4 ${items.filter(h => selected.has(h.item_id)).every(h => !removed.has(h.item_id)) ? "fill-current" : ""}`} />
              {items.filter(h => selected.has(h.item_id)).every(h => !removed.has(h.item_id)) ? "Unfavorite" : "Favorite"}
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
                  else setSelected(new Set(items.map(h => h.item_id)))
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
          </div>
        </div>
        {items.map((h, i) => (
          <div key={h.item_id}
            className="flex items-center gap-1 px-4 py-0 rounded-lg hover:bg-zinc-800/50 cursor-pointer group"
            onClick={() => playTrack(h)}>
            <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
              <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                onClick={(e) => { e.stopPropagation(); if (multi) toggleSelect(h.item_id); else playTrack(h); }}>
                {h.cover_image_id ? (
                  <img src={coverUrl("track", h.item_id, 64)} alt=""
                    className={`w-full h-full object-cover ${multi && selected.has(h.item_id) ? "opacity-60" : ""}`}
                    onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                ) : null}
                <Music className={`w-3.5 h-3.5 text-zinc-600 ${h.cover_image_id ? "hidden" : ""}`} />
                {!multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/30 rounded opacity-0 group-hover:opacity-100 transition-opacity">
                    <Play className="w-5 h-5 text-white" />
                  </div>
                )}
                {multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/20 rounded">
                    {selected.has(h.item_id) ? (
                      <CheckSquare className="w-5 h-5 text-green-400" />
                    ) : (
                      <span className="w-5 h-5 rounded border-2 border-zinc-400" />
                    )}
                  </div>
                )}
              </div>
              <div className="w-7 shrink-0 justify-end inline-flex items-center" onClick={(e) => { e.stopPropagation(); playTrack(h); }}>
                <span className={`text-sm ${player.track?.id === h.item_id ? "text-green-500" : "text-zinc-500"}`}>{i + 1}</span>
              </div>
              <span className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${player.track?.id === h.item_id ? "text-green-500" : ""}`}
                onClick={() => playTrack(h)}>{h.title || "Unknown track"}</span>
            </div>
            <div className="flex items-center gap-1 flex-1 min-w-0">
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={{ id: h.item_id, title: h.title || "", duration: h.duration || 0, suffix: h.suffix || "mp3", cover_image_id: h.cover_image_id, albums: h.albums }} versions={h.versions} />
                <AddBtn trackId={h.item_id} />
                <FavBtn trackId={h.item_id} initiallyFav={!removed.has(h.item_id)}
                  onToggle={(id, nowFav) => { setRemoved(prev => { const n = new Set(prev); nowFav ? n.delete(id) : n.add(id); return n }) }} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="w-24 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block"><ArtistLink artists={h.artists} /></span>
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">{h.albums?.[0]?.id ? <Link to={`/albums/${h.albums[0].id}`} className="text-sm text-zinc-500 truncate hover:text-white transition-colors" onClick={e => e.stopPropagation()}>{h.albums[0].title || ""}</Link> : <span className="text-sm text-zinc-500 truncate">{h.albums?.[0]?.title || ""}</span>}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{h.duration ? formatDuration(h.duration) : ""}</span>
            </div>
          </div>
        ))}
        {items.length === 0 && <p className="text-zinc-500 text-center py-12">No favorites yet</p>}
      </div>
    </div>
  )
}
