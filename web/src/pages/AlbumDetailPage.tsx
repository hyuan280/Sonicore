import { useEffect, useState } from "react"
import { useParams } from "react-router-dom"
import { api } from "../api/client"
import { usePlayer } from "../stores/player"
import { Button } from "../components/ui/button"
import { Play, Disc3, Music } from "lucide-react"
import { coverUrl } from "../lib/utils"
import { Link } from "react-router-dom"
import TrackTable, { type TrackRow } from "../components/TrackTable"
import { usePerPage } from "../hooks/usePerPage"

export default function AlbumDetailPage() {
  const { albumId } = useParams()
  const player = usePlayer()
  const [album, setAlbum] = useState<any>(null)
  const [tracks, setTracks] = useState<TrackRow[]>([])
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = usePerPage("tracks", 20)
  const [total, setTotal] = useState(0)
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())

  useEffect(() => {
    if (!albumId) return
    api.data.album(albumId, page, perPage).then(async d => {
      const items: TrackRow[] = (d.tracks || []).map((t: any) => ({
        id: t.id, title: t.title, duration: t.duration,
        suffix: t.file_format || "mp3", cover_image_id: t.cover_image_id,
        artists: t.artists, albums: t.albums, versions: t.versions,
      }))
      setTracks(items)
      setAlbum(d.album)
      setTotal(d.total || items.length)
      if (items.length > 0) {
        const fav = await api.user.checkFavorites(items.map(t => t.id))
        setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
      }
    })
  }, [albumId, page, perPage])

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }

  const makePlayerTracks = () => tracks.map(t => ({
    id: t.id, title: t.title, duration: t.duration, suffix: t.suffix || "mp3",
    cover_image_id: t.cover_image_id, artists: t.artists, albums: t.albums, versions: t.versions,
  }))

  if (!album) return <div className="p-6 text-zinc-500">Loading...</div>

  return (
    <div>
      <TrackTable
        tracks={tracks}
        showAlbum={false}
        header={
          <div className="flex gap-6 pb-2">
            <div className="w-48 h-48 rounded-xl bg-zinc-800 flex-shrink-0 flex items-center justify-center overflow-hidden">
              {album.cover_image_id ? (
                <img src={coverUrl("album", album.id, 256)} alt={album.title}
                  className="w-full h-full object-cover"
                  onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
              ) : null}
              <Disc3 className={`w-12 h-12 text-zinc-600 ${album.cover_image_id ? "hidden" : ""}`} />
            </div>
            <div className="flex flex-col justify-end">
              <p className="text-xs uppercase tracking-wider text-zinc-400">Album</p>
              <h1 className="text-3xl font-bold mt-1">{album.title}</h1>
              {album.artist && (
                <Link to={`/artists/${album.artist_id}`} className="text-sm text-zinc-300 mt-1 block hover:text-white transition-colors">
                  {album.artist}
                </Link>
              )}
              <p className="text-sm text-zinc-500 mt-1">{album.year || ""}{album.country ? ` · ${album.country}` : ""} · {tracks.length} tracks</p>
              <Button className="mt-4 w-fit" onClick={() => { if (tracks.length > 0) player.setQueue(makePlayerTracks(), 0) }}>
                <Play className="w-4 h-4 mr-2" />Play All
              </Button>
            </div>
          </div>
        }
        onPlay={(i) => player.setQueue(makePlayerTracks(), i)}
        currentTrackId={player.track?.id ?? null}
        favoriteIds={favoriteIds}
        onFavoriteToggle={(id, nowFav) => {
          setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n })
        }}
        multi={multi}
        selected={selected}
        onMultiToggle={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
        onToggleSelect={toggleSelect}
        page={page}
        perPage={perPage}
        total={total}
        onPageChange={setPage}
        onPerPageChange={(val) => { setPerPage(val); setPage(1) }}
        emptyText="No tracks in this album"
      />
    </div>
  )
}
