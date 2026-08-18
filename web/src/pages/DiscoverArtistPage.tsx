import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams, Link } from "react-router-dom";
import { api } from "../api/client";
import PlatformTrackList, { type PlatformTrackItem } from "../components/PlatformTrackList";
import PageNav from "../components/PageNav";
import { translateApiError } from "../i18n/errorCodes";
import { Mic2, ChevronLeft, ChevronDown } from "lucide-react";

interface ArtistDetail {
  artist_id: string;
  name: string;
  cover_url?: string;
  album_count: number;
  track_count: number;
  brief_desc?: string;
}

export default function DiscoverArtistPage() {
  const { t } = useTranslation();
  const { platform, artistId } = useParams();
  const [artist, setArtist] = useState<ArtistDetail | null>(null);
  const [tracks, setTracks] = useState<PlatformTrackItem[]>([]);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(30);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [artistError, setArtistError] = useState<string | null>(null);
  const [tracksError, setTracksError] = useState<string | null>(null);
  const [retryKey, setRetryKey] = useState(0);
  const [descOpen, setDescOpen] = useState(false);

  useEffect(() => {
    if (!platform || !artistId) return;
    let cancelled = false;
    setArtist(null);
    setArtistError(null);
    api.platform
      .artist(platform, artistId)
      .then((d) => {
        if (!cancelled) setArtist(d);
      })
      .catch((err) => {
        if (!cancelled) setArtistError(translateApiError(t, err));
      });
    return () => {
      cancelled = true;
    };
  }, [platform, artistId, retryKey]);

  useEffect(() => {
    if (!platform || !artistId) return;
    let cancelled = false;
    setLoading(true);
    setTracksError(null);
    api.platform
      .artistTracks(platform, artistId, page, perPage)
      .then((d) => {
        if (cancelled) return;
        setTracks(d.tracks || []);
        setTotal(d.total || 0);
      })
      .catch((err) => {
        if (!cancelled) {
          setTracks([]);
          setTracksError(translateApiError(t, err));
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [platform, artistId, page, perPage, retryKey]);

  const totalPages = Math.ceil(total / perPage);
  const brief = artist?.brief_desc || "";

  return (
    <div>
      <div className="sticky top-0 z-10 bg-black px-6 pt-6 pb-4">
        <Link
          to="/discover"
          className="flex items-center gap-1 text-sm text-zinc-400 hover:text-white transition-colors mb-3 w-fit"
        >
          <ChevronLeft className="w-4 h-4" />
          {t("nav.discover")}
        </Link>
        {artist && (
          <div className="flex gap-6">
            <div className="w-48 h-48 rounded-full bg-zinc-800 flex-shrink-0 flex items-center justify-center overflow-hidden">
              {artist.cover_url ? (
                <img
                  src={artist.cover_url}
                  alt={artist.name}
                  className="w-full h-full object-cover"
                  onError={(e) => {
                    (e.target as HTMLImageElement).style.display = "none";
                    (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden");
                  }}
                />
              ) : null}
              <Mic2 className={`w-12 h-12 text-zinc-500 ${artist.cover_url ? "hidden" : ""}`} />
            </div>
            <div className="flex flex-col justify-end min-w-0">
              <p className="text-xs uppercase tracking-wider text-zinc-400">{platform}</p>
              <h1 className="text-3xl font-bold mt-1">{artist.name}</h1>
              <p className="text-sm text-zinc-500 mt-1">
                {artist.album_count > 0
                  ? `${t("album.totalAlbums", { count: artist.album_count })} · `
                  : ""}
                {artist.track_count > 0
                  ? `${t("artist.tracks", { count: artist.track_count })}`
                  : ""}
              </p>
              {brief && (
                <div className="mt-2 max-w-2xl">
                  <p
                    className={`text-sm text-zinc-500 whitespace-pre-line ${descOpen ? "" : "line-clamp-2"}`}
                  >
                    {brief}
                  </p>
                  <button
                    onClick={() => setDescOpen(!descOpen)}
                    className="mt-1 flex items-center gap-1 text-xs text-zinc-400 hover:text-white transition-colors cursor-pointer"
                  >
                    {descOpen ? t("discover.collapse") : t("discover.expand")}
                    <ChevronDown
                      className={`w-3 h-3 transition-transform ${descOpen ? "rotate-180" : ""}`}
                    />
                  </button>
                </div>
              )}
            </div>
          </div>
        )}
        {artistError && (
          <div className="px-6 pb-3">
            <p className="text-sm text-zinc-500">{artistError}</p>
          </div>
        )}
      </div>

      <div className="px-6 pb-2">
        <PageNav
          page={page}
          totalPages={totalPages}
          total={total}
          perPage={perPage}
          onPage={setPage}
          onPerPage={setPerPage}
        />
      </div>

      <div className="px-6 pb-24">
        {tracksError ? (
          <div className="text-center py-16 space-y-3">
            <p className="text-zinc-400">{tracksError}</p>
            <button
              onClick={() => setRetryKey((k) => k + 1)}
              className="px-4 py-2 rounded-lg bg-zinc-800 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer"
            >
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
  );
}
