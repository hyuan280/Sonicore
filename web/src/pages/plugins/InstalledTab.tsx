import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import { translateApiError } from "../../i18n/errorCodes";
import { Button } from "../../components/ui/button";
import { Modal } from "../../components/ui/modal";
import { SchemaRenderer } from "../../components/ui/schema";
import { MenuItem } from "../../components/ui/menu";
import PluginToolbar from "./PluginToolbar";
import LogsModal from "./LogsModal";
import { PluginCard, sourceInfo } from "./PluginCard";
import { useRepoOfficialMap } from "../../hooks/useRepoOfficialMap";
import { api } from "../../api/client";
import {
  Loader2,
  Trash2,
  RefreshCw,
  Settings,
  LayoutDashboard,
  History,
  ScrollText,
} from "lucide-react";
import { cn } from "../../lib/utils";
import { PLUGIN_INSTALLED_FILTER_KEYS, PLUGIN_INSTALLED_SORT_KEYS } from "../../lib/constants";
import type { PluginInstance, PluginStatus, UINode, PluginsOutletContext } from "../../types";

const STATUS_ORDER: PluginStatus[] = ["ok", "disabled", "error"];

const STATUS_LABEL_KEYS: Record<PluginStatus, string> = {
  ok: "plugins.statusOk",
  disabled: "plugins.statusDisabled",
  error: "plugins.statusError",
};

export default function InstalledTab() {
  const { t } = useTranslation();
  const { setToolbar } = useOutletContext<PluginsOutletContext>();
  const [plugins, setPlugins] = useState<PluginInstance[]>([]);
  const repoOfficial = useRepoOfficialMap();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  // detail selects the plugin detail modal (data/config views). The enable
  // switch lives in the modal's title row, so there is no separate settings
  // dialog anymore.
  const [detail, setDetail] = useState<{ id: string; view: "page" | "config" } | null>(null);
  // historyId opens the version-history dialog of one plugin.
  const [historyId, setHistoryId] = useState<string | null>(null);
  // logId opens the live log viewer of one plugin.
  const [logId, setLogId] = useState<string | null>(null);
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

  const detailPlugin = detail ? plugins.find((p) => p.id === detail.id) : undefined;
  const historyPlugin = historyId ? plugins.find((p) => p.id === historyId) : undefined;
  const logPlugin = logId ? plugins.find((p) => p.id === logId) : undefined;

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
        // Even-division grid, same behavior as the reference project's
        // JS-computed progressive grid (repeat(N, minmax(0, 1fr))) but done
        // in pure CSS: auto-fill computes N from the 230px minimum, so each
        // card is exactly 1/N of the page width (~230–300px on desktop).
        // Shrinking the page narrows the cards first; once a column can no
        // longer hold 230px it is dropped and the remaining cards widen to
        // an even split again. auto-fill keeps the reserved slots when there
        // are fewer cards than columns, so a lone card never stretches.
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
                status={p.status}
                statusLabel={t(STATUS_LABEL_KEYS[p.status])}
                description={p.description}
                author={p.author}
                downloads={p.downloads}
                badge={
                  p.update_available ? (
                    <span className="text-[9px] font-bold text-orange-400 border border-orange-500/40 bg-orange-500/10 px-1 py-px rounded shrink-0">
                      NEW
                    </span>
                  ) : undefined
                }
                menuLabel={t("plugins.menuLabel")}
                onClick={() => setDetail({ id: p.id, view: p.has_page ? "page" : "config" })}
                menuItems={
                  <>
                    {p.has_page && (
                      <MenuItem
                        icon={<LayoutDashboard className="w-4 h-4" />}
                        onClick={() => setDetail({ id: p.id, view: "page" })}
                      >
                        {t("plugins.dataPanel")}
                      </MenuItem>
                    )}
                    <MenuItem
                      icon={<Settings className="w-4 h-4" />}
                      onClick={() => setDetail({ id: p.id, view: "config" })}
                    >
                      {t("plugins.settings")}
                    </MenuItem>
                    <MenuItem
                      icon={<ScrollText className="w-4 h-4" />}
                      onClick={() => setLogId(p.id)}
                    >
                      {t("plugins.viewLogs")}
                    </MenuItem>
                    {p.update_available ? (
                      <MenuItem
                        icon={<RefreshCw className="w-4 h-4" />}
                        disabled={busyIds.has(p.id)}
                        onClick={() => setHistoryId(p.id)}
                      >
                        {t("plugins.update")}
                      </MenuItem>
                    ) : (
                      <MenuItem
                        icon={<History className="w-4 h-4" />}
                        onClick={() => setHistoryId(p.id)}
                      >
                        {t("plugins.versionHistory")}
                      </MenuItem>
                    )}
                    <MenuItem
                      danger
                      icon={<Trash2 className="w-4 h-4" />}
                      disabled={busyIds.has(p.id)}
                      onClick={() => {
                        if (confirm(t("plugins.uninstallConfirm", { name: p.name }))) {
                          runAction(p.id, () => api.plugins.uninstall(p.id));
                        }
                      }}
                    >
                      {t("plugins.uninstall")}
                    </MenuItem>
                  </>
                }
              />
            );
          })}
        </div>
      )}

      {detailPlugin && detail && (
        <PluginDetailModal
          plugin={detailPlugin}
          initialView={detail.view}
          onClose={() => setDetail(null)}
          onEnabledChanged={loadPlugins}
        />
      )}
      {historyPlugin && (
        <VersionHistoryModal
          plugin={historyPlugin}
          onClose={() => setHistoryId(null)}
          onUpdated={loadPlugins}
        />
      )}
      {logPlugin && <LogsModal plugin={logPlugin} onClose={() => setLogId(null)} />}
    </div>
  );
}

// VersionHistoryModal shows the plugin's changelog from the manifest
// [[plugin.history]] entries. When an update is available the bottom of the
// dialog offers an "update to latest version" action.
function VersionHistoryModal({
  plugin,
  onClose,
  onUpdated,
}: {
  plugin: PluginInstance;
  onClose: () => void;
  onUpdated: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState("");
  const entries = plugin.history || [];

  const install = async () => {
    setInstalling(true);
    setError("");
    try {
      await api.plugins.update(plugin.id);
      await onUpdated();
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setInstalling(false);
    }
  };

  return (
    <Modal title={`${plugin.name} ${t("plugins.versionHistory")}`} onClose={onClose}>
      <div className="space-y-3">
        {error && <p className="text-xs text-red-400">{error}</p>}
        {entries.length === 0 ? (
          <p className="text-sm text-zinc-500">{t("plugins.noHistory")}</p>
        ) : (
          entries.map((h, i) => (
            <div key={i} className="border border-zinc-800 rounded-lg p-3">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">v{h.version}</span>
                {h.date && <span className="text-xs text-zinc-500">{h.date}</span>}
              </div>
              {h.description && <p className="text-xs text-zinc-400 mt-1">{h.description}</p>}
            </div>
          ))
        )}
        {plugin.update_available && (
          <div className="flex justify-end border-t border-zinc-800 pt-3">
            <Button size="sm" onClick={install} disabled={installing}>
              <RefreshCw className={cn("w-3.5 h-3.5", installing && "animate-spin")} />
              {installing ? t("plugins.installing") : t("plugins.updateToLatest")}
            </Button>
          </div>
        )}
      </div>
    </Modal>
  );
}

// PluginDetailModal shows a plugin's data page (get_page assembly) and its
// config form (get_form assembly). The data page is the default view; when
// the plugin provides none, the config form is shown instead. Navigation
// between the two lives in the bottom bar: the data view offers a settings
// button on the left (→ config), the config view offers a data button on
// the left (only when a page exists) and save on the right. The enable
// switch sits in the modal's title row (headerAction), right before the
// close button.
function PluginDetailModal({
  plugin,
  initialView,
  onClose,
  onEnabledChanged,
}: {
  plugin: PluginInstance;
  initialView?: "page" | "config";
  onClose: () => void;
  // onEnabledChanged reloads the plugin list after the toggle succeeded.
  onEnabledChanged: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<UINode | null>(null);
  const [page, setPage] = useState<UINode | null>(null);
  const [view, setView] = useState<"page" | "config">("page");
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [initValues, setInitValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const [toggleBusy, setToggleBusy] = useState(false);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [f, p, c] = await Promise.all([
        api.plugins.getForm(plugin.id),
        api.plugins.getPage(plugin.id),
        api.plugins.getConfig(plugin.id),
      ]);
      setForm(f.schema || null);
      setPage(p.schema || null);
      setValues(c.config || {});
      setInitValues(c.config || {});
      setView(initialView === "config" || !p.schema ? "config" : "page");
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin.id]);

  const toggleEnabled = async () => {
    setToggleBusy(true);
    setError("");
    const enabling = !plugin.enabled;
    try {
      await api.plugins.setEnabled(plugin.id, enabling);
      await onEnabledChanged();
      if (enabling) {
        await load();
      }
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setToggleBusy(false);
    }
  };

  const refreshPage = async () => {
    setPageLoading(true);
    setError("");
    try {
      const p = await api.plugins.getPage(plugin.id);
      setPage(p.schema || null);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setPageLoading(false);
    }
  };

  // Deep-compare via JSON: config values may be objects/arrays whose
  // references change on every onChange (e.g. VSelect multiple), so a
  // shallow !== would misreport them as dirty.
  const dirty = form
    ? collectModels(form).some(
        (key) => JSON.stringify(values[key]) !== JSON.stringify(initValues[key]),
      )
    : false;

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await api.plugins.updateConfig(plugin.id, values);
      setInitValues(values);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setSaving(false);
    }
  };

  let modalTitle = plugin.name;
  if (view === "config") {
    modalTitle = `${plugin.name} ${t("plugins.settings")}`;
  }

  // renderContent picks the body view without nested ternaries:
  // loading → disabled notice → data page → config form.
  const renderContent = () => {
    if (loading) {
      return (
        <div className="flex items-center justify-center py-6">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      );
    }
    if (!plugin.enabled) {
      return (
        <p className="text-sm text-zinc-500 py-6 text-center">{t("plugins.pluginDisabled")}</p>
      );
    }
    if (view === "page") {
      if (page) {
        return <SchemaRenderer node={page} />;
      }
      return <p className="text-sm text-zinc-500 py-2">{t("plugins.noPageData")}</p>;
    }
    if (form) {
      return (
        <SchemaRenderer
          node={form}
          values={values}
          onChange={(key, value) => {
            setValues((prev) => ({ ...prev, [key]: value }));
            setError("");
          }}
          disabled={saving}
        />
      );
    }
    return <p className="text-sm text-zinc-500">{t("plugins.noConfigYet")}</p>;
  };

  return (
    <Modal
      title={modalTitle}
      titleExtra={
        error ? (
          <p className="text-xs text-red-400 max-w-60 truncate" title={error}>
            {error}
          </p>
        ) : undefined
      }
      onClose={onClose}
      className="max-w-3xl"
      contentClassName="pb-1"
      headerAction={
        <button
          type="button"
          role="switch"
          aria-checked={!!plugin.enabled}
          title={t("plugins.enableSwitch")}
          onClick={toggleEnabled}
          disabled={toggleBusy}
          className={cn(
            "w-11 h-6 rounded-full transition-colors relative shrink-0 cursor-pointer disabled:opacity-50",
            plugin.enabled ? "bg-green-600" : "bg-zinc-700",
          )}
        >
          <span
            className={cn(
              "absolute top-0.5 w-5 h-5 rounded-full bg-white transition-all",
              plugin.enabled ? "left-[22px]" : "left-0.5",
            )}
          />
        </button>
      }
    >
      <div className="space-y-4">
        {plugin.status_msg && <p className="text-xs text-red-400">{plugin.status_msg}</p>}

        {renderContent()}

        {!loading && plugin.enabled && (
          <div className="flex items-center justify-between border-t border-zinc-800 pt-1">
            {view === "page" ? (
              <>
                <Button size="sm" variant="ghost" onClick={() => setView("config")}>
                  <Settings className="w-3.5 h-3.5" />
                  {t("plugins.settings")}
                </Button>
                {page && (
                  <Button size="sm" onClick={refreshPage} disabled={pageLoading}>
                    <RefreshCw className={cn("w-3.5 h-3.5", pageLoading && "animate-spin")} />
                    {t("plugins.refresh")}
                  </Button>
                )}
              </>
            ) : (
              <>
                <div className="flex items-center gap-2">
                  {page && (
                    <Button size="sm" variant="ghost" onClick={() => setView("page")}>
                      <LayoutDashboard className="w-3.5 h-3.5" />
                      {t("plugins.pageData")}
                    </Button>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {dirty && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setValues(initValues)}
                      disabled={saving}
                    >
                      {t("admin.revert")}
                    </Button>
                  )}
                  <Button size="sm" onClick={save} disabled={saving || !dirty}>
                    {saving ? t("common.saving") : t("common.save")}
                  </Button>
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}

// collectModels walks a UI tree and returns every field model (config
// key), used for dirty detection.
function collectModels(node: UINode): string[] {
  const out: string[] = [];
  const walk = (n: UINode) => {
    if (typeof n.props?.model === "string" && n.props.model !== "") {
      out.push(n.props.model);
    }
    (n.content || []).forEach(walk);
  };
  walk(node);
  return out;
}
