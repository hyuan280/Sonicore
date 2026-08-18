import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Play,
  Clock,
  CheckSquare,
  Check,
  Plus,
  ListPlus,
  Heart,
  Music,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
} from "lucide-react";
import { Link } from "react-router-dom";
import { usePlayer } from "../stores/player";
import { api } from "../api/client";
import { AddBtn, FavBtn, AddQueueBtn } from "./AddToPlaylist";
import ArtistLink from "./ArtistLink";
import { formatDuration, coverImageUrl } from "../lib/utils";

export interface TrackRow {
  id: string;
  trackId?: string;
  title: string;
  duration: number;
  suffix?: string;
  cover_image_id?: string;
  artists?: { artist_id: string; name: string; role: string }[];
  albums?: { id: string; title?: string }[];
  versions?: {
    id: string;
    version: number;
    version_label: string;
    suffix: string;
    bit_rate: number;
    duration: number;
    library_id: string;
  }[];
}

interface TrackTableProps {
  tracks: TrackRow[];
  header?: React.ReactNode;
  showArtist?: boolean;
  showAlbum?: boolean;
  showDuration?: boolean;
  onPlay: (index: number) => void;
  currentTrackId?: string | null;
  favoriteIds: Set<string>;
  onFavoriteToggle?: (id: string, nowFav: boolean) => void;
  multi: boolean;
  selected: Set<string>;
  onMultiToggle: () => void;
  onToggleSelect: (id: string) => void;
  bulkQueue?: boolean;
  bulkPlaylist?: boolean;
  bulkFavorite?: boolean;
  bulkBar?: boolean;
  extraBulkActions?: React.ReactNode;
  playlistFilter?: (pl: any) => boolean;
  page?: number;
  perPage?: number;
  total?: number;
  onPageChange?: (page: number) => void;
  onPerPageChange?: (perPage: number) => void;
  extraColumn?: (track: TrackRow) => React.ReactNode;
  extraColumnHeader?: string;
  extraAction?: (track: TrackRow, index: number) => React.ReactNode;
  emptyText?: string;
  onBulkChange?: () => void;
}

export default function TrackTable({
  tracks,
  header,
  showArtist = true,
  showAlbum = true,
  showDuration = true,
  onPlay,
  currentTrackId,
  favoriteIds,
  onFavoriteToggle,
  multi,
  selected,
  onMultiToggle,
  onToggleSelect,
  bulkQueue = true,
  bulkPlaylist = true,
  bulkFavorite = true,
  bulkBar = true,
  extraBulkActions,
  playlistFilter,
  page: extPage,
  perPage: extPerPage = 30,
  total: extTotal,
  onPageChange,
  onPerPageChange,
  extraColumn,
  extraColumnHeader,
  extraAction,
  emptyText,
  onBulkChange,
}: TrackTableProps) {
  const { t } = useTranslation();
  const player = usePlayer();
  const [plOpen, setPlOpen] = useState(false);
  const [playlists, setPlaylists] = useState<any[]>([]);
  const [internalPage, setInternalPage] = useState(1);
  const [internalPerPage, setInternalPerPage] = useState(extPerPage);
  const [pageEditing, setPageEditing] = useState(false);
  const [perPageOpen, setPerPageOpen] = useState(false);
  const [editValue, setEditValue] = useState("");

  const serverPaged = onPageChange != null;
  const page = serverPaged ? (extPage ?? 1) : internalPage;
  const perPage = serverPaged ? (extPerPage ?? 30) : internalPerPage;
  const total = extTotal != null && extTotal > 0 ? extTotal : tracks.length;
  const totalPages = Math.ceil(total / perPage);

  const displayedTracks = serverPaged
    ? tracks
    : totalPages > 1
      ? tracks.slice((page - 1) * perPage, page * perPage)
      : tracks;

  const displayOffset = serverPaged
    ? (page - 1) * perPage
    : totalPages > 1
      ? (page - 1) * perPage
      : 0;

  const handlePageChange = (p: number) => {
    if (serverPaged) {
      onPageChange?.(p);
    } else {
      setInternalPage(p);
    }
  };
  const handlePerPageChange = (val: number) => {
    if (!serverPaged) {
      setInternalPerPage(val);
      setInternalPage(1);
    }
    onPerPageChange?.(val);
  };

  const getTrackIds = (): string[] =>
    tracks
      .filter((_, i) => {
        const idx = displayOffset + i;
        return idx >= (page - 1) * perPage && idx < page * perPage;
      })
      .map((t) => t.trackId || t.id);

  const selIds = (): string[] =>
    displayedTracks.filter((t) => selected.has(t.id)).map((t) => t.trackId || t.id);

  const handleBulkQueue = useCallback(() => {
    player.addToQueue(
      displayedTracks
        .filter((t) => selected.has(t.id))
        .map((t) => ({
          id: t.trackId || t.id,
          title: t.title,
          duration: t.duration,
          suffix: t.suffix || "mp3",
          cover_image_id: t.cover_image_id,
          artists: t.artists,
          albums: t.albums,
          versions: t.versions,
        })),
    );
  }, [player, displayedTracks, selected]);

  const handleBulkPlaylist = useCallback(
    async (plId: string) => {
      await api.user.addTracksToPlaylist(plId, selIds());
      setPlOpen(false);
      onBulkChange?.();
    },
    [selIds, onBulkChange],
  );

  const handleBulkFavorite = useCallback(async () => {
    const ids = selIds();
    const allFav = ids.every((id) => favoriteIds.has(id));
    if (allFav) {
      await api.user.removeFavorites("track", ids);
      ids.forEach((id) => onFavoriteToggle?.(id, false));
    } else {
      await api.user.addFavorites("track", ids);
      ids.forEach((id) => onFavoriteToggle?.(id, true));
    }
    onBulkChange?.();
  }, [selIds, favoriteIds, onFavoriteToggle, onBulkChange]);

  const openPlaylistDropdown = useCallback(async () => {
    const d = await api.user.playlists();
    let items = d.items || [];
    if (playlistFilter) items = items.filter(playlistFilter);
    setPlaylists(items);
    setPlOpen(!plOpen);
  }, [plOpen, playlistFilter]);

  const selectAll = useCallback(() => {
    if (selected.size === displayedTracks.length && displayedTracks.length > 0) {
      displayedTracks.forEach((t) => {
        if (selected.has(t.id)) onToggleSelect(t.id);
      });
    } else {
      displayedTracks.forEach((t) => {
        if (!selected.has(t.id)) onToggleSelect(t.id);
      });
    }
  }, [selected, displayedTracks, onToggleSelect]);

  const commitPage = useCallback(
    (val: string) => {
      const v = parseInt(val);
      if (v >= 1 && v <= totalPages) handlePageChange(v);
      setPageEditing(false);
    },
    [totalPages, handlePageChange],
  );

  const startEdit = () => {
    setEditValue("");
    setPageEditing(true);
  };

  return (
    <div>
      <div className="sticky top-0 z-10 bg-black pb-2 space-y-2 px-6 pt-6">
        {header && <div>{header}</div>}
        {bulkBar ? (
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <button
                onClick={onMultiToggle}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm cursor-pointer transition-colors ${multi ? "bg-green-600/20 text-green-500" : "bg-zinc-800 text-zinc-400 hover:text-white"}`}
              >
                <CheckSquare className="w-4 h-4" />
                {multi && selected.size > 0
                  ? t("trackTable.selected", { count: selected.size })
                  : t("trackTable.select")}
              </button>
              {multi && selected.size > 0 && (
                <div className="flex items-center gap-2">
                  {bulkQueue && (
                    <button
                      onClick={handleBulkQueue}
                      className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer"
                    >
                      <Plus className="w-4 h-4" /> {t("trackTable.queue")}
                    </button>
                  )}
                  {bulkPlaylist && (
                    <div className="relative">
                      <button
                        onClick={openPlaylistDropdown}
                        className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer"
                      >
                        <ListPlus className="w-4 h-4" /> {t("trackTable.playlist")}
                      </button>
                      {plOpen && (
                        <div
                          className="absolute left-0 top-8 w-48 bg-zinc-800 border border-zinc-700 rounded-xl shadow-xl z-50 py-1 max-h-48 overflow-y-auto"
                          onClick={(e) => e.stopPropagation()}
                        >
                          <p className="text-xs text-zinc-500 px-3 py-1.5">
                            {t("trackTable.addToPlaylist")}
                          </p>
                          {playlists.map((p: any) => {
                            const sids = selIds();
                            const allIn =
                              sids.length > 0 &&
                              sids.every(
                                (id: string) =>
                                  Array.isArray(p.track_ids) && p.track_ids.includes(id),
                              );
                            return (
                              <button
                                key={p.id}
                                onClick={() => handleBulkPlaylist(p.id)}
                                className="w-full text-left px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-700 cursor-pointer flex items-center gap-2"
                              >
                                {allIn ? (
                                  <Check className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
                                ) : (
                                  <span className="w-3.5 flex-shrink-0" />
                                )}
                                <span className="flex-1 truncate">{p.name}</span>
                                {allIn && (
                                  <span className="text-xs text-zinc-600 ml-auto">
                                    {t("trackTable.added")}
                                  </span>
                                )}
                              </button>
                            );
                          })}
                          {playlists.length === 0 && (
                            <p className="text-xs text-zinc-600 px-3 py-2">
                              {t("trackTable.noPlaylists")}
                            </p>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                  {bulkFavorite && (
                    <button
                      onClick={handleBulkFavorite}
                      className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 cursor-pointer"
                    >
                      <Heart
                        className={`w-4 h-4 ${selIds().every((id) => favoriteIds.has(id)) ? "fill-current" : ""}`}
                      />
                      {selIds().every((id) => favoriteIds.has(id))
                        ? t("trackTable.unfavorite")
                        : t("trackTable.favorite")}
                    </button>
                  )}
                  {extraBulkActions}
                </div>
              )}
            </div>
            {
              <div className="flex items-center gap-2 shrink-0">
                <span className="text-sm text-zinc-400">
                  {t("trackTable.tracks", { count: total })}
                </span>
                <div className="flex items-center bg-zinc-800 rounded-lg">
                  <div className="relative">
                    <button
                      onClick={() => setPerPageOpen(!perPageOpen)}
                      className="px-3 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-l-lg cursor-pointer transition-colors"
                    >
                      {perPage} <ChevronDown className="w-4 h-4 inline-block -m-0.5" />
                    </button>
                    {perPageOpen && (
                      <div
                        className="absolute top-full left-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1"
                        onClick={() => setPerPageOpen(false)}
                      >
                        {[10, 20, 50].map((n) => (
                          <button
                            key={n}
                            onClick={() => handlePerPageChange(n)}
                            className={`w-full text-left px-3 py-1.5 text-sm cursor-pointer ${perPage === n ? "text-white" : "text-zinc-400 hover:text-white"}`}
                          >
                            {n}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                  <span className="w-px h-4 bg-zinc-700" />
                  <div className="w-24 flex items-center justify-center relative shrink-0">
                    {pageEditing ? (
                      <>
                        <input
                          type="text"
                          inputMode="numeric"
                          value={editValue}
                          onChange={(e) => setEditValue(e.target.value)}
                          onBlur={() => commitPage(editValue)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") commitPage(editValue);
                          }}
                          autoFocus
                          placeholder={`/ ${totalPages || 1}`}
                          className="w-full text-center py-2 text-sm bg-transparent text-zinc-400 border-none outline-none"
                        />
                        {totalPages > 1 && (
                          <div className="absolute left-0 top-full mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 max-h-48 overflow-y-auto min-w-[3rem]">
                            {Array.from({ length: totalPages }, (_, i) => i + 1).map((n) => (
                              <button
                                key={n}
                                onMouseDown={() => {
                                  handlePageChange(n);
                                  setPageEditing(false);
                                }}
                                className={`w-full text-center px-3 py-1.5 text-sm cursor-pointer ${n === page ? "text-white" : "text-zinc-400 hover:text-white"}`}
                              >
                                {n}
                              </button>
                            ))}
                          </div>
                        )}
                      </>
                    ) : totalPages > 1 ? (
                      <span
                        onClick={startEdit}
                        className="text-sm text-zinc-400 cursor-pointer hover:text-white hover:bg-zinc-700 w-full text-center py-2 transition-colors"
                      >
                        {page} / {totalPages}
                      </span>
                    ) : (
                      <span className="text-sm text-zinc-400 w-full text-center py-2">1 / 1</span>
                    )}
                  </div>
                  <span className="w-px h-4 bg-zinc-700" />
                  <button
                    disabled={page <= 1}
                    onClick={() => handlePageChange(page - 1)}
                    className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]"
                  >
                    <ChevronLeft className="w-4 h-4" />
                    {t("trackTable.prev")}
                  </button>
                  <span className="w-px h-4 bg-zinc-700" />
                  <button
                    disabled={page >= totalPages}
                    onClick={() => handlePageChange(page + 1)}
                    className="flex items-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-r-lg disabled:opacity-30 min-w-[3.5rem] justify-center disabled:hover:bg-transparent cursor-pointer transition-colors"
                  >
                    {t("trackTable.next")}
                    <ChevronRight className="w-4 h-4" />
                  </button>
                </div>
              </div>
            }
          </div>
        ) : (
          <div className="flex items-center justify-end gap-2">
            <span className="text-sm text-zinc-400">{total} tracks</span>
            <div className="flex items-center bg-zinc-800 rounded-lg">
              <div className="relative">
                <button
                  onClick={() => setPerPageOpen(!perPageOpen)}
                  className="px-3 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-l-lg cursor-pointer transition-colors"
                >
                  {perPage}
                </button>
                {perPageOpen && (
                  <div
                    className="absolute top-full left-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1"
                    onClick={() => setPerPageOpen(false)}
                  >
                    {[10, 20, 50].map((n) => (
                      <button
                        key={n}
                        onClick={() => handlePerPageChange(n)}
                        className={`w-full text-left px-3 py-1.5 text-sm cursor-pointer ${perPage === n ? "text-white" : "text-zinc-400 hover:text-white"}`}
                      >
                        {n}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <span className="w-px h-4 bg-zinc-700" />
              <div className="w-24 flex items-center justify-center relative shrink-0">
                {pageEditing ? (
                  <>
                    <input
                      type="text"
                      inputMode="numeric"
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      onBlur={() => commitPage(editValue)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") commitPage(editValue);
                      }}
                      autoFocus
                      placeholder={`/ ${totalPages || 1}`}
                      className="w-full text-center py-2 text-sm bg-transparent text-zinc-400 border-none outline-none"
                    />
                    {totalPages > 1 && (
                      <div className="absolute left-0 top-full mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 max-h-48 overflow-y-auto min-w-[3rem]">
                        {Array.from({ length: totalPages }, (_, i) => i + 1).map((n) => (
                          <button
                            key={n}
                            onMouseDown={() => {
                              handlePageChange(n);
                              setPageEditing(false);
                            }}
                            className={`w-full text-center px-3 py-1.5 text-sm cursor-pointer ${n === page ? "text-white" : "text-zinc-400 hover:text-white"}`}
                          >
                            {n}
                          </button>
                        ))}
                      </div>
                    )}
                  </>
                ) : totalPages > 1 ? (
                  <span
                    onClick={startEdit}
                    className="text-sm text-zinc-400 cursor-pointer hover:text-white hover:bg-zinc-700 w-full text-center py-2 transition-colors"
                  >
                    {page} / {totalPages}
                  </span>
                ) : (
                  <span className="text-sm text-zinc-400 w-full text-center py-2">1 / 1</span>
                )}
              </div>
              <span className="w-px h-4 bg-zinc-700" />
              <button
                disabled={page <= 1}
                onClick={() => handlePageChange(page - 1)}
                className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]"
              >
                <ChevronLeft className="w-4 h-4" />
                {t("trackTable.prev")}
              </button>
              <span className="w-px h-4 bg-zinc-700" />
              <button
                disabled={page >= totalPages}
                onClick={() => handlePageChange(page + 1)}
                className="flex items-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-r-lg disabled:opacity-30 min-w-[3.5rem] justify-center disabled:hover:bg-transparent cursor-pointer transition-colors"
              >
                {t("trackTable.next")}
                <ChevronRight className="w-4 h-4" />
              </button>
            </div>
          </div>
        )}
        <div className="flex items-center gap-1 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
          <div className="flex items-center gap-1 w-1/2 shrink-0">
            {multi ? (
              <label
                className="flex items-center justify-center cursor-pointer shrink-0 w-10"
                onClick={selectAll}
              >
                <input
                  type="checkbox"
                  checked={selected.size === displayedTracks.length && displayedTracks.length > 0}
                  onChange={() => {}}
                  className="accent-green-500 cursor-pointer"
                />
              </label>
            ) : (
              <span className="w-10 shrink-0" />
            )}
            <span className="w-7 text-right shrink-0">{t("trackTable.number")}</span>
            <span className="flex-1 min-w-0 ml-3">{t("trackTable.title")}</span>
          </div>
          <div className="flex items-center gap-1 flex-1">
            <span className="w-20 shrink-0" />
            <span className="flex-1 min-w-0" />
            {showArtist && (
              <span className="w-24 shrink-0 text-center hidden sm:block">
                {t("trackTable.artist")}
              </span>
            )}
            {showAlbum && (
              <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">
                {t("trackTable.album")}
              </span>
            )}
            {showDuration && (
              <span className="w-16 shrink-0 text-center">
                <Clock className="w-3 h-3 inline" />
              </span>
            )}
            {extraColumnHeader && (
              <span className="min-w-[80px] max-w-[140px] shrink-0 text-center hidden sm:block">
                {extraColumnHeader}
              </span>
            )}
            {extraAction && <span className="w-10" />}
          </div>
        </div>
      </div>

      <div className="space-y-1 px-6 pb-24">
        {displayedTracks.map((t, i) => {
          const tId = t.trackId || t.id;
          const isCurrent = currentTrackId === tId;
          const displayIdx = displayOffset + i;
          return (
            <div
              key={t.id}
              className={`flex items-center gap-1 px-4 py-0 rounded-lg group transition-colors ${isCurrent ? "bg-green-600/10" : "hover:bg-zinc-800/50"}`}
            >
              <div className="flex items-center gap-1 w-1/2 min-w-0 shrink-0">
                <div
                  className="w-10 h-10 rounded shrink-0 bg-zinc-800 flex items-center justify-center overflow-hidden relative group cursor-pointer"
                  onClick={(e) => {
                    e.stopPropagation();
                    if (multi) onToggleSelect(t.id);
                    else onPlay(i);
                  }}
                >
                  {t.cover_image_id ? (
                    <img
                      src={coverImageUrl(t.cover_image_id, 64)}
                      alt=""
                      className={`w-full h-full object-cover ${multi && selected.has(t.id) ? "opacity-60" : ""}`}
                      onError={(e) => {
                        (e.target as HTMLImageElement).style.display = "none";
                        (e.target as HTMLImageElement).nextElementSibling?.classList.remove(
                          "hidden",
                        );
                      }}
                    />
                  ) : null}
                  <Music
                    className={`w-3.5 h-3.5 text-zinc-600 ${t.cover_image_id ? "hidden" : ""}`}
                  />
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
                <div
                  className="w-7 shrink-0 justify-end inline-flex items-center"
                  onClick={(e) => {
                    e.stopPropagation();
                    onPlay(i);
                  }}
                >
                  <span className={`text-sm ${isCurrent ? "text-green-500" : "text-zinc-500"}`}>
                    {displayIdx + 1}
                  </span>
                </div>
                <span
                  className={`flex-1 min-w-[200px] text-sm truncate ml-3 cursor-pointer ${isCurrent ? "text-green-500" : ""}`}
                  onClick={() => onPlay(i)}
                >
                  {t.title}
                </span>
              </div>
              <div className="flex items-center gap-1 flex-1 min-w-0">
                <span className="w-20 shrink-0 flex items-center justify-end gap-0.5">
                  <AddQueueBtn
                    track={{
                      id: tId,
                      title: t.title,
                      duration: t.duration,
                      suffix: t.suffix,
                      cover_image_id: t.cover_image_id,
                      artists: t.artists,
                      albums: t.albums,
                    }}
                    versions={t.versions}
                  />
                  <AddBtn trackId={tId} />
                  <FavBtn
                    trackId={tId}
                    initiallyFav={favoriteIds.has(tId)}
                    onToggle={onFavoriteToggle}
                  />
                </span>
                <span className="flex-1 min-w-0" />
                {showArtist && (
                  <span className="w-24 shrink-0 text-sm text-zinc-400 truncate text-center hidden sm:block">
                    <ArtistLink artists={t.artists} />
                  </span>
                )}
                {showAlbum && (
                  <span className="min-w-[120px] max-w-[280px] shrink-0 text-center hidden sm:block">
                    {t.albums?.[0]?.id ? (
                      <Link
                        to={`/albums/${t.albums[0].id}`}
                        className="text-sm text-zinc-500 truncate hover:text-white transition-colors"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {t.albums[0].title || ""}
                      </Link>
                    ) : (
                      <span className="text-sm text-zinc-500 truncate">
                        {t.albums?.[0]?.title || ""}
                      </span>
                    )}
                  </span>
                )}
                {showDuration && (
                  <span className="w-16 shrink-0 text-center text-sm text-zinc-400">
                    {formatDuration(t.duration)}
                  </span>
                )}
                {extraColumn && (
                  <span className="min-w-[80px] max-w-[140px] shrink-0 text-center hidden sm:block">
                    {extraColumn(t)}
                  </span>
                )}
                {extraAction && (
                  <span className="w-10 shrink-0 text-center">{extraAction(t, i)}</span>
                )}
              </div>
            </div>
          );
        })}
        {tracks.length === 0 && (
          <p className="text-zinc-500 text-center py-12">
            {emptyText || t("trackTable.noTracksFound")}
          </p>
        )}
      </div>
    </div>
  );
}
