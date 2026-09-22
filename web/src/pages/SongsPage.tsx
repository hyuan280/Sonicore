import { useEffect, useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { usePlayer } from "../stores/player";
import { Play, X } from "lucide-react";
import { Button } from "../components/ui/button";
import TrackTable, { type TrackRow } from "../components/TrackTable";
import { usePerPage } from "../hooks/usePerPage";

export default function SongsPage() {
  const player = usePlayer();
  const [tracks, setTracks] = useState<TrackRow[]>([]);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = usePerPage("tracks", 20);
  const { t } = useTranslation();
  const [total, setTotal] = useState(0);
  const [searchQ, setSearchQ] = useState("");
  const [sort, setSort] = useState("");
  const [multi, setMulti] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set());
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  // Guards against out-of-order responses: sort changes fire immediately while
  // a pending searchQ debounce may fire a second request, so an older response
  // must not overwrite a newer one.
  const loadSeqRef = useRef(0);

  const load = useCallback(async () => {
    const seq = ++loadSeqRef.current;
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) });
    if (searchQ.trim()) params.set("q", searchQ.trim());
    if (sort) params.set("sort", sort);
    const r = await fetch(`/api/data/tracks?${params}`, {
      headers: { Authorization: "Bearer " + localStorage.getItem("token") },
    }).then((r) => r.json());
    if (seq !== loadSeqRef.current) return;
    const items: TrackRow[] = (r.items || []).map((t: any) => ({
      id: t.id,
      title: t.title,
      duration: t.duration,
      suffix: t.suffix,
      cover_image_id: t.cover_image_id,
      heat: t.heat,
      artists: t.artists,
      albums: t.albums,
      versions: t.versions,
    }));
    setTracks(items);
    setTotal(r.total || 0);
    if (items.length > 0) {
      const fav = await api.user.checkFavorites(items.map((t) => t.id));
      if (seq !== loadSeqRef.current) return;
      setFavoriteIds(new Set(Object.keys(fav.favorites || {})));
    }
  }, [page, perPage, searchQ, sort]);

  // Keep the newest load() reachable from the debounce timer: its effect only
  // depends on searchQ, so capturing `load` directly would let a pending timer
  // fire with a stale sort/page and overwrite the newer result. Synced in an
  // effect so the ref is not written during render.
  const loadRef = useRef(load);
  useEffect(() => {
    loadRef.current = load;
  }, [load]);

  useEffect(() => {
    loadRef.current();
  }, [page, perPage, sort]);

  useEffect(() => {
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      loadRef.current();
    }, 500);
    return () => clearTimeout(timerRef.current);
  }, [searchQ]);

  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const n = new Set(prev);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  };

  return (
    <div>
      <TrackTable
        tracks={tracks}
        header={
          <div className="relative flex items-center">
            <div className="shrink-0">
              <h1 className="text-2xl font-bold">{t("nav.songs")}</h1>
            </div>
            <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
              <input
                type="text"
                placeholder={t("search.searchSongs")}
                value={searchQ}
                onChange={(e) => {
                  setSearchQ(e.target.value);
                  setPage(1);
                }}
                className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
              />
              {searchQ && (
                <button
                  onClick={() => {
                    setSearchQ("");
                    setPage(1);
                  }}
                  className="absolute right-5 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer"
                >
                  <X className="w-4 h-4" />
                </button>
              )}
            </div>
            <div className="flex-1" />
            <select
              value={sort}
              onChange={(e) => {
                setSort(e.target.value);
                setPage(1);
              }}
              className="shrink-0 mr-2 px-2 py-1.5 text-sm bg-zinc-800 text-zinc-300 border-none outline-none rounded cursor-pointer"
            >
              <option value="">{t("songs.sortDefault")}</option>
              <option value="heat">{t("songs.sortHeat")}</option>
              <option value="plays">{t("songs.sortPlays")}</option>
              <option value="recent">{t("songs.sortRecent")}</option>
            </select>
            <Button
              onClick={() =>
                player.setQueue(
                  tracks.map((t) => ({
                    id: t.id,
                    title: t.title,
                    duration: t.duration,
                    suffix: t.suffix || "mp3",
                    cover_image_id: t.cover_image_id,
                    artists: t.artists,
                    albums: t.albums,
                    versions: t.versions,
                  })),
                  0,
                )
              }
              size="sm"
              className="shrink-0"
            >
              <Play className="w-4 h-4 mr-1" />
              {t("player.playAll")}
            </Button>
          </div>
        }
        onPlay={(i) =>
          player.setQueue(
            tracks.map((t) => ({
              id: t.id,
              title: t.title,
              duration: t.duration,
              suffix: t.suffix || "mp3",
              cover_image_id: t.cover_image_id,
              artists: t.artists,
              albums: t.albums,
              versions: t.versions,
            })),
            i,
          )
        }
        currentTrackId={player.track?.id ?? null}
        favoriteIds={favoriteIds}
        onFavoriteToggle={(id, nowFav) => {
          setFavoriteIds((prev) => {
            const n = new Set(prev);
            nowFav ? n.add(id) : n.delete(id);
            return n;
          });
        }}
        multi={multi}
        selected={selected}
        onMultiToggle={() => {
          setMulti(!multi);
          if (multi) setSelected(new Set());
        }}
        onToggleSelect={toggleSelect}
        page={page}
        perPage={perPage}
        total={total}
        onPageChange={setPage}
        onPerPageChange={(val) => {
          setPerPage(val);
          setPage(1);
        }}
        emptyText={t("trackTable.noSongsFound")}
      />
    </div>
  );
}
