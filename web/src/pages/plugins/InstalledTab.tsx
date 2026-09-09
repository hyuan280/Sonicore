import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import { translateApiError } from "../../i18n/errorCodes";
import { Card, CardGrid } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import PluginToolbar from "./PluginToolbar";
import { api } from "../../api/client";
import {
  Puzzle,
  ChevronDown,
  Loader2,
  Trash2,
  RefreshCw,
  ShieldCheck,
  ShieldQuestion,
} from "lucide-react";
import { cn } from "../../lib/utils";
import {
  PLUGIN_SOURCE,
  PLUGIN_INSTALLED_FILTER_KEYS,
  PLUGIN_INSTALLED_SORT_KEYS,
} from "../../lib/constants";
import type { PluginInstance, PluginStatus, PluginsOutletContext } from "../../types";

const STATUS_ORDER: PluginStatus[] = ["ok", "disabled", "error"];

function statusLabel(t: (k: string) => string, status: PluginStatus): string {
  switch (status) {
    case "ok":
      return t("plugins.statusOk");
    case "disabled":
      return t("plugins.statusDisabled");
    case "error":
      return t("plugins.statusError");
  }
}

function statusBadge(status: PluginStatus) {
  const base = "text-xs px-2 py-0.5 rounded-full shrink-0";
  switch (status) {
    case "ok":
      return <span className={cn(base, "bg-green-600/20 text-green-400")}>●</span>;
    case "disabled":
      return <span className={cn(base, "bg-zinc-700/60 text-zinc-400")}>●</span>;
    case "error":
      return <span className={cn(base, "bg-red-600/20 text-red-400")}>●</span>;
  }
}

function sourceMeta(source: string): { icon: React.ReactNode; labelKey: string } {
  if (source === PLUGIN_SOURCE.official) {
    return {
      icon: <ShieldCheck className="w-3 h-3 text-green-500" />,
      labelKey: "plugins.sourceOfficial",
    };
  }
  if (source === PLUGIN_SOURCE.third_party) {
    return {
      icon: <ShieldCheck className="w-3 h-3 text-zinc-500" />,
      labelKey: "plugins.sourceThirdParty",
    };
  }
  return {
    icon: <ShieldQuestion className="w-3 h-3 text-yellow-500" />,
    labelKey: "plugins.sourceUnverified",
  };
}

export default function InstalledTab() {
  const { t } = useTranslation();
  const { setToolbar } = useOutletContext<PluginsOutletContext>();
  const [plugins, setPlugins] = useState<PluginInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Record<string, string[]>>({
    [PLUGIN_INSTALLED_FILTER_KEYS.status]: [],
    [PLUGIN_INSTALLED_FILTER_KEYS.source]: [],
  });
  const [sortBy, setSortBy] = useState("");
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    loadPlugins();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadPlugins = async () => {
    try {
      const d = await api.plugins.installed();
      setPlugins(d.plugins || []);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  };

  const statusOptions = useMemo(
    () =>
      Array.from(new Set(plugins.map((p) => p.status))).sort(
        (a, b) => STATUS_ORDER.indexOf(a) - STATUS_ORDER.indexOf(b),
      ),
    [plugins],
  );
  const sourceOptions = useMemo(
    () => Array.from(new Set(plugins.map((p) => p.source))).sort(),
    [plugins],
  );

  const filtered = useMemo(() => {
    let list = plugins.filter((p) => {
      const q = search.trim().toLowerCase();
      if (q && !p.name.toLowerCase().includes(q)) return false;
      if (
        filters[PLUGIN_INSTALLED_FILTER_KEYS.status].length > 0 &&
        !filters[PLUGIN_INSTALLED_FILTER_KEYS.status].includes(p.status)
      )
        return false;
      if (
        filters[PLUGIN_INSTALLED_FILTER_KEYS.source].length > 0 &&
        !filters[PLUGIN_INSTALLED_FILTER_KEYS.source].includes(p.source)
      )
        return false;
      return true;
    });
    if (sortBy === PLUGIN_INSTALLED_SORT_KEYS.name) {
      list = [...list].sort((a, b) => a.name.localeCompare(b.name));
    } else if (sortBy === PLUGIN_INSTALLED_SORT_KEYS.status) {
      list = [...list].sort(
        (a, b) => STATUS_ORDER.indexOf(a.status) - STATUS_ORDER.indexOf(b.status),
      );
    } else if (sortBy === PLUGIN_INSTALLED_SORT_KEYS.updatedAt) {
      list = [...list].sort((a, b) => (b.updated_at || "").localeCompare(a.updated_at || ""));
    }
    return list;
  }, [plugins, search, filters, sortBy]);

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const runAction = async (id: string, action: () => Promise<unknown>) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
    setError("");
    try {
      await action();
      await loadPlugins();
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

  const filterGroups = useMemo(
    () =>
      [
        {
          key: PLUGIN_INSTALLED_FILTER_KEYS.status,
          label: t("plugins.filterByStatus"),
          options: statusOptions,
        },
        {
          key: PLUGIN_INSTALLED_FILTER_KEYS.source,
          label: t("plugins.filterBySource"),
          options: sourceOptions,
        },
      ].filter((g) => g.options.length > 0),
    [t, statusOptions, sourceOptions],
  );
  const sortOptions = useMemo(
    () => [
      { key: PLUGIN_INSTALLED_SORT_KEYS.name, label: t("plugins.sortByName") },
      { key: PLUGIN_INSTALLED_SORT_KEYS.status, label: t("plugins.sortByStatus") },
      { key: PLUGIN_INSTALLED_SORT_KEYS.updatedAt, label: t("plugins.sortByUpdatedAt") },
    ],
    [t],
  );
  const searchPlaceholder = t("plugins.searchInstalled");

  const toolbarNode = useMemo(
    () => (
      <PluginToolbar
        filterGroups={filterGroups}
        filters={filters}
        onFilterToggle={(group, value) =>
          setFilters((prev) => {
            const cur = prev[group] || [];
            const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
            return { ...prev, [group]: next };
          })
        }
        onFiltersClear={() =>
          setFilters({
            [PLUGIN_INSTALLED_FILTER_KEYS.status]: [],
            [PLUGIN_INSTALLED_FILTER_KEYS.source]: [],
          })
        }
        searchValue={search}
        onSearch={setSearch}
        searchPlaceholder={searchPlaceholder}
        sortOptions={sortOptions}
        sortBy={sortBy}
        onSortBy={setSortBy}
      />
    ),
    [filterGroups, filters, search, sortBy, sortOptions, searchPlaceholder],
  );

  useEffect(() => {
    setToolbar(toolbarNode);
    return () => setToolbar(null);
  }, [toolbarNode, setToolbar]);

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
          {plugins.length === 0 ? t("plugins.noInstalled") : t("plugins.noMatch")}
        </p>
      )}
      {!loading && filtered.length > 0 && (
        <CardGrid className="grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
          {filtered.map((p) => {
            const isExpanded = expanded.has(p.id);
            const isBusy = busyIds.has(p.id);
            const sm = sourceMeta(p.source);
            return (
              <Card key={p.id} className="p-0 overflow-hidden">
                <button
                  onClick={() => toggleExpanded(p.id)}
                  className="w-full flex items-center gap-3 p-4 text-left cursor-pointer hover:bg-zinc-800/40 transition-colors"
                >
                  <div className="w-10 h-10 rounded-lg bg-zinc-800 flex items-center justify-center shrink-0">
                    <Puzzle className="w-5 h-5 text-green-500" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium truncate">{p.name}</span>
                      {statusBadge(p.status)}
                    </div>
                    <div className="flex items-center gap-2 text-xs text-zinc-500 mt-0.5">
                      <span>v{p.version}</span>
                      <span>·</span>
                      <span className="flex items-center gap-1">
                        {sm.icon}
                        {t(sm.labelKey)}
                      </span>
                      <span>·</span>
                      <span>{statusLabel(t, p.status)}</span>
                    </div>
                  </div>
                  <ChevronDown
                    className={cn(
                      "w-4 h-4 text-zinc-500 shrink-0 transition-transform",
                      isExpanded && "rotate-180",
                    )}
                  />
                </button>

                {isExpanded && (
                  <div className="border-t border-zinc-800 px-4 py-3 space-y-3">
                    {p.status_msg && <p className="text-xs text-red-400">{p.status_msg}</p>}
                    <div>
                      <p className="text-xs text-zinc-400 mb-1">{t("plugins.config")}</p>
                      <p className="text-sm text-zinc-500">{t("plugins.noConfigYet")}</p>
                    </div>
                    <div className="flex items-center gap-2 flex-wrap">
                      {p.status === "disabled" ? (
                        <Button
                          size="sm"
                          disabled={isBusy}
                          onClick={() => runAction(p.id, () => api.plugins.setEnabled(p.id, true))}
                        >
                          {t("plugins.enable")}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={isBusy}
                          onClick={() => runAction(p.id, () => api.plugins.setEnabled(p.id, false))}
                        >
                          {t("plugins.disable")}
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={isBusy}
                        onClick={() => runAction(p.id, () => api.plugins.update(p.id))}
                      >
                        <RefreshCw className="w-3.5 h-3.5" />
                        {t("plugins.update")}
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        disabled={isBusy}
                        onClick={() => {
                          if (confirm(t("plugins.uninstallConfirm", { name: p.name }))) {
                            runAction(p.id, () => api.plugins.uninstall(p.id));
                          }
                        }}
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                        {t("plugins.uninstall")}
                      </Button>
                    </div>
                  </div>
                )}
              </Card>
            );
          })}
        </CardGrid>
      )}
    </div>
  );
}
