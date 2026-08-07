import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useParams, Link } from "react-router-dom"
import { api } from "../api/client"
import PlatformTrackList, { type PlatformTrackItem } from "../components/PlatformTrackList"
import PageNav from "../components/PageNav"
import { translateApiError } from "../i18n/errorCodes"
import { Music2, ChevronLeft } from "lucide-react"

interface ChartMeta {
  id: string
  name: string
  description?: string
  cover_url?: string
  track_count: number
  update_freq?: string
}

interface ChartResponse {
  tracks?: PlatformTrackItem[]
  total: number
}

export default function DiscoverChartPage() {
  const { t } = useTranslation()
  const { platform, chartId } = useParams()
  const [meta, setMeta] = useState<ChartMeta | null>(null)
  const [tracks, setTracks] = useState<PlatformTrackItem[]>([])
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(30)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [metaError, setMetaError] = useState<string | null>(null)
  const [tracksError, setTracksError] = useState<string | null>(null)
  const [retryKey, setRetryKey] = useState(0)

  useEffect(() => {
    if (!platform || !chartId) return
    let cancelled = false
    setMeta(null)
    setMetaError(null)
    api.platform.charts(platform).then(d => {
      if (cancelled) return
      setMeta(((d.charts || []) as ChartMeta[]).find(c => c.id === chartId) || null)
    }).catch(err => {
      if (!cancelled) setMetaError(translateApiError(t, err))
    })
    return () => { cancelled = true }
  }, [platform, chartId, retryKey])

  useEffect(() => {
    if (!platform || !chartId) return
    let cancelled = false
    setLoading(true)
    setTracksError(null)
    setTracks([])
    api.platform.chart(platform, chartId, page, perPage).then((d: ChartResponse) => {
      if (cancelled) return
      setTracks(d.tracks || [])
      setTotal(d.total || 0)
    }).catch(err => {
      if (!cancelled) { setTracks([]); setTracksError(translateApiError(t, err)) }
    }).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [platform, chartId, page, perPage, retryKey])

  const totalPages = Math.ceil(total / perPage)

  return (
    <div>
      <div className="sticky top-0 z-10 bg-black px-6 pt-6 pb-4">
        <Link to="/discover" className="flex items-center gap-1 text-sm text-zinc-400 hover:text-white transition-colors mb-3 w-fit">
          <ChevronLeft className="w-4 h-4" />{t("nav.discover")}
        </Link>
        <div className="flex items-end gap-6">
          <div className="w-44 h-44 rounded-xl bg-zinc-800 flex-shrink-0 flex items-center justify-center overflow-hidden">
            {meta?.cover_url ? (
              <img src={meta.cover_url} alt={meta.name || ""}
                className="w-full h-full object-cover"
                onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
            ) : null}
            <Music2 className={`w-10 h-10 text-zinc-600 ${meta?.cover_url ? "hidden" : ""}`} />
          </div>
          <div className="flex flex-col justify-end min-w-0">
            <p className="text-xs uppercase tracking-wider text-zinc-400">{platform}</p>
            <h1 className="text-3xl font-bold mt-1 truncate">{meta?.name || ""}</h1>
            {(meta?.update_freq || meta?.description) && (
              <p className="text-sm text-zinc-500 mt-1 line-clamp-2">
                {meta?.update_freq ? `${meta.update_freq} · ` : ""}{meta?.description || ""}
              </p>
            )}
            {meta && <p className="text-sm text-zinc-500 mt-1">{t("album.tracks", { count: meta.track_count || total })}</p>}
          </div>
        </div>
        {metaError && (
          <div className="px-6 pb-3">
            <p className="text-sm text-zinc-500">{metaError}</p>
          </div>
        )}
      </div>

      <div className="px-6 pb-2">
        <PageNav page={page} totalPages={totalPages} total={total} perPage={perPage}
          onPage={setPage} onPerPage={setPerPage} />
      </div>

      <div className="px-6 pb-24">
        {tracksError ? (
          <div className="text-center py-16 space-y-3">
            <p className="text-zinc-400">{tracksError}</p>
            <button onClick={() => setRetryKey(k => k + 1)}
              className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">
              {t("common.retry")}
            </button>
          </div>
        ) : loading ? (
          <div className="flex items-center justify-center py-16">
            <div className="animate-spin rounded-full h-8 w-8 border-2 border-zinc-700 border-t-green-500" />
          </div>
        ) : (
          <PlatformTrackList tracks={tracks} header />
        )}
      </div>
    </div>
  )
}
