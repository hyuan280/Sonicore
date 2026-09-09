import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Modal } from "../../components/ui/modal";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { api } from "../../api/client";
import { Plus, Trash2, Loader2, ShieldCheck } from "lucide-react";
import type { PluginRepo } from "../../types";

export function RepoSettingsModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const [repos, setRepos] = useState<PluginRepo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [newUrl, setNewUrl] = useState("");
  const [adding, setAdding] = useState(false);
  const [deletingUrl, setDeletingUrl] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    api.plugins
      .repos()
      .then((d) => {
        if (active) setRepos(d?.repos || []);
      })
      .catch((err: unknown) => {
        if (active) setError(translateApiError(t, err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const addRepo = async () => {
    const url = newUrl.trim();
    if (!url) return;
    try {
      const parsed = new URL(url);
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
        setError(t("plugins.repoInvalidUrl"));
        return;
      }
    } catch {
      setError(t("plugins.repoInvalidUrl"));
      return;
    }
    if (repos.some((r) => r.url === url)) {
      setError(t("plugins.repoDuplicate"));
      return;
    }
    setAdding(true);
    setError("");
    try {
      await api.plugins.addRepo(url);
      setRepos((prev) =>
        prev.some((r) => r.url === url) ? prev : [...prev, { name: url, url, official: false }],
      );
      setNewUrl("");
    } catch (err) {
      setError(translateApiError(t, err));
    } finally {
      setAdding(false);
    }
  };

  const removeRepo = async (repo: PluginRepo) => {
    if (deletingUrl || !confirm(t("plugins.repoRemoveConfirm", { url: repo.url }))) return;
    setDeletingUrl(repo.url);
    setError("");
    try {
      await api.plugins.removeRepo(repo.url);
      setRepos((prev) => prev.filter((r) => r.url !== repo.url));
    } catch (err) {
      setError(translateApiError(t, err));
    } finally {
      setDeletingUrl(null);
    }
  };

  return (
    <Modal title={t("plugins.repoSettings")} onClose={onClose}>
      <div className="space-y-4">
        <div>
          <p className="text-xs text-zinc-400 mb-2">{t("plugins.repoListDesc")}</p>
          {loading ? (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
            </div>
          ) : repos.length === 0 ? (
            <p className="text-sm text-zinc-500 py-2">{t("plugins.noRepos")}</p>
          ) : (
            <div className="space-y-2">
              {repos.map((repo) => (
                <div
                  key={repo.url}
                  className="flex items-center gap-2 rounded-lg bg-zinc-800 px-3 py-2"
                >
                  <ShieldCheck
                    className={`w-4 h-4 shrink-0 ${repo.official ? "text-green-500" : "text-zinc-500"}`}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm truncate">{repo.name}</div>
                    <div className="text-xs text-zinc-500 truncate">{repo.url}</div>
                  </div>
                  {repo.official && (
                    <span className="text-xs text-green-400 shrink-0">
                      {t("plugins.repoOfficial")}
                    </span>
                  )}
                  {!repo.official && (
                    <button
                      type="button"
                      onClick={() => removeRepo(repo)}
                      disabled={deletingUrl === repo.url}
                      aria-label={t("plugins.repoRemove")}
                      className="p-1.5 rounded text-zinc-400 hover:text-red-400 hover:bg-zinc-700 cursor-pointer shrink-0 disabled:opacity-50"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <div>
          <p className="text-xs text-zinc-400 mb-1">{t("plugins.repoAddLabel")}</p>
          <div className="flex gap-2">
            <Input
              value={newUrl}
              onChange={(e) => {
                setNewUrl(e.target.value);
                setError("");
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") addRepo();
              }}
              placeholder={t("plugins.repoAddPlaceholder")}
              disabled={adding}
            />
            <Button onClick={addRepo} disabled={adding || !newUrl.trim()}>
              <Plus className="w-4 h-4" />
              {t("plugins.repoAdd")}
            </Button>
          </div>
        </div>

        {error && <p className="text-xs text-red-400">{error}</p>}

        <div className="border-t border-zinc-800 pt-4">
          <p className="text-xs text-zinc-500">{t("plugins.moreSettingsComing")}</p>
        </div>
      </div>
    </Modal>
  );
}
