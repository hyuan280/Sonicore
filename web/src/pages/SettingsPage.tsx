import { useState, useRef, useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../i18n/errorCodes";
import { useAuth } from "../stores/auth";
import { useLibrary } from "../stores/library";
import { usePlayer } from "../stores/player";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Card } from "../components/ui/card";
import { api } from "../api/client";
import { Link } from "react-router-dom";
import {
  Music,
  ScanSearch,
  ColumnsSettings,
  Trash2,
  Plus,
  FolderOpen,
  Loader2,
  UserRound,
  SquareLibrary,
  Speaker,
  Turntable,
  Volume2,
  Search,
  X,
  Upload,
  FileText,
  Image,
  Pen,
  RefreshCw,
  Scan,
  TriangleAlert,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
} from "lucide-react";
import LanguageSwitcher from "../components/LanguageSwitcher";
import { formatDuration, performerNames } from "../lib/utils";
import ArtistLink from "../components/ArtistLink";
import ArtistSelector, { type SelectedArtist } from "../components/ArtistSelector";
import DirectoryPicker from "../components/DirectoryPicker";
import { usePerPage } from "../hooks/usePerPage";
import type { JukeboxInfo } from "../stores/jukebox";

export default function SettingsPage() {
  const { t } = useTranslation();
  const { user, logout } = useAuth();
  const { libraries, load: reloadLibs } = useLibrary();
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [scanning, setScanning] = useState<
    Record<string, { scanned: number; total: number; errors?: number }>
  >({});
  const pollingRef = useRef<Record<string, boolean>>({});

  const [dirPickerOpen, setDirPickerOpen] = useState(false);

  const [scanDialogLib, setScanDialogLib] = useState<string | null>(null);
  const [scanOverwrite, setScanOverwrite] = useState(false);
  const [manageLib, setManageLib] = useState<any>(null);
  const [manageTracks, setManageTracks] = useState<any[]>([]);
  const [availableSources, setAvailableSources] = useState<{ name: string; label: string }[]>([]);
  const [managePage, setManagePage] = useState(1);
  const [managePerPage, setManagePerPage] = usePerPage("manage", 20);
  const [manageTotal, setManageTotal] = useState(0);
  const [manageSearch, setManageSearch] = useState("");
  const [manageLoading, setManageLoading] = useState(false);
  const [managePageEditing, setManagePageEditing] = useState(false);
  const [manageEditValue, setManageEditValue] = useState("");
  const [managePerPageOpen, setManagePerPageOpen] = useState(false);
  const [searching, setSearching] = useState("");
  const [searchModal, setSearchModal] = useState<any>(null);

  useEffect(() => {
    if (!manageLib) return;
    const controller = new AbortController();
    const loadSources = async () => {
      try {
        const res = await fetch("/api/metadata/sources", {
          signal: controller.signal,
          headers: { Authorization: "Bearer " + localStorage.getItem("token") },
        });
        if (!res.ok) {
          console.error("Failed to load metadata sources:", res.status);
          return;
        }
        const d = await res.json();
        setAvailableSources(d.sources || []);
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") return;
        console.error("Failed to load metadata sources:", err);
      }
    };
    loadSources();
    return () => controller.abort();
  }, [manageLib]);

  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const searchRef = useRef(manageSearch);
  const skipDebounceRef = useRef(false);
  searchRef.current = manageSearch;

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

  const doLoad = useCallback(() => {
    if (!manageLib) {
      setManageTracks([]);
      return;
    }
    setManageLoading(true);
    const params = new URLSearchParams({
      libId: manageLib.id,
      page: String(managePage),
      per_page: String(managePerPage),
      all: "1",
    });
    const q = searchRef.current.trim();
    if (q) params.set("q", q);
    fetch(`/api/data/tracks?${params}`, {
      headers: { Authorization: "Bearer " + localStorage.getItem("token") },
    })
      .then((r) => r.json())
      .then((d) => {
        setManageTracks(d.items || []);
        setManageTotal(d.total || 0);
      })
      .catch(() => {})
      .finally(() => setManageLoading(false));
  }, [manageLib, managePage, managePerPage]);

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
      doLoad();
    }, 500);
    return () => clearTimeout(timerRef.current);
  }, [manageSearch]);

  const [showPwModal, setShowPwModal] = useState(false);
  const [pwForm, setPwForm] = useState({ oldPw: "", newPw: "", confirmPw: "" });
  const [pwError, setPwError] = useState("");
  const [pwSaving, setPwSaving] = useState(false);

  const roleLabels: Record<string, string> = {
    super_admin: t("settings.superAdmin"),
    admin: t("settings.admin"),
    user: t("settings.user"),
  };
  const roleLabel = (r: string) => roleLabels[r] || t("settings.user");
  const isAdmin = user?.role === "admin" || user?.role === "super_admin";
  useEffect(() => {
    return () => {
      pollingRef.current = {};
    };
  }, []);

  // On mount / libraries loaded, check for active scans
  useEffect(() => {
    if (!isAdmin || libraries.length === 0) return;
    libraries.forEach((lib) => {
      api.libraries
        .scanStatus(lib.id)
        .then((status) => {
          if (status.status === "running") {
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
        .catch(() => {});
    });
  }, [libraries.length]);

  const pollScan = useCallback(
    async (libId: string) => {
      if (!pollingRef.current[libId]) return;
      try {
        const status = await api.libraries.scanStatus(libId);
        if (!pollingRef.current[libId]) return;
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
        setTimeout(() => pollScan(libId), 1000);
      }
    },
    [reloadLibs],
  );

  const startScan = useCallback(
    async (id: string, mode?: string) => {
      pollingRef.current[id] = true;
      setScanning((prev) => ({ ...prev, [id]: { scanned: 0, total: 0 } }));
      try {
        await api.libraries.scan(id, mode);
        pollScan(id);
      } catch {
        setScanning((prev) => {
          const n = { ...prev };
          delete n[id];
          return n;
        });
        pollingRef.current[id] = false;
      }
    },
    [pollScan],
  );

  const create = async () => {
    await api.libraries.create({ name, path });
    setName("");
    setPath("");
    setShowForm(false);
    reloadLibs();
  };

  const del = async (id: string) => {
    if (confirm(t("settings.deleteLibrary"))) {
      await api.libraries.delete(id);
      reloadLibs();
      api.user
        .getQueue()
        .then((data: any) => {
          if (data?.tracks) {
            usePlayer.setState({
              queue: data.tracks.map((t: any) => ({
                id: t.id,
                title: t.title,
                duration: t.duration,
                suffix: t.suffix,
                cover_image_id: t.cover_image_id,
                artists: t.artists,
                albums: t.albums,
              })),
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

  const changePassword = async () => {
    setPwSaving(true);
    setPwError("");
    if (pwForm.newPw !== pwForm.confirmPw) {
      setPwError(t("settings.passwordsNoMatch"));
      setPwSaving(false);
      return;
    }
    try {
      await api.auth.changePassword(pwForm.oldPw, pwForm.newPw);
      setShowPwModal(false);
      setPwForm({ oldPw: "", newPw: "", confirmPw: "" });
    } catch (err: any) {
      setPwError(translateApiError(t, err));
    }
    setPwSaving(false);
  };

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">{t("settings.settings")}</h1>
        <LanguageSwitcher />
      </div>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <UserRound className="w-4 h-4" /> {t("settings.account")}
        </h3>
        <div className="space-y-1 p-3 rounded-lg bg-zinc-800/50">
          <p className="text-sm text-zinc-400">
            {t("settings.username")}: {user?.username}
          </p>
          <p className="text-sm text-zinc-400">
            {t("settings.email")}: {user?.email}
          </p>
          <p className="text-sm text-zinc-400">
            {t("settings.role")}:{" "}
            <span className="text-green-500">{roleLabel(user?.role || "")}</span>
          </p>
        </div>
        <Button variant="primary" size="sm" onClick={() => setShowPwModal(true)}>
          {t("settings.changePassword")}
        </Button>
      </Card>

      {isAdmin && (
        <Card className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="font-medium flex items-center gap-2">
              <SquareLibrary className="w-4 h-4" /> {t("settings.libraries")}
            </h3>
            <Button size="sm" onClick={() => setShowForm(!showForm)} className="px-2 py-1 text-xs">
              <Plus className="w-3.5 h-3.5 mr-0.5" />
              {t("settings.add")}
            </Button>
          </div>

          {showForm && (
            <div className="space-y-3 p-3 rounded-lg bg-zinc-800">
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
              <Button size="sm" onClick={create}>
                {t("settings.create")}
              </Button>
            </div>
          )}

          <div className="space-y-2">
            {libraries.map((lib) => {
              const prog = scanning[lib.id];
              return (
                <div
                  key={lib.id}
                  className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50"
                >
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
              );
            })}
          </div>
        </Card>
      )}

      {isAdmin && <DeviceManager />}
      {isAdmin && <SubsonicJukeboxSetting />}

      {showPwModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
          onClick={() => setShowPwModal(false)}
        >
          <div
            className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-lg font-bold">{t("settings.changePassword")}</h2>

            {pwError && <p className="text-sm text-red-400">{pwError}</p>}

            <Input
              type="password"
              placeholder={t("settings.currentPassword")}
              value={pwForm.oldPw}
              onChange={(e) => setPwForm({ ...pwForm, oldPw: e.target.value })}
            />
            <Input
              type="password"
              placeholder={t("settings.newPassword")}
              value={pwForm.newPw}
              onChange={(e) => setPwForm({ ...pwForm, newPw: e.target.value })}
            />
            <Input
              type="password"
              placeholder={t("settings.confirmPassword")}
              value={pwForm.confirmPw}
              onChange={(e) => setPwForm({ ...pwForm, confirmPw: e.target.value })}
              onKeyDown={(e) => e.key === "Enter" && changePassword()}
            />

            <div className="flex justify-end gap-2 pt-2">
              <Button
                variant="ghost"
                onClick={() => {
                  setShowPwModal(false);
                  setPwError("");
                  setPwForm({ oldPw: "", newPw: "", confirmPw: "" });
                }}
              >
                {t("settings.cancel")}
              </Button>
              <Button
                variant="primary"
                onClick={changePassword}
                disabled={pwSaving || !pwForm.oldPw || !pwForm.newPw || !pwForm.confirmPw}
              >
                {pwSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : t("settings.update")}
              </Button>
            </div>
          </div>
        </div>
      )}

      <Button variant="danger" onClick={logout}>
        {t("settings.signOut")}
      </Button>

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
                  setManagePage(1);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    clearTimeout(timerRef.current);
                    skipDebounceRef.current = true;
                    doLoad();
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
                    doLoad();
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
                            {Array.from({ length: manageTotalPages }, (_, i) => i + 1).map((n) => (
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
                            ))}
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
                {manageTracks.map((trk: any) => (
                  <div
                    key={trk.id}
                    className="flex items-center gap-1 py-1 rounded-lg hover:bg-zinc-800/50 text-sm group"
                  >
                    <span className="flex-1 min-w-0 truncate">{trk.title}</span>
                    <span
                      className={`w-24 shrink-0 text-center truncate text-xs ${trk.version >= 1 ? "text-blue-400" : "text-zinc-600"}`}
                    >
                      {trk.version_label || (trk.version ? `V${trk.version}` : "")}
                    </span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-400 hidden sm:block">
                      <ArtistLink artists={trk.artists} />
                    </span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-500 hidden sm:block">
                      {trk.albums?.[0]?.id ? (
                        <Link
                          to={`/albums/${trk.albums[0].id}`}
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
                          setSearching(trk.id);
                          setSearchModal({ track: trk, edit: {} });
                          try {
                            const res = await fetch("/api/metadata/search/track", {
                              method: "POST",
                              headers: {
                                "Content-Type": "application/json",
                                Authorization: "Bearer " + localStorage.getItem("token"),
                              },
                              body: JSON.stringify({
                                track_id: trk.id,
                              }),
                            });
                            setSearchModal({ track: trk, result: await res.json(), edit: {} });
                          } catch {
                            setSearchModal({ track: trk, error: t("settings.searchFailed") });
                          }
                          setSearching("");
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
                        title="Edit lyrics"
                      >
                        <FileText className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => {
                          /* TODO: edit cover art */
                        }}
                        className="p-1 rounded text-zinc-500 hover:text-yellow-400 cursor-pointer"
                        title="Change cover art"
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
          onClose={() => setSearchModal(null)}
          onUpdate={(edit) => setSearchModal((prev: any) => ({ ...prev, edit }))}
          onSaved={() => {
            doLoad();
          }}
          availableSources={availableSources}
        />
      )}
    </div>
  );
}

interface SearchResultData {
  track?: {
    id?: string;
    title?: string;
    external_id?: string;
    file_name?: string;
    file_hash?: string;
    album?: string;
    metadata_source?: string;
    version_label?: string;
    year?: number;
    genre?: string;
    artists?: Array<{ name?: string; artist?: { name?: string }; external_id?: string }>;
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
  edit?: Record<string, any>;
  error?: string;
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
  onUpdate: (e: any) => void;
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
  const [albumResults, setAlbumResults] = useState<any[]>([]);
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
  }, [data?.result?.album, data?.result?.album_external_id, data?.result?.albums]);

  const handleExtIDSearch = async () => {
    if (!extIDValue) return;
    setExtIDSearching(true);
    setExtIDError(false);
    try {
      const res = await fetch("/api/metadata/search/track", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + localStorage.getItem("token"),
        },
        body: JSON.stringify({
          track_id: data.track?.id || "",
          external_id: extIDValue,
          source: extIDSource || availableSources[0]?.name || "",
        }),
      });
      const result = await res.json();
      if (!res.ok) {
        setExtIDError(true);
        setSaveError(result.error || "Search failed");
        setExtIDSearching(false);
        return;
      }
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
            result.artists.map((a: any) => ({
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
    } catch {
      setExtIDError(true);
    }
    setExtIDSearching(false);
  };

  const doAlbumSearch = async () => {
    if (!albumQuery.trim()) return;
    setAlbumSearching(true);
    try {
      const res = await fetch("/api/metadata/search/album", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + localStorage.getItem("token"),
        },
        body: JSON.stringify({ name: albumQuery.trim(), source: extIDSource }),
      });
      const d = await res.json();
      setAlbumResults(d.releases || []);
    } catch {
      setAlbumResults([]);
    }
    setAlbumSearching(false);
  };

  const addAlbum = (al: {
    title: string;
    external_id: string;
    artist?: string;
    source?: string;
  }) => {
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

  const display = data.result as any;
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
          {isMatched && (
            <button
              onClick={async () => {
                setReidentifying(true);
                try {
                  await fetch("/api/metadata/reidentify", {
                    method: "POST",
                    headers: {
                      "Content-Type": "application/json",
                      Authorization: "Bearer " + localStorage.getItem("token"),
                    },
                    body: JSON.stringify({
                      track_id: data.track?.id,
                      file_hash: data.result?.file_hash || data.track?.file_hash || "",
                    }),
                  });
                  onSaved?.();
                  onClose();
                } catch (e) {
                  console.error("Reidentify failed:", e);
                  setSaveError(tModal("settings.networkError"));
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
              Restore defaults
            </button>
          )}
        </div>
        {data.result == null ? (
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
              <span className="text-zinc-400 w-20 shrink-0 text-right">File</span>
              <span className="text-sm text-zinc-600 truncate flex-1 min-w-0">
                {data.track?.file_name || ""}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">Title</span>
              <span className="text-sm text-zinc-200 flex-1 min-w-0">{display.title || ""}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">Artists</span>
              <span className="text-sm text-zinc-200 flex-1 min-w-0">
                {performerNames(display.artists)}
              </span>
            </div>
            <div className="flex items-start gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right mt-1">Albums</span>
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
                      <span className="text-xs text-green-500 shrink-0">primary</span>
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
                      placeholder="Search album..."
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
                                title: r.title,
                                external_id: r.external_id,
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
                                click to select
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
                  {f.charAt(0).toUpperCase() + f.slice(1)}
                </span>
                <input
                  value={data.edit?.[f] ?? (display[f] || "")}
                  onChange={(e) => onUpdate({ ...(data.edit ?? {}), [f]: e.target.value })}
                  className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                />
              </div>
            ))}
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">Label</span>
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
                  <div className="flex items-center flex-1 min-w-0">
                    <select
                      value={extIDSource || availableSources[0]?.name || ""}
                      onChange={(e) => {
                        const orig =
                          data?.result?.track_external_id ?? data?.track?.external_id ?? "";
                        if (e.target.value !== extIDSource && extIDValue === orig) {
                          setExtIDValue("");
                        }
                        setExtIDSource(e.target.value);
                        setExtIDError(false);
                      }}
                      className="bg-zinc-800 rounded-l px-2 py-0.5 text-sm border-r border-zinc-700 focus:outline-none focus:ring-1 focus:ring-green-500 shrink-0 min-w-0"
                    >
                      {availableSources.map((s) => (
                        <option key={s.name} value={s.name}>
                          {s.label}
                        </option>
                      ))}
                    </select>
                    <input
                      value={extIDValue}
                      onChange={(e) => {
                        setExtIDValue(e.target.value);
                        setExtIDError(false);
                      }}
                      className={`bg-zinc-800 px-2 py-0.5 text-sm flex-1 min-w-0 font-mono focus:outline-none focus:ring-1 ${extIDError ? "focus:ring-red-500 border border-red-500" : "focus:ring-green-500"}`}
                      placeholder={tModal("metadata.externalIdPlaceholder")}
                      onKeyDown={(e) => e.key === "Enter" && handleExtIDSearch()}
                    />
                    <button
                      onClick={handleExtIDSearch}
                      disabled={extIDSearching || !extIDValue}
                      className="rounded-r px-2 py-0.5 bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50 border-l border-zinc-700 shrink-0"
                    >
                      {extIDSearching ? (
                        <Loader2 className="w-4 h-4 animate-spin" />
                      ) : (
                        <Search className="w-4 h-4" />
                      )}
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="space-y-2 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">File</span>
              <span className="text-sm text-zinc-600 truncate flex-1 min-w-0">
                {data.track?.file_name || ""}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">Title</span>
              <input
                value={data.edit?.title ?? data.result?.title ?? (data.track?.title || "")}
                onChange={(e) => onUpdate({ ...(data.edit ?? {}), title: e.target.value })}
                className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
              />
            </div>
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right text-sm">Artists</span>
              <ArtistSelector
                artists={selectedArtists}
                onChange={setSelectedArtists}
                showAdd
                source={extIDSource}
              />
            </div>
            <div className="flex items-start gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right mt-1">Albums</span>
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
                      placeholder="Search album..."
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
                                title: r.title,
                                external_id: r.external_id,
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
                                click to select
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
                  {f.charAt(0).toUpperCase() + f.slice(1)}
                </span>
                <input
                  value={data.edit?.[f] ?? data.result?.[f] ?? (data.track?.[f] || "")}
                  onChange={(e) => onUpdate({ ...(data.edit ?? {}), [f]: e.target.value })}
                  className="bg-zinc-800 rounded px-2 py-0.5 text-sm flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                />
              </div>
            ))}
            <div className="flex items-center gap-2">
              <span className="text-zinc-400 w-20 shrink-0 text-right">Label</span>
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
                <div className="flex items-center flex-1 min-w-0">
                  <select
                    value={extIDSource || availableSources[0]?.name || ""}
                    onChange={(e) => {
                      const orig =
                        data?.result?.track_external_id ?? data?.track?.external_id ?? "";
                      if (e.target.value !== extIDSource && extIDValue === orig) {
                        setExtIDValue("");
                      }
                      setExtIDSource(e.target.value);
                      setExtIDError(false);
                    }}
                    className="bg-zinc-800 rounded-l px-2 py-0.5 text-sm border-r border-zinc-700 focus:outline-none focus:ring-1 focus:ring-green-500 shrink-0 min-w-0"
                  >
                    {availableSources.map((s) => (
                      <option key={s.name} value={s.name}>
                        {s.label}
                      </option>
                    ))}
                  </select>
                  <input
                    value={extIDValue}
                    onChange={(e) => {
                      setExtIDValue(e.target.value);
                      setExtIDError(false);
                    }}
                    className={`bg-zinc-800 px-2 py-0.5 text-sm flex-1 min-w-0 font-mono focus:outline-none focus:ring-1 ${extIDError ? "focus:ring-red-500 border border-red-500" : "focus:ring-green-500"}`}
                    placeholder={tModal("metadata.externalIdPlaceholder")}
                    onKeyDown={(e) => e.key === "Enter" && handleExtIDSearch()}
                  />
                  <button
                    onClick={handleExtIDSearch}
                    disabled={extIDSearching || !extIDValue}
                    className="rounded-r px-2 py-0.5 bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50 border-l border-zinc-700 shrink-0"
                  >
                    {extIDSearching ? (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    ) : (
                      <Search className="w-4 h-4" />
                    )}
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
        {saveError && <p className="text-sm text-red-400 mt-2">{saveError}</p>}
        {data.result != null && (
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
                    ? (display.artists?.map((a: any) => ({
                        name: a.name || a.artist?.name || "",
                        external_id: a.external_id || "",
                        source: a.source || saveSource,
                      })) ?? [])
                    : selectedArtists.map((a) => ({
                        name: a.name,
                        external_id: a.external_id || "",
                        source: saveSource,
                      }));
                const res = await fetch("/api/metadata/save", {
                  method: "POST",
                  headers: {
                    "Content-Type": "application/json",
                    Authorization: "Bearer " + localStorage.getItem("token"),
                  },
                  body: JSON.stringify({
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
                    year: parseInt(data.edit?.year, 10) || display?.year || 0,
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
                  }),
                });
                if (!res.ok) {
                  const err = await res
                    .json()
                    .catch(() => ({ error: tModal("settings.saveFailed") }));
                  setSaveError(translateApiError(tModal, err));
                  return;
                }
                onSaved?.();
                onClose();
              } catch {
                setSaveError(tModal("settings.networkError"));
              }
              setSaving(false);
            }}
            disabled={saving || extIDError || extIDValue !== extIDSearched}
            className="mt-4 w-full py-2 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer"
          >
            {saving ? "Saving..." : "Save"}
          </button>
        )}
      </div>
    </div>
  );
}

function DeviceManager() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<any[]>([]);
  const [detected, setDetected] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [addForm, setAddForm] = useState({
    name: "",
    device_type: "local",
    device_id: "",
    driver: "pulseaudio",
  });
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");

  const load = () => {
    setLoading(true);
    Promise.all([api.jukebox.deviceConfigs(), api.jukebox.audioDevices()])
      .then(([configs, d]) => {
        setDevices(configs.devices || []);
        setDetected(d.devices || []);
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
  }, []);

  const handleAdd = async () => {
    if (!addForm.name || !addForm.device_id) return;
    setAdding(true);
    setError("");
    try {
      await api.jukebox.createDeviceConfig({
        name: addForm.name,
        device_type: addForm.device_type,
        device_id: addForm.device_id,
        driver: addForm.driver,
      });
      setShowAdd(false);
      setAddForm({ name: "", device_type: "local", device_id: "", driver: "pulseaudio" });
      load();
    } catch (e: any) {
      setError(translateApiError(t, e));
    } finally {
      setAdding(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.jukebox.deleteDeviceConfig(id);
      load();
    } catch {}
  };

  const selectDetected = (d: any) => {
    setAddForm({
      name: d.name || d.id,
      device_type: "local",
      device_id: d.id,
      driver: d.id.startsWith("hw:") ? "alsa" : "pulseaudio",
    });
  };

  const existingIds = new Set(devices.map((d: any) => d.device_id + ":" + d.driver));
  const existingNames = new Set(devices.map((d: any) => d.name));
  const filteredDetected = detected.filter(
    (d: any) =>
      !existingIds.has(d.id + ":" + (d.id.startsWith("hw:") ? "alsa" : "pulseaudio")) &&
      !existingNames.has(d.name || d.id),
  );

  return (
    <Card className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="font-medium flex items-center gap-2">
          <Speaker className="w-4 h-4" /> {t("settings.audioDevices")}
        </h2>
        <Button size="sm" onClick={() => setShowAdd(!showAdd)} className="px-2 py-1 text-xs">
          <Plus className="w-3.5 h-3.5 mr-0.5" /> {t("settings.add")}
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      ) : (
        <>
          {showAdd && (
            <div className="p-3 border border-zinc-700 rounded-lg space-y-3">
              {error && <p className="text-xs text-red-400">{error}</p>}

              <select
                value={addForm.device_type}
                onChange={(e) => {
                  const t = e.target.value;
                  setAddForm({
                    name: "",
                    device_type: t,
                    device_id: "",
                    driver: t === "local" ? "pulseaudio" : t,
                  });
                }}
                className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white"
              >
                <option value="local">local</option>
                <option value="mpd">mpd</option>
                <option value="airplay">airplay</option>
              </select>

              {addForm.device_type === "local" && (
                <div className="space-y-1 max-h-40 overflow-y-auto border border-zinc-800 rounded-lg p-1">
                  {filteredDetected.length === 0 ? (
                    <p className="text-xs text-zinc-500 text-center py-4">
                      {t("settings.allDetectedConfigured")}
                    </p>
                  ) : (
                    filteredDetected.map((d: any) => (
                      <button
                        key={d.id}
                        onClick={() => selectDetected(d)}
                        className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${addForm.device_id === d.id ? "bg-green-600/20" : "hover:bg-zinc-800"}`}
                      >
                        <div className="text-white">{d.name}</div>
                        <div className="text-xs text-zinc-500">{d.id}</div>
                      </button>
                    ))
                  )}
                </div>
              )}

              <div className="space-y-2">
                <Input
                  value={addForm.name}
                  onChange={(e) => setAddForm({ ...addForm, name: e.target.value })}
                  placeholder={
                    addForm.device_type === "local"
                      ? t("settings.nameAutoFilled")
                      : t("settings.deviceName")
                  }
                />
                <Input
                  value={addForm.device_id}
                  onChange={(e) => setAddForm({ ...addForm, device_id: e.target.value })}
                  placeholder={t("settings.deviceId")}
                />
              </div>

              <div className="flex justify-end gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setShowAdd(false);
                    setError("");
                  }}
                >
                  {t("settings.cancel")}
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleAdd}
                  disabled={adding || !addForm.name || !addForm.device_id}
                >
                  {adding ? <Loader2 className="w-4 h-4 animate-spin" /> : t("settings.create")}
                </Button>
              </div>
            </div>
          )}
          {devices.length === 0 ? (
            <p className="text-zinc-500 text-center py-4">{t("settings.noDevices")}</p>
          ) : (
            <div className="space-y-2">
              {devices.map((d: any) => (
                <div key={d.id} className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800/50">
                  <Volume2 className="w-4 h-4 text-green-500 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm">{d.name}</div>
                    <div className="text-xs text-zinc-500">
                      {d.device_type} · {d.driver} · {d.device_id}
                    </div>
                  </div>
                  {d.bound_jukebox ? (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-green-600/20 text-green-400 border border-green-600/30">
                      {d.bound_jukebox}
                    </span>
                  ) : (
                    <Button variant="ghost" size="sm" onClick={() => handleDelete(d.id)}>
                      <Trash2 className="w-4 h-4 text-red-400" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function SubsonicJukeboxSetting() {
  const { t } = useTranslation();
  const [jukeboxes, setJukeboxes] = useState<JukeboxInfo[]>([]);
  const [selected, setSelected] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);

  useEffect(() => {
    Promise.all([api.jukebox.list(), api.admin.getSettings()])
      .then(([jbList, settings]) => {
        setJukeboxes(jbList.jukeboxes || []);
        setSelected(settings.subsonic_jukebox_id || "");
      })
      .catch(() => setError(t("settings.failedToLoadJukebox")))
      .finally(() => setLoading(false));
  }, [t]);

  const save = async (id: string) => {
    const prev = selected;
    setSaving(true);
    setSelected(id);
    setOpen(false);
    try {
      await api.admin.updateSettings({ subsonic_jukebox_id: id || "" });
    } catch {
      setSelected(prev);
      setError(t("settings.failedToSaveJukebox"));
    } finally {
      setSaving(false);
    }
  };

  const selectedJb = selected ? jukeboxes.find((j) => j.id === selected) : null;

  return (
    <Card className="p-4 space-y-3">
      <h2 className="font-medium flex items-center gap-2">
        <img src="/subsonic.png" className="w-4 h-4" /> {t("settings.subsonicJukebox")}
        {selected && (
          <span className="text-xs text-green-400 ml-auto">{t("settings.subsonicHint")}</span>
        )}
      </h2>
      {error && <p className="text-xs text-red-400">{error}</p>}
      {loading ? (
        <Loader2 className="w-4 h-4 animate-spin text-zinc-500" />
      ) : jukeboxes.length === 0 ? (
        <p className="text-xs text-zinc-500">{t("settings.noJukeboxesHint")}</p>
      ) : (
        <div className="relative">
          <button
            onClick={() => setOpen(!open)}
            disabled={saving}
            className="w-full flex items-center text-sm cursor-pointer rounded-lg px-3 py-2.5"
            style={{ backgroundColor: "rgb(32, 32, 34)" }}
          >
            <Turntable
              className={`w-4 h-4 shrink-0 mr-2 ${selected ? "text-green-400" : "text-zinc-500"}`}
            />
            <span className="flex-1 text-left min-w-0">
              {selectedJb ? (
                <>
                  <div className="text-white text-sm">{selectedJb.name}</div>
                  <div className="text-xs text-zinc-500">
                    {selectedJb.device_name || "No device"}
                  </div>
                </>
              ) : (
                <>
                  <div className="text-white text-sm">{t("settings.disabled")}</div>
                  <div className="text-xs text-zinc-500">{t("jukebox.noDevice")}</div>
                </>
              )}
            </span>
            <ChevronDown
              className={`w-4 h-4 ml-2 text-zinc-500 shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
            />
          </button>
          {open && (
            <div className="absolute top-full left-0 right-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 border border-zinc-700/50">
              <button
                onClick={() => save("")}
                className={`w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 ${!selected ? "text-white" : "text-zinc-400 hover:text-white"}`}
              >
                <Turntable className="w-4 h-4 shrink-0 text-zinc-500" />
                <div className="min-w-0">
                  <div className="text-sm">{t("settings.disabled")}</div>
                  <div className="text-xs text-zinc-500">{t("jukebox.noDevice")}</div>
                </div>
              </button>
              {jukeboxes.map((j) => (
                <button
                  key={j.id}
                  onClick={() => save(j.id)}
                  className={`w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 ${j.id === selected ? "text-white" : "text-zinc-400 hover:text-white"}`}
                >
                  <Turntable
                    className={`w-4 h-4 shrink-0 ${j.id === selected ? "text-green-400" : "text-zinc-500"}`}
                  />
                  <div className="min-w-0">
                    <div className="font-medium">{j.name}</div>
                    <div className="text-xs text-zinc-500">{j.device_name || j.device_id}</div>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
