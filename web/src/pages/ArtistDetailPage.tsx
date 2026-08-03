import { useEffect, useState, useMemo } from "react"
import { useParams, Link } from "react-router-dom"
import { api } from "../api/client"
import { usePlayer, type PlayerTrack } from "../stores/player"
import { Play, Mic2, Music } from "lucide-react"
import { coverUrl } from "../lib/utils"
import TrackTable, { type TrackRow } from "../components/TrackTable"
import { usePerPage } from "../hooks/usePerPage"

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

export default function ArtistDetailPage() {
  const { artistId } = useParams()
  const player = usePlayer()
  const [artist, setArtist] = useState<any>(null)
  const [rawTracks, setRawTracks] = useState<RawTrack[]>([])
  const [multi, setMulti] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [perPage] = usePerPage("tracks", 20)

  useEffect(() => {
    if (!artistId) return
    api.data.artist(artistId, 1, 100).then(async d => {
      setArtist(d.artist)
      setRawTracks(d.tracks || [])
      const ids = (d.tracks || []).map((t: any) => t.id)
      if (ids.length > 0) {
        const fav = await api.user.checkFavorites(ids)
        setFavoriteIds(new Set(Object.keys(fav.favorites || {})))
      }
    })
  }, [artistId])

  const allPlayerTracks: PlayerTrack[] = useMemo(() =>
    rawTracks.map(t => ({
      id: t.id, title: t.title,
      duration: t.duration, suffix: t.file_format || "mp3",
      cover_image_id: t.cover_image_id, artists: t.artists,
      albums: t.albums,
      version: t.version, version_label: t.version_label, versions: t.versions,
    })), [rawTracks])

  const allRows: TrackRow[] = rawTracks.map(t => ({
    id: t.id, title: t.title, duration: t.duration,
    suffix: t.file_format || "mp3", cover_image_id: t.cover_image_id,
    artists: t.artists, albums: t.albums, versions: t.versions,
  }))

  const toggleSelect = (id: string) => {
    setSelected(prev => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n })
  }

  if (!artist) return <div className="p-6 text-zinc-500">Loading...</div>

  return (
    <div>
      <TrackTable
        tracks={allRows}
        showArtist={false}
        perPage={perPage}
        header={
          <div className="flex gap-6 pb-2">
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
              <button className="mt-4 w-fit px-4 py-2 rounded-lg bg-green-600 text-white text-sm font-medium hover:bg-green-500 cursor-pointer flex items-center gap-2"
                onClick={() => { if (allPlayerTracks.length > 0) player.setQueue(allPlayerTracks, 0) }}>
                <Play className="w-4 h-4" />Play All
              </button>
            </div>
          </div>
        }
        multi={multi}
        selected={selected}
        onMultiToggle={() => { setMulti(!multi); if (multi) setSelected(new Set()) }}
        onToggleSelect={toggleSelect}
        favoriteIds={favoriteIds}
        onFavoriteToggle={(id, nowFav) => {
          setFavoriteIds(prev => { const n = new Set(prev); nowFav ? n.add(id) : n.delete(id); return n })
        }}
        onPlay={(i) => {
          if (i >= 0 && i < allPlayerTracks.length) player.setQueue(allPlayerTracks, i)
        }}
        currentTrackId={player.track?.id ?? null}
        emptyText="No tracks for this artist"
      />
    </div>
  )
}
