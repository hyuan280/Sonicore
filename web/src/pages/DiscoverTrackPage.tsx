import { useEffect, useState, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { useParams, Link } from "react-router-dom"
import { api } from "../api/client"
import { translateApiError } from "../i18n/errorCodes"
import { Music, ChevronLeft } from "lucide-react"
import { formatDuration, parseLRC } from "../lib/utils"

interface TrackDetail {
  platform: string
  track_id: string
  title: string
  artist: string
  artist_id?: string
  album?: string
  album_id?: string
  duration?: number
  cover_url?: string
  year?: number
  lyrics?: string
}

export default function DiscoverTrackPage() {
  const { t } = useTranslation()
  const { platform, trackId } = useParams()
  const [track, setTrack] = useState<TrackDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [retryKey, setRetryKey] = useState(0)

  useEffect(() => {
    if (!platform || !trackId) return
    let cancelled = false
    setTrack(null)
    setError(null)
    api.platform.track(platform, trackId).then(d => {
      if (!cancelled) setTrack(d)
    }).catch(err => {
      if (!cancelled) setError(translateApiError(t, err))
    })
    return () => { cancelled = true }
  }, [platform, trackId, retryKey])

  const lrcLines = useMemo(() => {
    if (!track?.lyrics) return null
    const lines = parseLRC(track.lyrics)
    return lines.length > 0 ? lines : null
  }, [track])

  if (!platform || !trackId) {
    return (
      <div className="px-6 pb-24 pt-6">
        <Link to="/discover" className="flex items-center gap-1 text-sm text-zinc-400 hover:text-white transition-colors mb-3 w-fit">
          <ChevronLeft className="w-4 h-4" />{t("nav.discover")}
        </Link>
        <p className="text-zinc-400 text-center py-16">{t("discover.loadError")}</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-6 pb-24 pt-6">
        <Link to="/discover" className="flex items-center gap-1 text-sm text-zinc-400 hover:text-white transition-colors mb-3 w-fit">
          <ChevronLeft className="w-4 h-4" />{t("nav.discover")}
        </Link>
        <div className="text-center py-16 space-y-3">
          <p className="text-zinc-400">{error}</p>
          <button onClick={() => setRetryKey(k => k + 1)}
            className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">
            {t("common.retry")}
          </button>
        </div>
      </div>
    )
  }

  if (!track) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-zinc-700 border-t-green-500" />
      </div>
    )
  }

  return (
    <div className="px-6 pb-24">
      <Link to="/discover" className="flex items-center gap-1 text-sm text-zinc-400 hover:text-white transition-colors mb-3 w-fit">
        <ChevronLeft className="w-4 h-4" />{t("nav.discover")}
      </Link>

      <div className="flex gap-6 pb-6">
        <div className="w-48 h-48 rounded-xl bg-zinc-800 flex-shrink-0 flex items-center justify-center overflow-hidden">
          {track.cover_url ? (
            <img src={track.cover_url} alt={track.title}
              className="w-full h-full object-cover"
              onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
          ) : null}
          <Music className={`w-10 h-10 text-zinc-600 ${track.cover_url ? "hidden" : ""}`} />
        </div>
        <div className="flex flex-col justify-end min-w-0">
          <p className="text-xs uppercase tracking-wider text-zinc-400">{platform}</p>
          <h1 className="text-3xl font-bold mt-1">{track.title}</h1>
          <p className="text-sm text-zinc-400 mt-1">
            {track.artist_id ? (
              <Link to={`/discover/artists/${encodeURIComponent(platform ?? "")}/${encodeURIComponent(track.artist_id)}`}
                className="hover:text-white transition-colors">{track.artist}</Link>
            ) : track.artist}
            {track.album ? ` · ${track.album}` : ""}
            {track.duration !== undefined && track.duration !== null ? ` · ${formatDuration(track.duration)}` : ""}
          </p>
          {track.year ? <p className="text-sm text-zinc-500 mt-1">{track.year}</p> : null}
        </div>
      </div>

      {track.lyrics ? (
        <div className="rounded-xl bg-zinc-900 border border-zinc-800 p-5">
          {lrcLines ? (
            <div className="space-y-1.5 max-h-[50vh] overflow-y-auto">
              {lrcLines.map((line, i) => (
                <p key={i} className="text-sm text-zinc-400">{line.text}</p>
              ))}
            </div>
          ) : (
            <pre className="text-sm text-zinc-400 whitespace-pre-wrap font-sans max-h-[50vh] overflow-y-auto">{track.lyrics}</pre>
          )}
        </div>
      ) : (
        <p className="text-zinc-500 text-center py-12">{t("discover.noLyrics")}</p>
      )}
    </div>
  )
}
