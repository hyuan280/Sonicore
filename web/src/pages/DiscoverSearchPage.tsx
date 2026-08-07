import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { useParams, useSearchParams, Link } from "react-router-dom"
import { api } from "../api/client"
import PlatformTrackList, { type PlatformTrackItem } from "../components/PlatformTrackList"
import PageNav from "../components/PageNav"
import { Card } from "../components/ui/card"
import { translateApiError } from "../i18n/errorCodes"
import { Search, Mic2, X } from "lucide-react"

interface ArtistItem {
  platform: string
  artist_id: string
  name: string
  cover_url?: string
}

interface SearchResponse {
  tracks?: PlatformTrackItem[]
  artists?: ArtistItem[]
  total?: number
}

export default function DiscoverSearchPage() {
  const { t } = useTranslation()
  const { platform } = useParams()
  const [params, setParams] = useSearchParams()
  const q = params.get("q") || ""
  const [query, setQuery] = useState(q)
  const [type, setType] = useState<"track" | "artist">("track")
  const [tracks, setTracks] = useState<PlatformTrackItem[]>([])
  const [artists, setArtists] = useState<ArtistItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(30)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [retryKey, setRetryKey] = useState(0)

  const submitSearch = (val?: string) => {
    const s = (val ?? query).trim()
    if (!s || !platform) return
    setPage(1)
    setParams({ q: s })
  }

  const clearSearch = () => {
    setQuery("")
    setParams({})
    setTracks([])
    setArtists([])
    setTotal(0)
    setError(null)
    setLoading(false)
  }

  useEffect(() => {
    setQuery(q)
    setPage(1)
  }, [q])

  useEffect(() => {
    if (!platform || !q) { setLoading(false); return }
    let cancelled = false
    setLoading(true)
    setError(null)
    const p: Promise<SearchResponse> = type === "track"
      ? api.platform.search(platform, q, "track", page, perPage)
      : api.platform.search(platform, q, "artist", page, perPage)
    p.then(d => {
      if (cancelled) return
      if (type === "track") {
        setTracks(d.tracks || [])
      } else {
        setArtists(d.artists || [])
      }
      setTotal(d.total || 0)
    }).catch(err => {
      if (!cancelled) {
        if (type === "track") setTracks([])
        else setArtists([])
        setTotal(0)
        setError(translateApiError(t, err))
      }
    }).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [platform, q, type, page, perPage, retryKey])

  const totalPages = Math.ceil(total / perPage)

  return (
    <div className="px-6 pb-24">
      <div className="sticky top-0 z-10 bg-black pt-6 pb-4 space-y-3">
        <div className="relative max-w-xl mx-auto">
          <input
            type="text"
            value={query}
            autoFocus
            placeholder={t("discover.searchPlaceholder")}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={e => { if (e.key === "Enter") submitSearch() }}
            className="w-full px-4 py-2.5 pr-16 text-base bg-zinc-800 text-zinc-200 border-none outline-none placeholder-zinc-500"
          />
          {query && (
            <button onClick={clearSearch} aria-label={t("discover.clearSearch")}
              className="absolute right-10 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer">
              <X className="w-4 h-4" />
            </button>
          )}
          <button onClick={() => submitSearch()} aria-label={t("discover.search")}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-2 text-zinc-400 hover:text-green-500 cursor-pointer">
            <Search className="w-5 h-5" />
          </button>
        </div>
        {q && (
          <div className="flex items-center gap-2">
            <button onClick={() => { setType("track"); setPage(1) }}
              className={`px-3 py-1.5 rounded-lg text-sm cursor-pointer transition-colors ${type === "track" ? "bg-green-600/20 text-green-500" : "bg-zinc-800 text-zinc-400 hover:text-white"}`}>
              {t("search.track")}
            </button>
            <button onClick={() => { setType("artist"); setPage(1) }}
              className={`px-3 py-1.5 rounded-lg text-sm cursor-pointer transition-colors ${type === "artist" ? "bg-green-600/20 text-green-500" : "bg-zinc-800 text-zinc-400 hover:text-white"}`}>
              {t("search.artist")}
            </button>
            <div className="ml-auto">
              <PageNav page={page} totalPages={totalPages} total={total} perPage={perPage}
                onPage={setPage} onPerPage={setPerPage} />
            </div>
          </div>
        )}
      </div>

      {!q && (
        <p className="text-zinc-500 text-center py-24">{t("discover.searchHint")}</p>
      )}

      {q && error && (
        <div className="text-center py-16 space-y-3">
          <p className="text-zinc-400">{error}</p>
          <button onClick={() => { setError(null); setRetryKey(k => k + 1) }}
            className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer">
            {t("common.retry")}
          </button>
        </div>
      )}

      {q && !error && loading && (
        <div className="flex items-center justify-center py-16">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-zinc-700 border-t-green-500" />
        </div>
      )}

      {q && !error && !loading && type === "track" && (
        <PlatformTrackList tracks={tracks} header />
      )}

      {q && !error && !loading && type === "artist" && (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
          {artists.map(a => (
            <Link key={a.artist_id} to={`/discover/artists/${platform}/${a.artist_id}`} className="block">
              <Card className="hover:bg-zinc-800/50 transition-colors h-full p-0 overflow-hidden">
                <div className="aspect-square flex items-center justify-center overflow-hidden bg-zinc-800">
                  {a.cover_url ? (
                    <img src={a.cover_url} alt={a.name} loading="lazy"
                      className="w-full h-full object-cover"
                      onError={e => { (e.target as HTMLImageElement).style.display = "none"; (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden") }} />
                  ) : null}
                  <Mic2 className={`w-8 h-8 text-zinc-600 ${a.cover_url ? "hidden" : ""}`} />
                </div>
                <div className="p-3">
                  <p className="font-medium text-sm truncate text-center">{a.name}</p>
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}

      {q && !error && !loading && ((type === "track" && tracks.length === 0) || (type === "artist" && artists.length === 0)) && (
        <p className="text-zinc-500 text-center py-12">{t("search.noResults")}</p>
      )}
    </div>
  )
}
