import { useState, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Link } from "react-router-dom";
import { ROUTES } from "../../lib/constants";
import { translateApiError } from "../../i18n/errorCodes";
import { useLibrary } from "../../stores/library";
import { usePlayer, type PlayerTrack } from "../../stores/player";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Card } from "../../components/ui/card";
import { api } from "../../api/client";
import {
  Music,
  ScanSearch,
  ColumnsSettings,
  Trash2,
  Plus,
  FolderOpen,
  Loader2,
  Search,
  X,
  Image,
  Pen,
  RefreshCw,
  Scan,
  TriangleAlert,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
  FileText,
} from "lucide-react";
import { formatDuration, performerNames } from "../../lib/utils";
import ArtistLink from "../../components/ArtistLink";
import ArtistSelector, { type SelectedArtist } from "../../components/ArtistSelector";
import ExtIDEditor from "../../components/ExtIDEditor";
import DirectoryPicker from "../../components/DirectoryPicker";
import { usePerPage } from "../../hooks/usePerPage";
import type { Library } from "../../types";

export default function LibrariesTab() {
  const { t } = useTranslation();
  const { libraries, load: reloadLibs } = useLibrary();
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [createError, setCreateError] = useState("");
  const formRef = useRef<HTMLDivElement>(null);
  const [scanning, setScanning] = useState<
    Record<string, { scanned: number; total: number; errors?: number }>
  >({});
  const pollingRef = useRef<Record<string, boolean>>({});
  const pollRetryRef = useRef<Record<string, number>>({});
  const statusCheckSeqRef = useRef(0);
  // Give up polling after this many consecutive status failures (~30s).
  const MAX_POLL_RETRIES = 30;

  const [dirPickerOpen, setDirPickerOpen] = useState(false);

  const [scanDialogLib, setScanDialogLib] = useState<string | null>(null);
  const [scanOverwrite, setScanOverwrite] = useState(false);
  const [manageLib, setManageLib] = useState<Library | null>(null);
  const [manageTracks, setManageTracks] = useState<ManageTrack[]>([]);
  const [availableSources, setAvailableSources] = useState<{ name: string; label: string }[]>([]);
  const [managePage, setManagePage] = useState(1);
  const [managePerPage, setManagePerPage] = usePerPage("manage", 20);
  const [manageTotal, setManageTotal] = useState(0);
  const [manageSearch, setManageSearch] = useState("");
  const [manageLoading, setManageLoading] = useState(false);
  const [managePageEditing, setManagePageEditing] = useState(false);
  const [manageEditValue, setManageEditValue] = useState("");
  const [managePerPageOpen, setManagePerPageOpen] = useState(false);
  const [manageError, setManageError] = useState("");
  const [listError, setListError] = useState("");
  const [searching, setSearching] = useState("");
  const [searchModal, setSearchModal] = useState<SearchResultData | null>(null);
  const loadSeqRef = useRef(0);
  const searchSeqRef = useRef(0);

  useEffect(() => {
    if (!manageLib) return;
    let cancelled = false;
    api.metadata
      .sources()
      .then((d) => {
        if (!cancelled) setAvailableSources(d.sources || []);
      })
      .catch((err) => {
        console.error("Failed to load metadata sources:", err);
      });
    return () => {
      cancelled = true;
    };
  }, [manageLib]);

  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const searchRef = useRef(manageSearch);
  const skipDebounceRef = useRef(false);

  // Keep the ref in sync via an effect — assigning refs during render is a
  // render-phase side effect and can diverge under concurrent rendering.
  useEffect(() => {
    searchRef.current = manageSearch;
  }, [manageSearch]);

  const manageTotalPages = Math.ceil(manageTotal / managePerPage);
  const manageCommitPage = (val: string) => {
    const v = parseInt(val);
    if (v >= 1 && v <= manageTotalPages) setManagePage(v);
    setManagePageEditing(false);
  };
  const manageStartEdit = () => {
    setManageEditValue("");
    setManagePageEditing(true);
  };

  const doLoad = useCallback(
    (pageOverride?: number) => {
      if (!manageLib) {
        setManageTracks([]);
        return;
      }
      // Sequence guard: a slow stale response must not overwrite a newer one
      // when the user flips pages or types in the search box quickly.
      const seq = ++loadSeqRef.current;
      setManageLoading(true);
      setManageError("");
      const params: Record<string, string> = {
        libId: manageLib.id,
        page: String(pageOverride ?? managePage),
        per_page: String(managePerPage),
        all: "1",
      };
      const q = searchRef.current.trim();
      if (q) params.q = q;
      api.data
        .tracksQuery(params)
        .then((d) => {
          if (seq !== loadSeqRef.current) return;
          setManageTracks(d.items || []);
          setManageTotal(d.total || 0);
        })
        .catch((err: unknown) => {
          if (seq !== loadSeqRef.current) return;
          setManageTracks([]);
          setManageTotal(0);
          setManageError(translateApiError(t, err));
        })
        .finally(() => {
          if (seq === loadSeqRef.current) setManageLoading(false);
        });
    },
    [manageLib, managePage, managePerPage, t],
  );

  useEffect(() => {
    doLoad();
  }, [doLoad]);

  useEffect(() => {
    if (!manageLib) return;
    if (skipDebounceRef.current) {
      skipDebounceRef.current = false;
      return;
    }
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      // Reset to page 1 inside the debounced callback so typing never
      // triggers an immediate request via the managePage dependency.
      setManagePage(1);
      doLoad(1);
    }, 500);
    return () => clearTimeout(timerRef.current);
  }, [manageSearch]);

  useEffect(() => {
    if (!showForm) return;
    const el = formRef.current;
    if (!el) return;
    requestAnimationFrame(() => {
      const container = el.closest("main");
      if (container) {
        container.scrollTo({ top: container.scrollHeight, behavior: "smooth" });
      } else {
        el.scrollIntoView({ behavior: "smooth", block: "end" });
      }
    });
  }, [showForm]);

  useEffect(() => {
    return () => {
      pollingRef.current = {};
      pollRetryRef.current = {};
    };
  }, []);

  // On mount / libraries loaded, check for active scans
  useEffect(() => {
    if (libraries.length === 0) return;
    // Sequence guard: a new check cycle (or unmount) invalidates in-flight
    // responses from the previous one, so a late failure can never
    // resurrect a cleared error or poll a deleted library.
    const seq = ++statusCheckSeqRef.current;
    setListError("");
    libraries.forEach((lib) => {
      api.libraries
        .scanStatus(lib.id)
        .then((status) => {
          if (seq !== statusCheckSeqRef.current) return;
          // Skip when a polling loop for this library is already active so
          // re-running this effect (e.g. after reloadLibs) never spawns a
          // second loop.
          if (status.status === "running" && !pollingRef.current[lib.id]) {
            pollingRef.current[lib.id] = true;
            setScanning((prev) => ({
              ...prev,
              [lib.id]: {
                scanned: status.scanned,
                total: status.total_files,
                errors: status.errors,
              },
            }));
            setTimeout(() => pollScan(lib.id), 1000);
          }
        })
        .catch((err) => {
          if (seq !== statusCheckSeqRef.current) return;
          setListError(translateApiError(t, err));
        });
    });
    return () => {
      statusCheckSeqRef.current++;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libraries]);

  const pollScan = useCallback(
    async (libId: string) => {
      if (!pollingRef.current[libId]) return;
      try {
        const status = await api.libraries.scanStatus(libId);
        if (!pollingRef.current[libId]) return;
        pollRetryRef.current[libId] = 0;
        if (status.status === "running") {
          setScanning((prev) => ({
            ...prev,
            [libId]: { scanned: status.scanned, total: status.total_files, errors: status.errors },
          }));
          setTimeout(() => pollScan(libId), 1000);
        } else {
          setScanning((prev) => {
            const n = { ...prev };
            delete n[libId];
            return n;
          });
          pollingRef.current[libId] = false;
          reloadLibs();
        }
      } catch {
        if (pollingRef.current[libId] === false) return;
        pollRetryRef.current[libId] = (pollRetryRef.current[libId] || 0) + 1;
        if (pollRetryRef.current[libId] >= MAX_POLL_RETRIES) {
          pollingRef.current[libId] = false;
          delete pollRetryRef.current[libId];
          setScanning((prev) => {
            const n = { ...prev };
            delete n[libId];
            return n;
          });
          setListError(t("settings.scanStatusFailed"));
          return;
        }
        setTimeout(() => pollScan(libId), 1000);
      }
    },
    [reloadLibs, t],
  );

  const startScan = useCallback(
    async (id: string, mode?: string) => {
      pollingRef.current[id] = true;
      pollRetryRef.current[id] = 0;
      setListError("");
      setScanning((prev) => ({ ...prev, [id]: { scanned: 0, total: 0 } }));
      try {
        await api.libraries.scan(id, mode);
        pollScan(id);
      } catch (err) {
        setScanning((prev) => {
          const n = { ...prev };
          delete n[id];
          return n;
        });
        pollingRef.current[id] = false;
        setListError(translateApiError(t, err));
      }
    },
    [pollScan, t],
  );

  const create = async () => {
    setCreateError("");
    try {
      await api.libraries.create({ name, path });
      setName("");
      setPath("");
      setShowForm(false);
      reloadLibs();
    } catch (err) {
      setCreateError(translateApiError(t, err));
    }
  };

  const del = async (id: string) => {
    if (confirm(t("settings.deleteLibrary"))) {
      setListError("");
      // Stop any scan polling for this library and clear its progress so a
      // deleted library is never polled again.
      pollingRef.current[id] = false;
      delete pollRetryRef.current[id];
      setScanning((prev) => {
        const n = { ...prev };
        delete n[id];
        return n;
      });
      try {
        await api.libraries.delete(id);
      } catch (err) {
        setListError(translateApiError(t, err));
        return;
      }
      reloadLibs();
      api.user
        .getQueue()
        .then((data: UserQueue) => {
          if (data?.tracks) {
            usePlayer.setState({
              queue: data.tracks,
              queueIdx: data.queue_idx ?? 0,
              shuffleOrder: data.shuffle_order ?? [],
              shuffleIdx: data.shuffle_idx ?? 0,
              mode: data.mode ?? "normal",
            });
          }
          if (!data?.tracks?.length) {
            usePlayer.setState({ track: null, playing: false });
          }
        })
        .catch(() => {});
    }
  };

  return (
    <div className="space-y-4">
      {listError && <p className="text-sm text-red-400">{listError}</p>}
      {libraries.map((lib) => {
        const prog = scanning[lib.id];
        return (
          <Card key={lib.id} className="p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3 min-w-0">
                <Music className="w-4 h-4 text-green-500 shrink-0" />
                <div className="min-w-0">
                  <p className="text-sm font-medium truncate flex items-center gap-2">
                    {lib.name}
                    {(lib.last_scan_errors || 0) > 0 && (
                      <span
                        className="text-yellow-500 text-xs flex items-center gap-0.5 shrink-0"
                        title={t("settings.scanErrors", { count: lib.last_scan_errors })}
                      >
                        <TriangleAlert className="w-4 h-4" />
                        {lib.last_scan_errors}
                      </span>
                    )}
                  </p>
                  <p className="text-xs text-zinc-500 truncate">
                    {lib.track_count} tracks · {lib.path}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {prog ? (
                  <>
                    {(prog.errors ?? 0) > 0 && (
                      <span className="text-yellow-500 text-xs font-semibold tabular-nums flex items-center gap-0.5">
                        <TriangleAlert className="w-3.5 h-3.5" />
                        {prog.errors}
                      </span>
                    )}
                    <span className="text-sm font-semibold text-zinc-300 tabular-nums">
                      {prog.scanned}/{prog.total || "?"}
                    </span>
                  </>
                ) : null}
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setManageLib(lib)}
                  disabled={!!prog}
                >
                  <ColumnsSettings className="w-4 h-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setScanDialogLib(lib.id);
                    setScanOverwrite(false);
                  }}
                  disabled={!!prog}
                >
                  {prog ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <ScanSearch className="w-4 h-4" />
                  )}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => del(lib.id)}>
                  <Trash2 className="w-4 h-4 text-red-400" />
                </Button>
              </div>
            </div>
          </Card>
        );
      })}

      {!showForm && (
        <Card
          role="button"
          tabIndex={0}
          aria-label={t("settings.addLibrary")}
          className="group relative p-4 overflow-hidden border-dashed cursor-pointer hover:border-zinc-500 focus:border-green-500 focus:outline-none transition-colors"
          onClick={() => {
            setCreateError("");
            setShowForm(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              setCreateError("");
              setShowForm(true);
            }
          }}
        >
          <div
            aria-hidden="true"
            className="flex items-center justify-between gap-3 min-w-0 blur-[3px] select-none pointer-events-none"
          >
            <div className="flex items-center gap-3 min-w-0 flex-1">
              <Music className="w-4 h-4 text-green-500 shrink-0" />
              <div className="min-w-0 flex-1">
                <span className="block h-5 w-40 max-w-full rounded bg-zinc-700/80" />
                <span className="block h-4 w-64 max-w-full rounded bg-zinc-700/50" />
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <Button variant="ghost" size="sm" tabIndex={-1}>
                <ColumnsSettings className="w-4 h-4" />
              </Button>
              <Button variant="ghost" size="sm" tabIndex={-1}>
                <ScanSearch className="w-4 h-4" />
              </Button>
              <Button variant="ghost" size="sm" tabIndex={-1}>
                <Trash2 className="w-4 h-4 text-red-400" />
              </Button>
            </div>
          </div>
          <div className="absolute inset-0 flex items-center justify-center">
            <span className="w-9 h-9 rounded-full border-2 border-dashed border-zinc-600 bg-zinc-800/90 flex items-center justify-center shadow-lg transition-colors group-hover:border-green-500 group-focus:border-green-500">
              <Plus className="w-4 h-4 text-zinc-300 transition-colors group-hover:text-green-400 group-focus:text-green-400" />
            </span>
          </div>
        </Card>
      )}

      {showForm && (
        <div ref={formRef}>
          <Card className="space-y-3">
            <Input
              placeholder={t("settings.libraryName")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <div>
              <button
                onClick={() => setDirPickerOpen(true)}
                className="w-full flex items-center gap-2 bg-zinc-800 text-sm text-zinc-300 rounded-lg px-3 py-2.5 border border-zinc-700 hover:border-zinc-500 cursor-pointer text-left"
              >
                <FolderOpen className="w-4 h-4 text-yellow-500 shrink-0" />
                <span className="truncate flex-1">{path || t("settings.selectDirectory")}</span>
              </button>
            </div>
            <DirectoryPicker
              open={dirPickerOpen}
              initialPath={path}
              onClose={() => setDirPickerOpen(false)}
              onSelect={setPath}
            />
            {createError && <p className="text-sm text-red-400">{createError}</p>}
            <div className="flex items-center gap-2">
              <Button size="sm" onClick={create}>
                {t("settings.create")}
              </Button>
              <Button
                size="sm"
                className="bg-zinc-600 hover:bg-zinc-500 text-white"
                onClick={() => {
                  setShowForm(false);
                  setName("");
                  setPath("");
                  setCreateError("");
                }}
              >
                {t("settings.cancel")}
              </Button>
            </div>
          </Card>
        </div>
      )}

      {/* Scan dialog */}
      {scanDialogLib && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
          onClick={() => setScanDialogLib(null)}
        >
          <div
            className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-lg font-bold">{t("settings.scanLibrary")}</h2>
            <label
              className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800 cursor-pointer"
              onClick={() => setScanOverwrite(false)}
            >
              <input
                type="radio"
                checked={!scanOverwrite}
                onChange={() => setScanOverwrite(false)}
                className="accent-green-500"
              />
              <div>
                <p className="text-sm font-medium">{t("settings.searchMissing")}</p>
                <p className="text-xs text-zinc-400">{t("settings.searchMissingDesc")}</p>
              </div>
            </label>
            <label
              className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800 cursor-pointer"
              onClick={() => setScanOverwrite(true)}
            >
              <input
                type="radio"
                checked={scanOverwrite}
                onChange={() => setScanOverwrite(true)}
                className="accent-green-500"
              />
              <div>
                <p className="text-sm font-medium">{t("settings.overwriteAll")}</p>
                <p className="text-xs text-zinc-400">{t("settings.overwriteAllDesc")}</p>
              </div>
            </label>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => setScanDialogLib(null)}>
                {t("settings.cancel")}
              </Button>
              <Button
                variant="primary"
                onClick={() => {
                  const id = scanDialogLib;
                  const mode = scanOverwrite ? "overwrite" : "missing";
                  setScanDialogLib(null);
                  startScan(id, mode);
                }}
              >
                {t("settings.startScan")}
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Library manage modal (full-screen) */}
      {manageLib && (
        <div className="fixed inset-0 bottom-16 z-50 bg-zinc-950 flex flex-col">
          {/* Header */}
          <div className="relative flex items-center px-6 py-4 border-b border-zinc-800 shrink-0">
            <div className="shrink-0">
              <h2 className="text-lg font-bold">{manageLib.name}</h2>
            </div>
            <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-6">
              <input
                type="text"
                placeholder={t("search.searchTracks")}
                value={manageSearch}
                onChange={(e) => {
                  setManageSearch(e.target.value);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    clearTimeout(timerRef.current);
                    skipDebounceRef.current = true;
                    setManagePage(1);
                    doLoad(1);
                  }
                }}
                className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
              />
              {manageSearch && (
                <button
                  onClick={() => {
                    setManageSearch("");
                    setManagePage(1);
                    searchRef.current = "";
                    clearTimeout(timerRef.current);
                    skipDebounceRef.current = true;
                    doLoad(1);
                  }}
                  className="absolute right-7 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer"
                >
                  <X className="w-4 h-4" />
                </button>
              )}
            </div>
            <div className="flex-1" />
            <button
              onClick={() => {
                setManageLib(null);
                setManageTracks([]);
                setManageSearch("");
                setManagePage(1);
                setManageTotal(0);
                setManageError("");
              }}
              className="p-2 rounded-lg hover:bg-zinc-800 cursor-pointer shrink-0"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
          {/* Pagination + Table header + Track list (shared scroll) */}
          <div className="flex-1 overflow-y-auto pb-6">
            <div className="sticky top-0 z-10 bg-zinc-950 py-2">
              <div className="flex items-center justify-end px-6 mb-2">
                <span className="text-sm text-zinc-400">
                  {t("settings.trackCount", { count: manageTotal })}
                </span>
                <div className="flex items-center bg-zinc-800 rounded-lg ml-2">
                  <div className="relative">
                    <button
                      onClick={() => setManagePerPageOpen(!managePerPageOpen)}
                      className="px-3 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-l-lg cursor-pointer transition-colors"
                    >
                      {managePerPage} <ChevronDown className="w-4 h-4 inline-block -m-0.5" />
                    </button>
                    {managePerPageOpen && (
                      <div
                        className="absolute top-full left-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1"
                        onClick={() => setManagePerPageOpen(false)}
                      >
                        {[10, 20, 50].map((n) => (
                          <button
                            key={n}
                            onClick={() => {
                              setManagePerPage(n);
                              setManagePage(1);
                              setManagePerPageOpen(false);
                            }}
                            className={`w-full text-left px-3 py-1.5 text-sm cursor-pointer ${managePerPage === n ? "text-white" : "text-zinc-400 hover:text-white"}`}
                          >
                            {n}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                  <span className="w-px h-4 bg-zinc-700" />
                  <div className="w-24 flex items-center justify-center relative shrink-0">
                    {managePageEditing ? (
                      <>
                        <input
                          type="text"
                          inputMode="numeric"
                          value={manageEditValue}
                          onChange={(e) => setManageEditValue(e.target.value)}
                          onBlur={() => manageCommitPage(manageEditValue)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") manageCommitPage(manageEditValue);
                          }}
                          autoFocus
                          placeholder={`/ ${manageTotalPages || 1}`}
                          className="w-full text-center py-2 text-sm bg-transparent text-zinc-400 border-none outline-none"
                        />
                        {manageTotalPages > 1 && (
                          <div className="absolute left-0 top-full mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 max-h-48 overflow-y-auto min-w-[3rem]">
                            {(() => {
                              // Render only ~50 pages around the current one;
                              // the input above stays for arbitrary jumps.
                              const half = 25;
                              const start = Math.max(1, managePage - half);
                              const end = Math.min(manageTotalPages, managePage + half);
                              const pages: number[] = [];
                              for (let n = start; n <= end; n++) pages.push(n);
                              return pages.map((n) => (
                                <button
                                  key={n}
                                  onMouseDown={() => {
                                    setManagePage(n);
                                    setManagePageEditing(false);
                                  }}
                                  className={`w-full text-center px-3 py-1.5 text-sm cursor-pointer ${n === managePage ? "text-white" : "text-zinc-400 hover:text-white"}`}
                                >
                                  {n}
                                </button>
                              ));
                            })()}
                          </div>
                        )}
                      </>
                    ) : manageTotalPages > 1 ? (
                      <span
                        onClick={manageStartEdit}
                        className="text-sm text-zinc-400 cursor-pointer hover:text-white hover:bg-zinc-700 w-full text-center py-2 transition-colors"
                      >
                        {managePage} / {manageTotalPages}
                      </span>
                    ) : (
                      <span className="text-sm text-zinc-400 w-full text-center py-2">1 / 1</span>
                    )}
                  </div>
                  <span className="w-px h-4 bg-zinc-700" />
                  <button
                    disabled={managePage <= 1}
                    onClick={() => setManagePage((p) => p - 1)}
                    className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]"
                  >
                    <ChevronLeft className="w-4 h-4" />
                    {t("trackTable.prev")}
                  </button>
                  <span className="w-px h-4 bg-zinc-700" />
                  <button
                    disabled={managePage >= manageTotalPages}
                    onClick={() => setManagePage((p) => p + 1)}
                    className="flex items-center justify-center gap-1 px-2 py-2 text-sm text-zinc-400 hover:text-white hover:bg-zinc-700 rounded-r-lg disabled:opacity-30 disabled:hover:bg-transparent cursor-pointer transition-colors min-w-[3.5rem]"
                  >
                    {t("trackTable.next")}
                    <ChevronRight className="w-4 h-4" />
                  </button>
                </div>
              </div>
              <div className="flex items-center gap-1 text-xs text-zinc-500 px-6 py-1">
                <span className="flex-1 min-w-0">{t("trackTable.title")}</span>
                <span className="w-24 shrink-0 text-center">{t("trackTable.version")}</span>
                <span className="w-32 shrink-0 text-center hidden sm:block">
                  {t("trackTable.artist")}
                </span>
                <span className="w-32 shrink-0 text-center hidden sm:block">
                  {t("trackTable.album")}
                </span>
                <span className="w-16 shrink-0 text-center">{t("trackTable.format")}</span>
                <span className="w-16 shrink-0 text-center">{t("trackTable.duration")}</span>
                <span className="w-36 shrink-0 text-center">{t("trackTable.actions")}</span>
              </div>
            </div>
            {manageError && (
              <p className="text-sm text-red-400 px-6 py-2 text-center">{manageError}</p>
            )}
            {manageLoading ? (
              <div className="flex items-center justify-center h-[50vh] text-zinc-500">
                <Loader2 className="w-5 h-5 animate-spin mr-2" /> {t("settings.loadingTracks")}
              </div>
            ) : manageTracks.length === 0 ? (
              <div className="flex items-center justify-center h-[50vh] text-zinc-500">
                {t("trackTable.noResults")}
              </div>
            ) : (
              <div className="space-y-1 px-6">
                {manageTracks.map((trk: ManageTrack) => (
                  <div
                    key={trk.id}
                    className="flex items-center gap-1 py-1 rounded-lg hover:bg-zinc-800/50 text-sm group"
                  >
                    <span className="flex-1 min-w-0 truncate">{trk.title}</span>
                    <span
                      className={`w-24 shrink-0 text-center truncate text-xs ${(trk.version ?? 0) >= 1 ? "text-blue-400" : "text-zinc-600"}`}
                    >
                      {trk.version_label || (trk.version ? `V${trk.version}` : "")}
                    </span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-400 hidden sm:block">
                      <ArtistLink artists={trk.artists} />
                    </span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-500 hidden sm:block">
                      {trk.albums?.[0]?.id ? (
                        <Link
                          to={`${ROUTES.albums}/${trk.albums[0].id}`}
                          className="hover:text-white transition-colors"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {trk.albums[0].title || ""}
                        </Link>
                      ) : (
                        <span>{trk.albums?.[0]?.title || ""}</span>
                      )}
                    </span>
                    <span className="w-16 shrink-0 text-center text-zinc-500">
                      {trk.suffix || trk.file_format || ""}
                    </span>
                    <span className="w-16 shrink-0 text-center text-zinc-400">
                      {formatDuration(trk.duration)}
                    </span>
                    <span className="w-36 shrink-0 flex items-center justify-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                      <button
                        onClick={async () => {
                          // Sequence guard: closing the modal or scanning
                          // another track invalidates any in-flight response.
                          const seq = ++searchSeqRef.current;
                          setSearching(trk.id);
                          setSearchModal({ track: trk, edit: {} });
                          try {
                            const result = await api.metadata.searchTrack({ track_id: trk.id });
                            if (seq !== searchSeqRef.current) return;
                            setSearchModal({ track: trk, result, edit: {} });
                          } catch (err) {
                            if (seq !== searchSeqRef.current) return;
                            setSearchModal({
                              track: trk,
                              error: apiErrorText(t, err),
                            });
                          }
                          if (seq === searchSeqRef.current) setSearching("");
                        }}
                        className="p-1 rounded text-zinc-500 hover:text-green-400 cursor-pointer"
                        title={t("metadata.viewMetadata")}
                      >
                        {searching === trk.id ? (
                          <Loader2 className="w-4 h-4 animate-spin" />
                        ) : (
                          <Scan className="w-4 h-4" />
                        )}
                      </button>
                      <button
                        className="p-1 rounded text-zinc-500 hover:text-blue-400 cursor-pointer"
                        title={t("metadata.editLyrics")}
                      >
                        <FileText className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => {
                          /* TODO: edit cover art */
                        }}
                        className="p-1 rounded text-zinc-500 hover:text-yellow-400 cursor-pointer"
                        title={t("metadata.changeCoverArt")}
                      >
                        <Image className="w-4 h-4" />
                      </button>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
      {searchModal && (
        <SearchResultModal
          data={searchModal}
          onClose={() => {
            // Invalidate any in-flight metadata search so a late response
            // cannot re-open the just-closed modal.
            searchSeqRef.current++;
            // Reset the row spinner too — the seq guard above would
            // otherwise skip its reset and leave it spinning forever.
            setSearching("");
            setSearchModal(null);
          }}
          onUpdate={(edit) => setSearchModal((prev) => (prev ? { ...prev, edit } : prev))}
          onSaved={() => {
            doLoad();
          }}
          availableSources={availableSources}
        />
      )}
    </div>
  );
}

interface TrackArtist {
  artist_id: string;
  name?: string;
  role?: string;
  artist?: { name?: string };
}

interface ManageTrack {
  id: string;
  title: string;
  version?: number;
  version_label?: string;
  suffix?: string;
  file_format?: string;
  duration: number;
  cover_image_id?: string;
  artists?: TrackArtist[];
  albums?: Array<{ id?: string; title?: string }>;
}

interface TrackEdit {
  title?: string;
  album?: string;
  year?: string | number;
  genre?: string;
  track_external_id?: string;
  album_external_id?: string;
  version_label?: string;
}

interface AlbumSearchResult {
  title?: string;
  external_id?: string;
  artist?: string;
  source?: string;
}

interface UserQueue {
  tracks?: PlayerTrack[];
  queue_idx?: number;
  shuffle_order?: number[];
  shuffle_idx?: number;
  mode?: "normal" | "all" | "one" | "shuffle";
}

interface SearchResultData {
  track?: ManageTrack & {
    external_id?: string;
    file_name?: string;
    file_hash?: string;
    album?: string;
    metadata_source?: string;
    year?: number;
    genre?: string;
  };
  result?: {
    matched?: boolean;
    cached?: boolean;
    track_external_id?: string;
    source?: string;
    title?: string;
    album?: string;
    album_external_id?: string;
    year?: number;
    genre?: string;
    file_hash?: string;
    artists?: Array<{ name?: string; external_id?: string; source?: string }>;
    albums?: Array<{ id?: string; title?: string; external_id?: string; source?: string }>;
  };
  edit?: TrackEdit;
  error?: string;
}

// Translates API errors (objects carrying a status, as thrown by the api
// client) and falls back to the network-error message for anything else
// (e.g. fetch TypeErrors without structured error data).
function apiErrorText(t: TFunction, err: unknown): string {
  const e = err as { status?: unknown };
  if (e && typeof e === "object" && typeof e.status === "number") {
    return translateApiError(t, err);
  }
  return t("settings.networkError");
}

function SearchResultModal({
  data,
  onClose,
  onUpdate,
  onSaved,
  availableSources,
}: {
  data: SearchResultData;
  onClose: () => void;
  onUpdate: (e: TrackEdit) => void;
  onSaved?: () => void;
  availableSources: { name: string; label: string }[];
}) {
  const { t: tModal } = useTranslation();
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [selectedArtists, setSelectedArtists] = useState<SelectedArtist[]>([]);
  const [extIDValue, setExtIDValue] = useState("");
  const [extIDEditing, setExtIDEditing] = useState(false);
  const [extIDSearching, setExtIDSearching] = useState(false);
  const [extIDError, setExtIDError] = useState(false);
  const [extIDSource, setExtIDSource] = useState("");
  const [extIDSearched, setExtIDSearched] = useState("");
  const [forceUnmatched, setForceUnmatched] = useState(false);
  const [reidentifying, setReidentifying] = useState(false);
  const [selectedAlbums, setSelectedAlbums] = useState<
    { id?: string; title: string; external_id?: string; artist?: string; source?: string }[]
  >([]);
  const [albumQuery, setAlbumQuery] = useState("");
  const [albumResults, setAlbumResults] = useState<AlbumSearchResult[]>([]);
  const [albumSearching, setAlbumSearching] = useState(false);

  useEffect(() => {
    const result = data?.result;
    if (!result?.artists?.length) return;
    setSelectedArtists(
      result.artists.map((a) => ({
        name: a.name || "",
        external_id: a.external_id || "",
      })),
    );
  }, [data?.result?.artists]);

  useEffect(() => {
    const extID = data?.result?.track_external_id ?? data?.track?.external_id ?? "";
    setExtIDValue(extID);
    setExtIDSearched(extID);
    const src = data?.result?.source ?? data?.track?.metadata_source ?? "";
    setExtIDSource(src || availableSources[0]?.name || "");
    setForceUnmatched(false);
    setSaveError("");
  }, [
    data?.result?.track_external_id,
    data?.track?.external_id,
    data?.result?.source,
    data?.track?.metadata_source,
    availableSources,
  ]);

  useEffect(() => {
    const albums = data?.result?.albums;
    if (albums && albums.length > 0) {
      setSelectedAlbums(
        albums.map((a) => ({
          id: a.id,
          title: a.title || "",
          external_id: a.external_id || "",
          source: a.source,
        })),
      );
    } else if (data?.result?.album) {
      const initial = [
        {
          title: data.result.album,
          external_id: data.result.album_external_id || "",
          source: data.result.source,
        },
      ];
      if (data.result.album_external_id) {
        setSelectedAlbums(initial);
      } else if (!data.edit?.album) {
        setSelectedAlbums(initial);
      }
    } else if (data?.edit?.album) {
      setSelectedAlbums([
        { title: data.edit.album, external_id: data.edit.album_external_id || "" },
      ]);
    }
  }, [
    data?.result?.album,
    data?.result?.album_external_id,
    data?.result?.albums,
    data?.edit?.album,
    data?.edit?.album_external_id,
  ]);

  const handleExtIDSearch = async () => {
    if (!extIDValue) return;
    setExtIDSearching(true);
    setExtIDError(false);
    try {
      const result = await api.metadata.searchTrack({
        track_id: data.track?.id || "",
        external_id: extIDValue,
        source: extIDSource || availableSources[0]?.name || "",
      });
      if (result.matched && result.track_external_id) {
        setExtIDSearched(extIDValue);
        onUpdate({
          ...(data.edit ?? {}),
          title: result.title ?? "",
          album: result.album ?? "",
          year: result.year ?? 0,
          genre: result.genre ?? "",
          track_external_id: result.track_external_id ?? "",
          album_external_id: result.album_external_id ?? "",
        });
        if (isMatched) {
          setForceUnmatched(true);
        }
        if (result.album || result.album_external_id) {
          setSelectedAlbums([
            {
              title: result.album || "",
              external_id: result.album_external_id || "",
              artist: result.artists?.[0]?.name || "",
              source: result.source,
            },
          ]);
        }
        if (result.artists?.length) {
          setSelectedArtists(
            result.artists.map((a: { name?: string; external_id?: string }) => ({
              name: a.name || "",
              external_id: a.external_id || "",
            })),
          );
        }
      } else {
        // Design: on search failure, keep the user-entered extIDValue so the
        // Save button stays disabled (extIDValue !== extIDSearched). This
        // forces the user to either correct the ID or close the modal — stale
        // data must not be saved.
        setExtIDError(true);
      }
    } catch (err) {
      setExtIDError(true);
      setSaveError(apiErrorText(tModal, err));
      setExtIDSearching(false);
      return;
    }
    setExtIDSearching(false);
  };

  const albumSearchSeqRef = useRef(0);

  const doAlbumSearch = async () => {
    if (!albumQuery.trim()) return;
    const seq = ++albumSearchSeqRef.current;
    setAlbumSearching(true);
    try {
      const d = await api.metadata.searchAlbum({
        name: albumQuery.trim(),
        source: extIDSource,
      });
      if (seq !== albumSearchSeqRef.current) return;
      setAlbumResults(d.releases || []);
    } catch {
      if (seq === albumSearchSeqRef.current) setAlbumResults([]);
    }
    if (seq === albumSearchSeqRef.current) setAlbumSearching(false);
  };

  const addAlbum = (al: {
    title: string;
    external_id: string;
    artist?: string;
    source?: string;
  }) => {
    // Selecting an album invalidates any in-flight search so a late
    // response cannot repopulate the results list.
    albumSearchSeqRef.current++;
    const exists = al.external_id
      ? selectedAlbums.some((a) => a.external_id === al.external_id)
      : selectedAlbums.some((a) => a.title === al.title && !a.external_id);
    if (!exists) {
      setSelectedAlbums([...selectedAlbums, al]);
    }
    setAlbumQuery("");
    setAlbumResults([]);
  };

  const removeAlbum = (idx: number) => {
    setSelectedAlbums(selectedAlbums.filter((_, i) => i !== idx));
  };

  const display = data.result;
  const isMatched = display?.matched && display?.track_external_id;
  const locked = !!data?.result?.track_external_id;

  useEffect(() => {
    if (data?.result && !(data.result?.matched && data.result?.track_external_id)) {
      setExtIDEditing(true);
    }
  }, [data?.result?.matched, data?.result?.track_external_id]);

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="bg-zinc-900 border border-zinc-800 rounded-xl p-6 w-full max-w-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-bold">{tModal("metadata.songMetadata")}</h3>
          <button onClick={onClose} className="p-1 rounded hover:bg-zinc-800 cursor-pointer">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex items-center gap-2 mb-4">
          <p className="text-xs text-zinc-500 truncate flex-1">{data.track?.title}</p>
          {data.result !== null && data.result !== undefined && data.track?.id && (
            <button
              onClick={async () => {
                setReidentifying(true);
                try {
                  await api.metadata.reidentify({
                    track_id: data.track?.id,
                    file_hash: data.result?.file_hash || data.track?.file_hash || "",
                  });
                  onSaved?.();
                  onClose();
                } catch (err) {
                  console.error("Reidentify failed:", err);
                  setSaveError(apiErrorText(tModal, err));
                  setReidentifying(false);
                  return;
                }
                setReidentifying(false);
              }}
              disabled={reidentifying}
              className="flex items-center gap-1 px-2 py-0.5 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-50 shrink-0"
            >
              {reidentifying ? (
                <Loader2 className="w-3 h-3 animate-spin" />
              ) : (
                <RefreshCw className="w-3 h-3" />
              )}
              {tModal("metadata.restoreDefaults")}
            </button>
          )}
        </div>
        {data.result === null || data.result === undefined ? (
          <div className="space-y-3">
            <div className="w-full h-1 bg-zinc-700 rounded-full overflow-hidden">
              <div className="h-full bg-green-500 animate-pulse rounded-full w-full" />
            </div>
            <p className="text-sm text-zinc-400 text-center">
              {tModal("settings.loadingMetadata")}
            </p>
          </div>
        ) : data.error ? (
          <p className="text-sm text-red-400">{data.error}</p>
        ) : isMatched && !forceUnmatched ? (
          <div className="space-y-2 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.file")}
              </span>
              <span className="text-sm text-zinc-600 truncate flex-1 min-w-0">
                {data.track?.file_name || ""}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.title")}
              </span>
              <span className="text-sm text-zinc-200 flex-1 min-w-0">{display.title || ""}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.artists")}
              </span>
              <span className="text-sm text-zinc-200 flex-1 min-w-0">
                {performerNames(
                  display.artists?.map((a) => ({
                    artist_id: "",
                    name: a.name,
                    role: "performer",
                  })),
                )}
              </span>
            </div>
            <div className="flex items-start gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right mt-1">
                {tModal("metadata.albums")}
              </span>
              <div className="flex-1 min-w-0 bg-zinc-800 rounded-lg p-1.5 space-y-1">
                {selectedAlbums.map((a, i) => (
                  <div key={i} className="flex items-center gap-1 text-sm w-full">
                    <span className="text-zinc-200 truncate flex-1 min-w-0">{a.title}</span>
                    {a.external_id && (
                      <span className="text-xs text-zinc-500 font-mono shrink-0">
                        {a.external_id.substring(0, 8)}
                      </span>
                    )}
                    {(!locked || i > 0) && (
                      <button
                        onClick={() => removeAlbum(i)}
                        className="p-0.5 text-zinc-500 hover:text-red-400 cursor-pointer shrink-0"
                      >
                        <X className="w-3 h-3" />
                      </button>
                    )}
                    {locked && i === 0 && (
                      <span className="text-xs text-green-500 shrink-0">
                        {tModal("metadata.primary")}
                      </span>
                    )}
                  </div>
                ))}
                <div className="border-t border-zinc-700 pt-1">
                  <div className="flex gap-1">
                    <input
                      value={albumQuery}
                      onChange={(e) => setAlbumQuery(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") doAlbumSearch();
                      }}
                      placeholder={tModal("metadata.searchAlbum")}
                      className="bg-zinc-800 rounded px-2 py-1 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                    />
                    <button
                      onClick={doAlbumSearch}
                      disabled={albumSearching}
                      className="p-1 text-zinc-400 hover:text-white cursor-pointer"
                    >
                      {albumSearching ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : (
                        <Search className="w-3.5 h-3.5" />
                      )}
                    </button>
                  </div>
                  {albumResults.length > 0 && (
                    <div className="mt-1 border border-zinc-700 rounded-lg max-h-32 overflow-y-auto">
                      {albumResults.map((r, i) => {
                        const added = r.external_id
                          ? selectedAlbums.some((a) => a.external_id === r.external_id)
                          : selectedAlbums.some((a) => a.title === r.title && !a.external_id);
                        return (
                          <button
                            key={i}
                            onClick={() => {
                              addAlbum({
                                title: r.title || "",
                                external_id: r.external_id || "",
                                artist: r.artist || "",
                                source: r.source,
                              });
                            }}
                            className="w-full flex items-center gap-1 px-2 py-1 text-left text-sm hover:bg-zinc-700 cursor-pointer"
                          >
                            <span className="text-zinc-200 truncate min-w-0">{r.title}</span>
                            {!r.external_id && <X className="w-3.5 h-3.5 text-zinc-500 shrink-0" />}
                            {r.artist && (
                              <span className="text-xs text-zinc-500 shrink-0">({r.artist})</span>
                            )}
                            {r.external_id ? (
                              <span className="text-xs text-zinc-500 font-mono shrink-0 ml-auto">
                                {r.external_id.substring(0, 8)}
                              </span>
                            ) : (
                              <span className="text-xs text-green-400 shrink-0 ml-auto">
                                {tModal("metadata.clickToSelect")}
                              </span>
                            )}
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            </div>
            {(["year", "genre"] as const).map((f) => (
              <div key={f} className="flex items-center gap-2">
                <span className="text-zinc-400 w-20 shrink-0 text-right">
                  {tModal(f === "year" ? "metadata.year" : "metadata.genre")}
                </span>
                <input
                  value={data.edit?.[f] ?? (display[f] || "")}
                  onChange={(e) => onUpdate({ ...(data.edit ?? {}), [f]: e.target.value })}
                  className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                />
              </div>
            ))}
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.label")}
              </span>
              <input
                value={data.edit?.version_label ?? data.track?.version_label ?? ""}
                onChange={(e) => onUpdate({ ...(data.edit ?? {}), version_label: e.target.value })}
                className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                placeholder="e.g. FLAC 900kbps"
              />
            </div>
            {availableSources.length > 0 && (
              /* Hide ExternalID row when no sources are available — without a
               source selector the user cannot search, and changing external_id
               requires a confirmed search first */
              <div className="flex items-center gap-2">
                <span className="text-zinc-400 w-20 shrink-0 text-right">
                  {tModal("metadata.externalId")}
                </span>
                {extIDValue && !extIDEditing && !extIDError ? (
                  <div className="flex items-center gap-2 flex-1 min-w-0">
                    <span className="text-sm font-mono text-zinc-400 shrink-0">
                      {extIDSource || "musicbrainz"}
                    </span>
                    <span className="text-zinc-600 shrink-0">|</span>
                    <span className="text-xs font-mono text-zinc-500 flex-1 min-w-0 truncate">
                      {extIDValue}
                    </span>
                    <button
                      onClick={() => setExtIDEditing(true)}
                      className="p-1 rounded hover:bg-zinc-700 cursor-pointer shrink-0"
                    >
                      <Pen className="w-3.5 h-3.5" />
                    </button>
                  </div>
                ) : (
                  <ExtIDEditor
                    extIDValue={extIDValue}
                    setExtIDValue={setExtIDValue}
                    extIDSource={extIDSource}
                    setExtIDSource={setExtIDSource}
                    extIDError={extIDError}
                    setExtIDError={setExtIDError}
                    extIDSearching={extIDSearching}
                    handleExtIDSearch={handleExtIDSearch}
                    availableSources={availableSources}
                    orig={data?.result?.track_external_id ?? data?.track?.external_id ?? ""}
                    placeholder={tModal("metadata.externalIdPlaceholder")}
                  />
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-2 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.file")}
              </span>
              <span className="text-sm text-zinc-600 truncate flex-1 min-w-0">
                {data.track?.file_name || ""}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.title")}
              </span>
              <input
                value={data.edit?.title ?? data.result?.title ?? (data.track?.title || "")}
                onChange={(e) => onUpdate({ ...(data.edit ?? {}), title: e.target.value })}
                className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
              />
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right text-sm">
                {tModal("metadata.artists")}
              </span>
              <ArtistSelector
                artists={selectedArtists}
                onChange={setSelectedArtists}
                showAdd
                source={extIDSource}
              />
            </div>
            <div className="flex items-start gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right mt-1">
                {tModal("metadata.albums")}
              </span>
              <div className="flex-1 min-w-0 bg-zinc-800 rounded-lg p-1.5 space-y-1">
                {selectedAlbums.map((a, i) => (
                  <div key={i} className="flex items-center gap-1 text-sm w-full">
                    <span className="text-zinc-200 truncate flex-1 min-w-0">{a.title}</span>
                    {a.external_id && (
                      <span className="text-xs text-zinc-500 font-mono shrink-0">
                        {a.external_id.substring(0, 8)}
                      </span>
                    )}
                    <button
                      onClick={() => removeAlbum(i)}
                      className="p-0.5 text-zinc-500 hover:text-red-400 cursor-pointer shrink-0"
                    >
                      <X className="w-3 h-3" />
                    </button>
                  </div>
                ))}
                <div className="border-t border-zinc-700 pt-1">
                  <div className="flex gap-1">
                    <input
                      value={albumQuery}
                      onChange={(e) => setAlbumQuery(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") doAlbumSearch();
                      }}
                      placeholder={tModal("metadata.searchAlbum")}
                      className="bg-zinc-800 rounded px-2 py-1 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                    />
                    <button
                      onClick={doAlbumSearch}
                      disabled={albumSearching}
                      className="p-1 text-zinc-400 hover:text-white cursor-pointer"
                    >
                      {albumSearching ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : (
                        <Search className="w-3.5 h-3.5" />
                      )}
                    </button>
                  </div>
                  {albumResults.length > 0 && (
                    <div className="mt-1 border border-zinc-700 rounded-lg max-h-32 overflow-y-auto">
                      {albumResults.map((r, i) => {
                        const added = r.external_id
                          ? selectedAlbums.some((a) => a.external_id === r.external_id)
                          : selectedAlbums.some((a) => a.title === r.title && !a.external_id);
                        return (
                          <button
                            key={i}
                            onClick={() => {
                              addAlbum({
                                title: r.title || "",
                                external_id: r.external_id || "",
                                artist: r.artist || "",
                                source: r.source,
                              });
                            }}
                            className="w-full flex items-center gap-1 px-2 py-1 text-left text-sm hover:bg-zinc-700 cursor-pointer"
                          >
                            <span className="text-zinc-200 truncate min-w-0">{r.title}</span>
                            {!r.external_id && <X className="w-3.5 h-3.5 text-zinc-500 shrink-0" />}
                            {r.artist && (
                              <span className="text-xs text-zinc-500 shrink-0">({r.artist})</span>
                            )}
                            {r.external_id ? (
                              <span className="text-xs text-zinc-500 font-mono shrink-0 ml-auto">
                                {r.external_id.substring(0, 8)}
                              </span>
                            ) : (
                              <span className="text-xs text-green-400 shrink-0 ml-auto">
                                {tModal("metadata.clickToSelect")}
                              </span>
                            )}
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            </div>
            {(["year", "genre"] as const).map((f) => (
              <div key={f} className="flex items-center gap-2">
                <span className="text-zinc-400 w-20 shrink-0 text-right">
                  {tModal(f === "year" ? "metadata.year" : "metadata.genre")}
                </span>
                <input
                  value={data.edit?.[f] ?? data.result?.[f] ?? (data.track?.[f] || "")}
                  onChange={(e) => onUpdate({ ...(data.edit ?? {}), [f]: e.target.value })}
                  className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                />
              </div>
            ))}
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">
                {tModal("metadata.label")}
              </span>
              <input
                value={data.edit?.version_label ?? data.track?.version_label ?? ""}
                onChange={(e) => onUpdate({ ...(data.edit ?? {}), version_label: e.target.value })}
                className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                placeholder="e.g. FLAC 900kbps"
              />
            </div>
            {availableSources.length > 0 && (
              /* Same as above: hide ExternalID when no sources available */
              <div className="flex items-center gap-2">
                <span className="text-zinc-400 w-20 shrink-0 text-right">
                  {tModal("metadata.externalId")}
                </span>
                <ExtIDEditor
                  extIDValue={extIDValue}
                  setExtIDValue={setExtIDValue}
                  extIDSource={extIDSource}
                  setExtIDSource={setExtIDSource}
                  extIDError={extIDError}
                  setExtIDError={setExtIDError}
                  extIDSearching={extIDSearching}
                  handleExtIDSearch={handleExtIDSearch}
                  availableSources={availableSources}
                  orig={data?.result?.track_external_id ?? data?.track?.external_id ?? ""}
                  placeholder={tModal("metadata.externalIdPlaceholder")}
                />
              </div>
            )}
          </div>
        )}
        {saveError && <p className="text-sm text-red-400 mt-2">{saveError}</p>}
        {data.result !== null && data.result !== undefined && (
          <button
            onClick={async () => {
              setSaving(true);
              setSaveError("");
              try {
                // When empty, the backend reuses the track's existing source or defaults to musicbrainz
                const saveSource =
                  extIDSource || display?.source || data.track?.metadata_source || "";
                const artists =
                  isMatched && !forceUnmatched
                    ? (display.artists?.map((a) => ({
                        name: a.name || "",
                        external_id: a.external_id || "",
                        source: a.source || saveSource,
                      })) ?? [])
                    : selectedArtists.map((a) => ({
                        name: a.name,
                        external_id: a.external_id || "",
                        source: saveSource,
                      }));
                await api.metadata.save({
                  track_id: data.track?.id || "",
                  file_hash: data.result?.file_hash || data.track?.file_hash || "",
                  track_external_id:
                    extIDValue ||
                    (data.edit?.track_external_id ??
                      data.result?.track_external_id ??
                      data.track?.external_id ??
                      ""),
                  source: saveSource,
                  album_external_id:
                    isMatched && !forceUnmatched
                      ? (display?.album_external_id ?? "")
                      : selectedAlbums[0]?.external_id || data.edit?.album_external_id || "",
                  title:
                    isMatched && !forceUnmatched
                      ? display.title || ""
                      : (data.edit?.title ?? data.result?.title ?? data.track?.title ?? ""),
                  album:
                    isMatched && !forceUnmatched
                      ? display.album || ""
                      : selectedAlbums[0]?.title ||
                        data.edit?.album ||
                        data.result?.album ||
                        data.track?.album ||
                        "",
                  year: parseInt(String(data.edit?.year ?? ""), 10) || display?.year || 0,
                  genre: data.edit?.genre ?? display?.genre ?? "",
                  artists,
                  version_label: data.edit?.version_label ?? data.track?.version_label ?? "",
                  albums: selectedAlbums.map((a) => ({
                    id: a.id || "",
                    title: a.title,
                    external_id: a.external_id || "",
                    artist: a.artist || "",
                    source: a.source || saveSource,
                  })),
                });
                onSaved?.();
                onClose();
              } catch (err) {
                setSaveError(apiErrorText(tModal, err));
              }
              setSaving(false);
            }}
            disabled={saving || extIDError || extIDValue !== extIDSearched}
            className="mt-4 w-full py-2 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer"
          >
            {saving ? tModal("metadata.saving") : tModal("metadata.save")}
          </button>
        )}
      </div>
    </div>
  );
}
