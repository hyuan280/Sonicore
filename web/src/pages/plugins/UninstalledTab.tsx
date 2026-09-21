import { useEffect, useState, useMemo, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import { translateApiError } from "../../i18n/errorCodes";
import { MenuItem } from "../../components/ui/menu";
import PluginToolbar from "./PluginToolbar";
import { PluginRefreshButton } from "./PluginRefreshButton";
import { useListControls } from "./useListControls";
import { PluginCard, sourceInfo } from "./PluginCard";
import { useRepoOfficialMap } from "../../hooks/useRepoOfficialMap";
import { api } from "../../api/client";
import { Loader2, Trash2, PackagePlus } from "lucide-react";
import type { PluginInstance, PluginsOutletContext } from "../../types";

const INITIAL_FILTERS: Record<string, string[]> = { source: [] };

const SORT_SPECS = [
  { key: "name", labelKey: "plugins.sortByName" },
  { key: "downloads", labelKey: "plugins.sortByDownloads" },
  { key: "updated_at", labelKey: "plugins.sortByUpdatedAt" },
] as const;

type SortKey = (typeof SORT_SPECS)[number]["key"];

const SORTERS: Record<SortKey, (a: PluginInstance, b: PluginInstance) => number> = {
  name: (a, b) => a.name.localeCompare(b.name),
  downloads: (a, b) => (b.downloads ?? -1) - (a.downloads ?? -1),
  updated_at: (a, b) => (b.updated_at || "").localeCompare(a.updated_at || ""),
};

function matches(p: PluginInstance, query: string, filters: Record<string, string[]>): boolean {
  if (query && !p.name.toLowerCase().includes(query)) return false;
  if (filters.source.length > 0 && !filters.source.includes(p.source)) return false;
  return true;
}

// UninstalledTab lists plugins that were discovered on disk but are not
// installed yet (or were soft-uninstalled). Each card's menu offers exactly
// two actions: install (moves the plugin to the installed tab, disabled by
// default) and delete (removes the plugin files and the DB record).
export default function UninstalledTab() {
  const { t } = useTranslation();
  const { setToolbar } = useOutletContext<PluginsOutletContext>();
  const [plugins, setPlugins] = useState<PluginInstance[]>([]);
  const repoOfficial = useRepoOfficialMap();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const [refreshing, setRefreshing] = useState(false);
  const { search, setSearch, filters, setFilters, sortBy, setSortBy, filtered, toggleFilter } =
    useListControls(plugins, INITIAL_FILTERS, matches, SORTERS);

  const load = useCallback(async () => {
    try {
      const d = await api.plugins.uninstalled();
      setPlugins(d.plugins || []);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  }, [t]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    setError("");
    try {
      await load();
    } finally {
      setRefreshing(false);
    }
  }, [load]);

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const sourceOptions = useMemo(
    () => Array.from(new Set(plugins.map((p) => p.source))).sort(),
    [plugins],
  );

  const filterGroups = useMemo(
    () =>
      [
        {
          key: "source",
          label: t("plugins.filterBySource"),
          options: sourceOptions,
        },
      ].filter((g) => g.options.length > 0),
    [t, sourceOptions],
  );

  const sortOptions = useMemo(
    () => SORT_SPECS.map((s) => ({ key: s.key, label: t(s.labelKey) })),
    [t],
  );

  const searchPlaceholder = t("plugins.searchUninstalled");

  const toolbarNode = useMemo(
    () => (
      <div className="flex items-center gap-2">
        <PluginToolbar
          filterGroups={filterGroups}
          filters={filters}
          onFilterToggle={toggleFilter}
          onFiltersClear={() => setFilters({ ...INITIAL_FILTERS })}
          searchValue={search}
          onSearch={setSearch}
          searchPlaceholder={searchPlaceholder}
          sortOptions={sortOptions}
          sortBy={sortBy}
          onSortBy={setSortBy}
        />
        <PluginRefreshButton label={t("plugins.refresh")} busy={refreshing} onClick={refresh} />
      </div>
    ),
    [
      filterGroups,
      filters,
      search,
      sortBy,
      sortOptions,
      searchPlaceholder,
      refreshing,
      refresh,
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

  const run = async (id: string, action: () => Promise<unknown>) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
    setError("");
    try {
      await action();
      await load();
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setBusyIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  };

  return (
    <div className="space-y-4">
      {error && <p className="text-xs text-red-400">{error}</p>}

      {loading && (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      )}
      {!loading && filtered.length === 0 && (
        <p className="text-sm text-zinc-500 py-8 text-center">
          {plugins.length === 0 ? t("plugins.noUninstalled") : t("plugins.noMatch")}
        </p>
      )}
      {!loading && filtered.length > 0 && (
        <div className="grid gap-4 grid-cols-[repeat(auto-fill,minmax(230px,1fr))]">
          {filtered.map((p) => {
            const sm = sourceInfo(p.source, repoOfficial.get(p.source));
            return (
              <PluginCard
                key={p.id}
                name={p.name}
                version={p.version}
                verified={sm.kind}
                sourceLabel={sm.isLocal ? t("plugins.sourceLocal") : p.source}
                description={p.description}
                author={p.author}
                downloads={p.downloads}
                menuLabel={t("plugins.menuLabel")}
                menuItems={
                  <>
                    <MenuItem
                      icon={<PackagePlus className="w-4 h-4" />}
                      disabled={busyIds.has(p.id)}
                      onClick={() => run(p.id, () => api.plugins.installPlugin(p.id))}
                    >
                      {t("plugins.installPlugin")}
                    </MenuItem>
                    <MenuItem
                      danger
                      icon={<Trash2 className="w-4 h-4" />}
                      disabled={busyIds.has(p.id)}
                      onClick={() => {
                        if (confirm(t("plugins.deletePluginConfirm", { name: p.name }))) {
                          run(p.id, () => api.plugins.remove(p.id));
                        }
                      }}
                    >
                      {t("plugins.deletePlugin")}
                    </MenuItem>
                  </>
                }
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
