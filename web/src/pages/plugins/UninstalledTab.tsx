import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { MenuItem } from "../../components/ui/menu";
import { PluginCard, sourceInfo } from "./PluginCard";
import { useRepoOfficialMap } from "../../hooks/useRepoOfficialMap";
import { api } from "../../api/client";
import { Loader2, Trash2, PackagePlus } from "lucide-react";
import type { PluginInstance } from "../../types";

// UninstalledTab lists plugins that were discovered on disk but are not
// installed yet (or were soft-uninstalled). Each card's menu offers exactly
// two actions: install (moves the plugin to the installed tab, disabled by
// default) and delete (removes the plugin files and the DB record).
export default function UninstalledTab() {
  const { t } = useTranslation();
  const [plugins, setPlugins] = useState<PluginInstance[]>([]);
  const repoOfficial = useRepoOfficialMap();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());

  const load = async () => {
    try {
      const d = await api.plugins.uninstalled();
      setPlugins(d.plugins || []);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
      {!loading && plugins.length === 0 && (
        <p className="text-sm text-zinc-500 py-8 text-center">{t("plugins.noUninstalled")}</p>
      )}
      {!loading && plugins.length > 0 && (
        <div className="grid gap-4 grid-cols-[repeat(auto-fill,minmax(230px,1fr))]">
          {plugins.map((p) => {
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
