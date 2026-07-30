import { Link } from "react-router-dom"

interface TrackArtist {
  artist_id: string
  name?: string
  role?: string
  artist?: { name?: string }
}

interface ArtistLinkProps {
  artists?: TrackArtist[]
  className?: string
}

export default function ArtistLink({ artists, className }: ArtistLinkProps) {
  if (!artists || artists.length === 0) return null

  const performers = artists.filter(a => a.role === "performer")
  const list = performers.length > 0 ? performers : artists

  return (
    <span className={className}>
      {list.map((a, i) => (
        <span key={a.artist_id}>
          {i > 0 && <span className="mx-0.5 text-zinc-600">/</span>}
          <Link
            to={`/artists/${a.artist_id}`}
            className="hover:text-white transition-colors"
            onClick={e => e.stopPropagation()}
          >
            {a.name || a.artist?.name || ""}
          </Link>
        </span>
      ))}
    </span>
  )
}
