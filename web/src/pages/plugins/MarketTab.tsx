import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useOutletContext } from "react-router-dom";
import { translateApiError } from "../../i18n/errorCodes";
import { Button } from "../../components/ui/button";
import { Modal } from "../../components/ui/modal";
import { MenuItem } from "../../components/ui/menu";
import PluginToolbar from "./PluginToolbar";
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

const DEFAULT_FILTERS = { author: [], tag: [], repo: [] };

export default function MarketTab() {
  const { t } = useTranslation();
  const { setToolbar } = useOutletContext<PluginsOutletContext>();
  const [plugins, setPlugins] = useState<PluginCatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [installError, setInstallError] = useState("");
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Record<string, string[]>>(DEFAULT_FILTERS);
  const [sortBy, setSortBy] = useState("");
  const [selected, setSelected] = useState<PluginCatalogEntry | null>(null);
  const [historyEntry, setHistoryEntry] = useState<PluginCatalogEntry | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);

  useEffect(() => {
    loadMarket();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadMarket = async () => {
    try {
      const d = await api.plugins.market();
      setPlugins(d.plugins || []);
    } catch (err: unknown) {
      setLoadError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  };

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

  const filtered = useMemo(() => {
    let list = plugins.filter((p) => {
      const q = search.trim().toLowerCase();
      if (
        q &&
        ![p.name, p.description || "", p.author || "", ...(p.tags || [])]
          .join(" ")
          .toLowerCase()
          .includes(q)
      )
        return false;
      if (filters.author.length > 0 && (!p.author || !filters.author.includes(p.author)))
        return false;
      if (filters.repo.length > 0 && !filters.repo.includes(p.repo)) return false;
      if (filters.tag.length > 0 && !(p.tags || []).some((tag) => filters.tag.includes(tag)))
        return false;
      return true;
    });
    const sorted = [...list];
    switch (sortBy) {
      case "name":
        sorted.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case "downloads":
        sorted.sort((a, b) => (b.downloads ?? -1) - (a.downloads ?? -1));
        break;
      case "author":
        sorted.sort((a, b) => (a.author || "").localeCompare(b.author || ""));
        break;
      case "tag":
        sorted.sort((a, b) => (a.tags || []).join(",").localeCompare((b.tags || []).join(",")));
        break;
      case "repo":
        sorted.sort((a, b) => a.repo.localeCompare(b.repo));
        break;
      case "updated_at":
        sorted.sort((a, b) => (b.updated_at || "").localeCompare(a.updated_at || ""));
        break;
    }
    return sorted;
  }, [plugins, search, filters, sortBy]);

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
    () => [
      { key: "name", label: t("plugins.sortByName") },
      { key: "downloads", label: t("plugins.sortByDownloads") },
      { key: "author", label: t("plugins.sortByAuthor") },
      { key: "tag", label: t("plugins.sortByTag") },
      { key: "repo", label: t("plugins.sortByRepo") },
      { key: "updated_at", label: t("plugins.sortByUpdatedAt") },
    ],
    [t],
  );
  const searchPlaceholder = t("plugins.searchMarket");

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
        onFiltersClear={() => setFilters({ ...DEFAULT_FILTERS })}
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
      {loadError && <p className="text-xs text-red-400">{loadError}</p>}

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
