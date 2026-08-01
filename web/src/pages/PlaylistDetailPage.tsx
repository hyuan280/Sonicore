import { useEffect, useState } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { Button } from "../components/ui/button"
import { Play, Clock, Trash2, CheckSquare, Plus, ListPlus, Heart, ListMusic, Music } from "lucide-react"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { Link } from "react-router-dom"
import { formatDuration, coverUrl, performerNames } from "../lib/utils"
import ArtistLink from "../components/ArtistLink"

export default function PlaylistDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const player = usePlayer()
  const [playlist, setPlaylist] = useState<any>(null)
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])
  const [delConfirm, setDelConfirm] = useState(false)
  const [favs, setFavs] = useState<Set<string>>(new Set())

  const load = () => {
    if (!id) return
    api.user.getPlaylist(id).then(p => {
      setPlaylist(p)
      const ids = (p.tracks || []).map((t: any) => t.id)
      if (ids.length > 0) {
        api.user.checkFavorites(ids).then(d => {
          setFavs(new Set(Object.keys(d.favorites || {})))
        }).catch(() => {})
      }
    })
  }

  useEffect(() => { load() }, [id])

  const handleDelete = async () => {
    if (!id) return
    await api.user.deletePlaylist(id)
    navigate("/playlists")
  }

  const playTrack = (track: any, idx: number) => {
    const tracks = (playlist?.tracks || []).map((t: any): PlayerTrack => ({
      id: t.id, title: t.title,
      duration: t.duration, suffix: t.file_format || t.suffix || "mp3",
      cover_image_id: t.cover_image_id, artists: t.artists,
      albums: t.albums,
      version: t.version, version_label: t.version_label,
    }))
    player.setQueue(tracks, idx, id)
  }

  const removeTrack = async (trackId: string) => {
    if (!id) return
    await api.user.removeTracksFromPlaylist(id, [trackId])
    load()
  }

  const toggleSelect = (tid: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(tid)) n.delete(tid); else n.add(tid); return n })
  }
  const batchRemove = async () => {
    if (!id) return
    if (!confirm(`Remove ${selected.size} track(s) from playlist?`)) return
    await api.user.removeTracksFromPlaylist(id, [...selected])
    setSelected(new Set())
    load()
  }

  if (!playlist) return <div className="p-6 text-zinc-500">Loading...</div>
  const tracks: any[] = playlist.tracks || []


  return (
    <div className="p-6 space-y-6 pb-24">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <ListMusic className={`w-8 h-8 ${id && player.currentPlaylistId === id ? "text-green-500" : "text-zinc-500"}`} />
          <div>
            <h1 className="text-xl font-bold">{playlist.name}</h1>
          </div>
          <span className="text-xs text-zinc-500 self-end pb-0.5">{tracks.length} tracks</span>
        </div>
        <div className="flex gap-1">
          {delConfirm ? (
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
          )}
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
            <button onClick={() => { player.addToQueue(tracks.filter(t => selected.has(t.id))); setSelected(new Set()) }}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Plus className="w-4 h-4" /> Queue
            </button>
            <div className="relative">
              <button onClick={async () => { const d = await api.user.playlists(); setPlaylists((d.items || []).filter((p: any) => p.id !== id)); setPlOpen(!plOpen) }}
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
                  {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No other playlists</p>}
                </div>
              )}
            </div>
            <button onClick={async () => { await api.user.addFavorites("track", tracks.filter(t => selected.has(t.id)).map(t => t.id)); setSelected(new Set()) }}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Heart className="w-4 h-4" /> Favorite
            </button>
            <button onClick={batchRemove}
              className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              <Trash2 className="w-4 h-4" /> Remove
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
            <span className="w-24 shrink-0 text-center hidden sm:block">Artist</span>
            <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">Album</span>
            <span className="w-16 shrink-0 text-center"><Clock className="w-3 h-3 inline" /></span>
            <span className="w-10" />
          </div>
        </div>
        {tracks.map((t, i) => (
          <div key={t.id}
            className="flex items-center gap-1 px-4 py-0 rounded-lg hover:bg-zinc-800/50 cursor-pointer group"
            onClick={() => playTrack(t, i)}>
            <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
              <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                onClick={(e) => { e.stopPropagation(); if (multi) toggleSelect(t.id); else playTrack(t, i); }}>
                {t.cover_image_id ? (
                  <img src={coverUrl("track", t.id, 64)} alt=""
                    className={`w-full h-full object-cover ${multi && selected.has(t.id) ? "opacity-60" : ""}`}
                    onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                ) : null}
                <Music className={`w-3.5 h-3.5 text-zinc-600 ${t.cover_image_id ? "hidden" : ""}`} />
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
              <div className="w-7 shrink-0 justify-end inline-flex items-center" onClick={(e) => { e.stopPropagation(); playTrack(t, i); }}>
                <span className={`text-sm ${player.track?.id === t.id ? "text-green-500" : "text-zinc-500"}`}>{i + 1}</span>
              </div>
              <span className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${player.track?.id === t.id ? "text-green-500" : ""}`}
                onClick={() => playTrack(t, i)}>{t.title}</span>
            </div>
            <div className="flex items-center gap-1 flex-1 min-w-0">
              <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                <AddQueueBtn track={t} versions={t.versions} />
                <AddBtn trackId={t.id} />
                <FavBtn trackId={t.id} initiallyFav={favs.has(t.id)} />
              </span>
              <span className="flex-1 min-w-0" />
              <span className="w-24 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block"><ArtistLink artists={t.artists} /></span>
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">{t.albums?.[0]?.id ? <Link to={`/albums/${t.albums[0].id}`} className="text-sm text-zinc-500 truncate hover:text-white transition-colors" onClick={e => e.stopPropagation()}>{t.albums[0].title || ""}</Link> : <span className="text-sm text-zinc-500 truncate">{t.albums?.[0]?.title || ""}</span>}</span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{formatDuration(t.duration)}</span>
              <button onClick={e => { e.stopPropagation(); removeTrack(t.id) }}
                className="w-10 shrink-0 text-center text-zinc-500 hover:text-red-400 opacity-0 group-hover:opacity-100 cursor-pointer">
                <Trash2 className="w-4 h-4 inline" />
              </button>
            </div>
          </div>
        ))}
        {tracks.length === 0 && (
          <p className="text-zinc-500 text-center py-12">No tracks in this playlist</p>
        )}
      </div>
    </div>
  )
}
