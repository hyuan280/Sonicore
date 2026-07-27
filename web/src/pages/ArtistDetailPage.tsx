import { useEffect, useState } from "react"
import { useParams, useSearchParams } from "react-router-dom"
import { api } from "../api/client"
import { useLibrary } from "../stores/library"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { Play, Clock, CheckSquare, Plus, ListPlus, Heart, Mic2, Music } from "lucide-react"
import { formatDuration, coverUrl } from "../lib/utils"

export default function ArtistDetailPage() {
  const { artistId } = useParams()
  const [searchParams] = useSearchParams()
  const libId = searchParams.get("lib")
  const { activeId, libraries } = useLibrary()
  const player = usePlayer()
  const [artist, setArtist] = useState<any>(null)
  const [tracks, setTracks] = useState<PlayerTrack[]>([])
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])

  useEffect(() => {
    if (!artistId) return
    const id = libId || (activeId === "__all__" ? libraries[0]?.id : activeId)
    if (!id) return
    api.data.artist(id, artistId).then(async d => {
      setArtist(d.artist)
      const items: PlayerTrack[] = (d.tracks || []).map((t: any) => ({
        id: t.id, title: t.title, artist: d.artist?.name || "",
        album: t.album || "", album_id: t.album_id, duration: t.duration, suffix: t.file_format || "mp3",
      }))
      setTracks(items)
      if (items.length > 0) {
        const fav = await api.user.checkFavorites(items.map((t: PlayerTrack) => t.id))
        setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
      }
    })
  }, [artistId, libId, activeId, libraries.length])

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  const playAll = () => { if (tracks.length > 0) player.setQueue(tracks, 0) }

  if (!artist) return <div className="p-6 text-zinc-500">Loading...</div>

  return (
    <div className="p-6 space-y-6 pb-24">
      <div className="flex gap-6">
        <div className="w-48 h-48 rounded-full bg-zinc-800 flex-shrink-0 flex items-center justify-center overflow-hidden">
          {artist.cover_image_id ? (
            <img src={coverUrl("artist", artist.id)} alt={artist.name}
              className="w-full h-full object-cover"
              onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
          ) : null}
          <Mic2 className={`w-12 h-12 text-zinc-500 ${artist.cover_image_id ? "hidden" : ""}`} />
        </div>
        <div className="flex flex-col justify-end">
          <p className="text-xs uppercase tracking-wider text-zinc-400">Artist</p>
          <h1 className="text-3xl font-bold mt-1">{artist.name}</h1>
          <p className="text-sm text-zinc-500 mt-1">{tracks.length} tracks</p>
          <button className="mt-4 w-fit px-4 py-2 rounded-lg bg-green-600 text-white text-sm font-medium hover:bg-green-500 cursor-pointer flex items-center gap-2" onClick={playAll}>
            <Play className="w-4 h-4" />Play All
          </button>
        </div>
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
        <div className="flex items-center gap-1 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          <div className="flex items-center gap-1 w-1/2 shrink-0">
            {multi ? (
              <label className="flex items-center justify-center cursor-pointer shrink-0 w-10"
                onClick={() => {
                  if (selected.size === tracks.length) setSelected(new Set())
                  else setSelected(new Set(tracks.map(t => t.id)))
                }}>
                <input type="checkbox" checked={selected.size === tracks.length && tracks.length > 0}
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
            <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">Album</span>
            <span className="w-16 shrink-0 text-center"><Clock className="w-3 h-3 inline" /></span>
          </div>
        </div>
        {tracks.map((t, i) => (
          <div key={t.id}
            className="flex items-center gap-1 px-4 py-1 rounded-lg hover:bg-zinc-800/50 cursor-pointer group">
            <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
              <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                onClick={(e) => { e.stopPropagation(); if (multi) toggleSelect(t.id); else player.setQueue(tracks, i); }}>
                <img src={coverUrl("track", t.id, 256)} alt=""
                  className={`w-full h-full object-cover ${multi && selected.has(t.id) ? "opacity-60" : ""}`}
                  onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                <Music className="w-3.5 h-3.5 text-zinc-600 hidden" />
                {!multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/30 rounded opacity-0 group-hover:opacity-100 transition-opacity">
                    <Play className="w-5 h-5 text-white" />
                  </div>
                )}
                {multi && (
                  <div className="absolute inset-0 flex items-center justify-center bg-black/20 rounded">
                    {selected.has(t.id) ? (
                      <CheckSquare className="w-5 h-5 text-green-400" />
                    ) : (
                      <span className="w-5 h-5 rounded border-2 border-zinc-400" />
                    )}
                  </div>
                )}
              </div>
              <div className="w-7 shrink-0 justify-end inline-flex items-center" onClick={(e) => { e.stopPropagation(); player.setQueue(tracks, i); }}>
                <span className={`text-sm ${player.track?.id === t.id ? "text-green-500" : "text-zinc-500"}`}>{i + 1}</span>
              </div>
              <span className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${player.track?.id === t.id ? "text-green-500" : ""}`}
                onClick={() => player.setQueue(tracks, i)}>{t.title}</span>
            </div>
            <div className="flex items-center gap-1 flex-1 min-w-0">
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={t} />
                <AddBtn trackId={t.id} />
                <FavBtn trackId={t.id} initiallyFav={favoriteIds.has(t.id)}
                  onToggle={(id, nowFav) => { setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n }) }} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-sm text-zinc-500 truncate text-center hidden sm:block">{t.album || ""}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{formatDuration(t.duration)}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
