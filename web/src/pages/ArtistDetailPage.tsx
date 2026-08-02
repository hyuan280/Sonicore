import { useEffect, useState, useMemo } from "react"
import { useParams, Link } from "react-router-dom"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { AddBtn, FavBtn, AddQueueBtn } from "../components/AddToPlaylist"
import { Play, Clock, CheckSquare, Plus, ListPlus, Heart, Mic2, Music } from "lucide-react"
import { formatDuration, coverUrl } from "../lib/utils"

const roleLabels: Record<string, string> = {
  performer: "Singer", composer: "Composer", lyricist: "Lyricist",
  arranger: "Arranger", album_artist: "Album Artist", producer: "Producer",
  conductor: "Conductor", remixer: "Remixer",
}

interface RawTrack {
  id: string; title: string; duration: number
  file_format: string; cover_image_id?: string
  artists: { artist_id: string; name: string; role: string }[]
  albums?: { id: string; title?: string; track?: number; disc_number?: number }[]
  version?: number; version_label?: string
  versions?: { id: string; version: number; version_label: string; suffix: string; bit_rate: number; duration: number; library_id: string }[]
}

interface RoleGroup { role: string; label: string; tracks: RawTrack[] }

export default function ArtistDetailPage() {
  const { artistId } = useParams()
  const player = usePlayer()
  const [artist, setArtist] = useState<any>(null)
  const [rawTracks, setRawTracks] = useState<RawTrack[]>([])
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [plOpen, setPlOpen] = useState(false)
  const [playlists, setPlaylists] = useState<any[]>([])

  useEffect(() => {
    if (!artistId) return
    api.data.artist(artistId).then(async d => {
      setArtist(d.artist)
      setRawTracks(d.tracks || [])
      const ids = (d.tracks || []).map((t: any) => t.id)
      if (ids.length > 0) {
        const fav = await api.user.checkFavorites(ids)
        setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
      }
    })
  }, [artistId])

  const roleGroups: RoleGroup[] = useMemo(() => {
    const groups: Record<string, RawTrack[]> = {}
    for (const t of rawTracks) {
      const match = (t.artists || []).find(a => a.artist_id === artistId)
      const role = match?.role || "performer"
      if (!groups[role]) groups[role] = []
      groups[role].push(t)
    }
    // Order: performer first, then alphabetically
    const order = Object.keys(groups).sort((a, b) => {
      if (a === "performer") return -1
      if (b === "performer") return 1
      return a.localeCompare(b)
    })
    return order.map(role => ({ role, label: roleLabels[role] || role, tracks: groups[role] }))
  }, [rawTracks, artistId])

  const allPlayerTracks: PlayerTrack[] = useMemo(() =>
    rawTracks.map(t => ({
      id: t.id, title: t.title,
      duration: t.duration, suffix: t.file_format || "mp3",
      cover_image_id: t.cover_image_id, artists: t.artists,
      albums: t.albums,
      version: t.version, version_label: t.version_label, versions: t.versions,
    })), [rawTracks, artist])

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }
  const playAll = () => { if (allPlayerTracks.length > 0) player.setQueue(allPlayerTracks, 0) }

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
          <p className="text-sm text-zinc-500 mt-1">
            {artist.country ? `${artist.country} · ` : ""}
            {(artist.roles || []).map((r: string) => roleLabels[r] || r).join(" · ") || ""}
            {artist.roles ? " · " : ""}{allPlayerTracks.length} tracks
          </p>
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
            <button onClick={() => player.addToQueue(allPlayerTracks.filter(t => selected.has(t.id)))}
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
                    <button key={p.id} onClick={async () => { await api.user.addTracksToPlaylist(p.id, allPlayerTracks.filter(t => selected.has(t.id)).map(t => t.id)); setPlOpen(false) }}
                      className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">{p.name}</button>
                  ))}
                  {playlists.length === 0 && <p className="text-xs text-zinc-600 px-3 py-2">No playlists</p>}
                </div>
              )}
            </div>
            <button onClick={async () => {
                const sel = allPlayerTracks.filter(t => selected.has(t.id))
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
              <Heart className={`w-4 h-4 ${allPlayerTracks.filter(t => selected.has(t.id)).every(t => favoriteIds.has(t.id)) ? "fill-red-500 text-red-500" : ""}`} />
              {allPlayerTracks.filter(t => selected.has(t.id)).every(t => favoriteIds.has(t.id)) ? "Unfavorite" : "Favorite"}
            </button>
          </div>
        )}
      </div>

      {roleGroups.map(group => {
        const groupTracks: PlayerTrack[] = group.tracks.map(t => ({
          id: t.id, title: t.title,
          duration: t.duration, suffix: t.file_format || "mp3",
          cover_image_id: t.cover_image_id, artists: t.artists,
          albums: t.albums,
          version: t.version, version_label: t.version_label, versions: t.versions,
        }))
        return (
          <div key={group.role} className="space-y-1">
            <h2 className="text-sm font-medium text-zinc-300 px-4 py-2 border-b border-zinc-800">
              {group.label} ({group.tracks.length})
            </h2>
            <div className="flex items-center gap-1 text-xs text-zinc-500 px-4 py-1">
              <div className="flex items-center gap-1 w-1/2 shrink-0">
                <span className="w-10 shrink-0" />
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
            {groupTracks.map((t, i) => {
              const gi = allPlayerTracks.findIndex(p => p.id === t.id)
              return (
                <div key={t.id}
                  className="flex items-center gap-1 px-4 py-0 rounded-lg hover:bg-zinc-800/50 cursor-pointer group">
                  <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
                    <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                      onClick={(e) => { e.stopPropagation(); if (multi) toggleSelect(t.id); else player.setQueue(allPlayerTracks, gi); }}>
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
                    <div className="w-7 shrink-0 justify-end inline-flex items-center" onClick={(e) => { e.stopPropagation(); player.setQueue(allPlayerTracks, gi); }}>
                      <span className={`text-sm ${player.track?.id === t.id ? "text-green-500" : "text-zinc-500"}`}>{i + 1}</span>
                    </div>
                    <span className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${player.track?.id === t.id ? "text-green-500" : ""}`}
                      onClick={() => player.setQueue(allPlayerTracks, gi)}>{t.title}</span>
                  </div>
                  <div className="flex items-center gap-1 flex-1 min-w-0">
                    <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                      <AddQueueBtn track={t} versions={t.versions} />
                      <AddBtn trackId={t.id} />
                      <FavBtn trackId={t.id} initiallyFav={favoriteIds.has(t.id)}
                        onToggle={(id, nowFav) => { setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n }) }} />
                    </span>
                    <span className="flex-1 min-w-0" />
                    <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">{t.albums?.[0]?.id ? <Link to={`/albums/${t.albums[0].id}`} className="text-sm text-zinc-500 truncate hover:text-white transition-colors" onClick={e => e.stopPropagation()}>{t.albums[0].title || ""}</Link> : <span className="text-sm text-zinc-500 truncate">{t.albums?.[0]?.title || ""}</span>}</span>
                    <span className="w-16 shrink-0 text-center text-sm text-zinc-400">{formatDuration(t.duration)}</span>
                  </div>
                </div>
              )
            })}
          </div>
        )
      })}
    </div>
  )
}
