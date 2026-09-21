import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import { translateApiError } from "../../i18n/errorCodes";
import { Button } from "../../components/ui/button";
import { Modal } from "../../components/ui/modal";
import { MenuItem } from "../../components/ui/menu";
import PluginToolbar from "./PluginToolbar";
import { PluginRefreshButton } from "./PluginRefreshButton";
import { useListControls } from "./useListControls";
import { PluginCard } from "./PluginCard";
import { api } from "../../api/client";
import { Puzzle, Download, Loader2, ShieldCheck, History, PackagePlus } from "lucide-react";
import type { PluginCatalogEntry, PluginsOutletContext } from "../../types";

// pluginKey is the stable identity of a market entry (repo:name); two repos
// can host plugins with the same name, so name alone is not unique.
function pluginKey(e: PluginCatalogEntry): string {
  return `${e.repo}:${e.name}`;
}

function installLabel(
  entry: PluginCatalogEntry,
  installing: string | null,
  t: (key: string) => string,
): string {
  if (installing === pluginKey(entry)) return t("plugins.installing");
  if (entry.installed) return t("plugins.installed");
  return t("plugins.installPlugin");
}

const DEFAULT_FILTERS: Record<string, string[]> = { author: [], tag: [], repo: [] };

const SORT_SPECS = [
  { key: "name", labelKey: "plugins.sortByName" },
  { key: "downloads", labelKey: "plugins.sortByDownloads" },
  { key: "author", labelKey: "plugins.sortByAuthor" },
  { key: "tag", labelKey: "plugins.sortByTag" },
  { key: "repo", labelKey: "plugins.sortByRepo" },
  { key: "updated_at", labelKey: "plugins.sortByUpdatedAt" },
] as const;

type SortKey = (typeof SORT_SPECS)[number]["key"];

const SORTERS: Record<SortKey, (a: PluginCatalogEntry, b: PluginCatalogEntry) => number> = {
  name: (a, b) => a.name.localeCompare(b.name),
  downloads: (a, b) => (b.downloads ?? -1) - (a.downloads ?? -1),
  author: (a, b) => (a.author || "").localeCompare(b.author || ""),
  tag: (a, b) => (a.tags || []).join(",").localeCompare((b.tags || []).join(",")),
  repo: (a, b) => a.repo.localeCompare(b.repo),
  updated_at: (a, b) => (b.updated_at || "").localeCompare(a.updated_at || ""),
};

function matches(p: PluginCatalogEntry, query: string, filters: Record<string, string[]>): boolean {
  if (
    query &&
    ![p.name, p.description || "", p.author || "", ...(p.tags || [])]
      .join(" ")
      .toLowerCase()
      .includes(query)
  )
    return false;
  if (filters.author.length > 0 && (!p.author || !filters.author.includes(p.author))) return false;
  if (filters.repo.length > 0 && !filters.repo.includes(p.repo)) return false;
  if (filters.tag.length > 0 && !(p.tags || []).some((tag) => filters.tag.includes(tag)))
    return false;
  return true;
}

// The marketplace refresh button drives the plugin repository sync task. We
// poll that single task's status so the "syncing" spinner survives page
// navigation: on re-mount the poll re-discovers a still-running sync and
// keeps spinning until it finishes, then reloads the market cards.
const SYNC_TASK_ID = "plugin_repo_sync";
const SYNC_POLL_MS = 2000;

export default function MarketTab() {
  const { t } = useTranslation();
  const { setToolbar } = useOutletContext<PluginsOutletContext>();
  const [plugins, setPlugins] = useState<PluginCatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [installError, setInstallError] = useState("");
  const [syncing, setSyncing] = useState(false);
  const [syncError, setSyncError] = useState("");
  const { search, setSearch, filters, setFilters, sortBy, setSortBy, filtered, toggleFilter } =
    useListControls(plugins, DEFAULT_FILTERS, matches, SORTERS);
  const [selected, setSelected] = useState<PluginCatalogEntry | null>(null);
  const [historyEntry, setHistoryEntry] = useState<PluginCatalogEntry | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);
  // Tracks the previous poll's running state so the running→idle transition
  // (and only that transition) triggers a market reload.
  const prevRunningRef = useRef(false);

  const loadMarket = useCallback(async () => {
    try {
      const d = await api.plugins.market();
      setPlugins(d.plugins || []);
      setLoadError("");
    } catch (err: unknown) {
      setLoadError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadMarket();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Poll the sync task's status. A running→idle transition means a sync just
  // finished: reload the market (surfacing last_error as a friendly failure).
  const pollSync = useCallback(async () => {
    try {
      const task = await api.tasks.get(SYNC_TASK_ID);
      const running = task.status === "running";
      setSyncing(running);
      if (prevRunningRef.current && !running) {
        setSyncError(task.last_error ? `${t("plugins.syncFailed")}: ${task.last_error}` : "");
        await loadMarket();
      }
      prevRunningRef.current = running;
    } catch {
      // Surface a failed status check instead of leaving the spinner stuck
      // silently (or, on the mount check, hiding a running background sync).
      setSyncError(t("plugins.syncFailed"));
    }
  }, [loadMarket, t]);

  // One-shot check on mount: a sync may already be running (scheduled, or
  // triggered from another tab) when entering the market page, in which case
  // the poll loop below picks it up and keeps the spinner until it finishes.
  useEffect(() => {
    pollSync();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Poll only while a sync is in progress; the interval stops as soon as the
  // task leaves "running", so an idle market page does not keep requesting.
  useEffect(() => {
    if (!syncing) return;
    const id = setInterval(pollSync, SYNC_POLL_MS);
    return () => clearInterval(id);
  }, [syncing, pollSync]);

  const syncRepos = useCallback(async () => {
    setSyncError("");
    try {
      await api.tasks.run(SYNC_TASK_ID);
    } catch (err: unknown) {
      // 409 means a sync is already in progress: keep spinning until the poll
      // loop observes it finish; any other error stops immediately.
      if ((err as { status?: number }).status !== 409) {
        setSyncError(translateApiError(t, err));
        return;
      }
    }
    // Only start polling after the task is known to be running (RunNow marks
    // it running before responding). Setting these optimistically raced the
    // first poll against a slow POST: a still-idle task was misread as
    // "just finished", which stopped polling early.
    prevRunningRef.current = true;
    setSyncing(true);
  }, [t]);

  const authorOptions = useMemo(
    () => Array.from(new Set(plugins.map((p) => p.author).filter((a): a is string => !!a))).sort(),
    [plugins],
  );
  const tagOptions = useMemo(
    () => Array.from(new Set(plugins.flatMap((p) => p.tags || []))).sort(),
    [plugins],
  );
  const repoOptions = useMemo(
    () => Array.from(new Set(plugins.map((p) => p.repo))).sort(),
    [plugins],
  );

  const install = async (entry: PluginCatalogEntry) => {
    setInstalling(pluginKey(entry));
    setInstallError("");
    try {
      await api.plugins.install(entry.name, entry.repo);
      setPlugins((prev) =>
        prev.map((p) =>
          p.repo === entry.repo && p.name === entry.name ? { ...p, installed: true } : p,
        ),
      );
      setSelected(null);
    } catch (err: unknown) {
      setInstallError(translateApiError(t, err));
    } finally {
      setInstalling(null);
    }
  };

  const filterGroups = useMemo(
    () =>
      [
        { key: "author", label: t("plugins.filterByAuthor"), options: authorOptions },
        { key: "tag", label: t("plugins.filterByTag"), options: tagOptions },
        { key: "repo", label: t("plugins.filterByRepo"), options: repoOptions },
      ].filter((g) => g.options.length > 0),
    [t, authorOptions, tagOptions, repoOptions],
  );
  const sortOptions = useMemo(
    () => SORT_SPECS.map((s) => ({ key: s.key, label: t(s.labelKey) })),
    [t],
  );
  const searchPlaceholder = t("plugins.searchMarket");

  const toolbarNode = useMemo(
    () => (
      <div className="flex items-center gap-2">
        <PluginToolbar
          filterGroups={filterGroups}
          filters={filters}
          onFilterToggle={toggleFilter}
          onFiltersClear={() => setFilters({ ...DEFAULT_FILTERS })}
          searchValue={search}
          onSearch={setSearch}
          searchPlaceholder={searchPlaceholder}
          sortOptions={sortOptions}
          sortBy={sortBy}
          onSortBy={setSortBy}
        />
        <PluginRefreshButton
          label={t("plugins.syncRepos")}
          busyLabel={t("plugins.syncing")}
          busy={syncing}
          onClick={syncRepos}
        />
      </div>
    ),
    [
      filterGroups,
      filters,
      search,
      sortBy,
      sortOptions,
      searchPlaceholder,
      syncing,
      syncRepos,
      toggleFilter,
      setFilters,
      setSearch,
      setSortBy,
      t,
    ],
  );

  useEffect(() => {
    setToolbar(toolbarNode);
    return () => setToolbar(null);
  }, [toolbarNode, setToolbar]);

  return (
    <div className="space-y-4">
      {loadError && <p className="text-xs text-red-400">{loadError}</p>}
      {syncError && <p className="text-xs text-red-400">{syncError}</p>}

      {loading && (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      )}
      {!loading && filtered.length === 0 && (
        <p className="text-sm text-zinc-500 py-8 text-center">
          {plugins.length === 0 ? t("plugins.marketEmpty") : t("plugins.noMatch")}
        </p>
      )}
      {!loading && filtered.length > 0 && (
        <div className="grid gap-4 grid-cols-[repeat(auto-fill,minmax(230px,1fr))]">
          {filtered.map((p) => (
            <PluginCard
              key={`${p.repo}:${p.name}`}
              name={p.name}
              version={p.version}
              verified={p.official ? "official" : "third_party"}
              sourceLabel={p.repo}
              description={p.description}
              author={p.author}
              downloads={p.downloads}
              badge={
                p.installed ? (
                  <span className="text-[10px] px-1.5 py-px rounded-full bg-green-600/20 text-green-400 shrink-0">
                    {t("plugins.installed")}
                  </span>
                ) : undefined
              }
              menuLabel={t("plugins.menuLabel")}
              onClick={() => setSelected(p)}
              menuItems={
                <>
                  <MenuItem
                    icon={<PackagePlus className="w-4 h-4" />}
                    disabled={p.installed}
                    onClick={() => setSelected(p)}
                  >
                    {installLabel(p, installing, t)}
                  </MenuItem>
                  <MenuItem
                    icon={<History className="w-4 h-4" />}
                    onClick={() => setHistoryEntry(p)}
                  >
                    {t("plugins.versionHistory")}
                  </MenuItem>
                </>
              }
            />
          ))}
        </div>
      )}

      {selected && (
        <Modal title={selected.name} onClose={() => setSelected(null)}>
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <div className="w-12 h-12 rounded-lg bg-zinc-800 flex items-center justify-center shrink-0">
                <Puzzle className="w-6 h-6 text-green-500" />
              </div>
              <div className="min-w-0">
                <div className="font-medium">{selected.name}</div>
                <div className="text-xs text-zinc-500">
                  v{selected.version} · {selected.author || t("common.unknown")}
                </div>
              </div>
              <div className="ml-auto flex items-center gap-1 text-xs text-zinc-400 shrink-0">
                {selected.official ? (
                  <ShieldCheck className="w-4 h-4 text-green-500" />
                ) : (
                  <ShieldCheck className="w-4 h-4 text-zinc-500" />
                )}
                {selected.repo}
              </div>
            </div>

            {selected.description && (
              <p className="text-sm text-zinc-300">{selected.description}</p>
            )}

            {selected.tags && selected.tags.length > 0 && (
              <div className="flex items-center gap-1.5 flex-wrap">
                {selected.tags.map((tag) => (
                  <span
                    key={tag}
                    className="text-xs px-2 py-0.5 rounded-full bg-zinc-800 text-zinc-400"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            )}

            <div className="flex items-center gap-4 text-xs text-zinc-500">
              {selected.downloads !== undefined && (
                <span className="flex items-center gap-1">
                  <Download className="w-3.5 h-3.5" />
                  {t("plugins.downloads", { count: selected.downloads })}
                </span>
              )}
              {selected.updated_at && <span>{selected.updated_at}</span>}
            </div>

            {installError && <p className="text-xs text-red-400">{installError}</p>}

            <div className="flex justify-end">
              <Button
                onClick={() => install(selected)}
                disabled={selected.installed || installing === pluginKey(selected)}
              >
                {installLabel(selected, installing, t)}
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {historyEntry && (
        <Modal
          title={`${historyEntry.name} ${t("plugins.versionHistory")}`}
          onClose={() => setHistoryEntry(null)}
        >
          <div className="space-y-3">
            {!historyEntry.history || historyEntry.history.length === 0 ? (
              <p className="text-sm text-zinc-500">{t("plugins.noHistory")}</p>
            ) : (
              historyEntry.history.map((h, i) => (
                <div key={i} className="border border-zinc-800 rounded-lg p-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">v{h.version}</span>
                    {h.date && <span className="text-xs text-zinc-500">{h.date}</span>}
                  </div>
                  {h.description && <p className="text-xs text-zinc-400 mt-1">{h.description}</p>}
                </div>
              ))
            )}
          </div>
        </Modal>
      )}
    </div>
  );
}
