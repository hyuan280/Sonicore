import { useTranslation } from "react-i18next"
import { Link } from "react-router-dom"
import { Music, Clock } from "lucide-react"
import { formatDuration } from "../lib/utils"

export interface PlatformTrackItem {
  platform: string
  track_id: string
  title: string
  artist: string
  artist_id?: string
  album?: string
  album_id?: string
  duration?: number
  cover_url?: string
}

interface PlatformTrackListProps {
  tracks: PlatformTrackItem[]
  header?: React.ReactNode
  onSelect?: (track: PlatformTrackItem, index: number) => void
  emptyText?: string
}

export default function PlatformTrackList({ tracks, header, onSelect, emptyText }: PlatformTrackListProps) {
  const { t } = useTranslation()
  return (
    <div>
      {header && (
        <div className="flex items-center gap-1 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          <div className="flex items-center gap-1 w-1/2 shrink-0">
            <span className="w-7 text-right shrink-0">{t("trackTable.number")}</span>
            <span className="flex-1 min-w-0 ml-3">{t("trackTable.title")}</span>
          </div>
          <div className="flex items-center gap-1 flex-1">
            <span className="w-24 shrink-0 text-center hidden sm:block">{t("trackTable.artist")}</span>
            <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">{t("trackTable.album")}</span>
            <span className="w-16 shrink-0 text-center"><Clock className="w-3 h-3 inline" /></span>
          </div>
        </div>
      )}
      <div className="space-y-1">
        {tracks.map((tr, i) => (
          <div key={`${tr.platform}-${tr.track_id}-${i}`}
            role={onSelect ? "button" : undefined}
            tabIndex={onSelect ? 0 : undefined}
            onClick={() => onSelect?.(tr, i)}
            onKeyDown={e => {
              if (onSelect && (e.key === "Enter" || e.key === " ")) {
                e.preventDefault()
                onSelect(tr, i)
              }
            }}
            className={`flex items-center gap-1 px-4 py-0 rounded-lg group transition-colors ${onSelect ? "cursor-pointer" : ""} hover:bg-zinc-800/50`}>
            <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
              <div className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative">
                {tr.cover_url ? (
                  <img src={tr.cover_url} alt="" loading="lazy"
                    className="w-full h-full object-cover"
                    onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                ) : null}
                <Music className={`w-3.5 h-3.5 text-zinc-600 ${tr.cover_url ? "hidden" : ""}`} />
              </div>
              <div className="w-7 shrink-0 justify-end inline-flex items-center">
                <span className="text-sm text-zinc-500">{i + 1}</span>
              </div>
              <Link to={`/discover/tracks/${encodeURIComponent(tr.platform)}/${encodeURIComponent(tr.track_id)}`}
                onClick={e => e.stopPropagation()}
                className="flex-1 min-w-[200px] text-sm truncate ml-3 hover:text-green-500 transition-colors">
                {tr.title}
              </Link>
            </div>
            <div className="flex items-center gap-1 flex-1 min-w-0">
              <span className="w-24 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block">
                {tr.artist_id ? (
                  <Link to={`/discover/artists/${encodeURIComponent(tr.platform)}/${encodeURIComponent(tr.artist_id)}`}
                    onClick={e => e.stopPropagation()}
                    className="hover:text-white transition-colors">{tr.artist}</Link>
                ) : tr.artist}
              </span>
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">
                <span className="text-sm text-zinc-500 truncate">{tr.album || ""}</span>
              </span>
              <span className="w-16 shrink-0 text-center text-sm text-zinc-400">
                {tr.duration !== undefined && tr.duration !== null ? formatDuration(tr.duration) : ""}
              </span>
            </div>
          </div>
        ))}
        {tracks.length === 0 && <p className="text-zinc-500 text-center py-12">{emptyText || t("trackTable.noTracksFound")}</p>}
      </div>
    </div>
  )
}
