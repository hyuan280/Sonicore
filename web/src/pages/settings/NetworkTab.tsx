import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Card } from "../../components/ui/card";
import { Input } from "../../components/ui/input";
import { Button } from "../../components/ui/button";
import { api } from "../../api/client";
import { Globe, Network, Loader2 } from "lucide-react";

// useProxyUrl encapsulates one proxy-URL form: value + dirty tracking, save
// (with error handling) and revert. The two proxy cards share it.
function useProxyUrl(saveValue: (url: string) => Promise<unknown>) {
  const { t } = useTranslation();
  const [value, setValue] = useState("");
  const [init, setInit] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const dirty = value !== init;

  const load = (v: string) => {
    setValue(v);
    setInit(v);
  };

  const change = (v: string) => {
    setValue(v);
    setError("");
  };

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      await saveValue(value);
      setInit(value);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setSaving(false);
    }
  };

  const revert = () => {
    setValue(init);
    setError("");
  };

  return { value, init, saving, error, setError, dirty, load, change, save, revert };
}

export default function NetworkTab() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const proxy = useProxyUrl((url) => api.admin.updateSettings({ network_proxy_url: url }));
  const githubProxy = useProxyUrl((url) =>
    api.admin.updateSettings({ network_github_proxy_url: url }),
  );
  const [githubUseGlobal, setGithubUseGlobal] = useState(false);
  const [githubSaving, setGithubSaving] = useState(false);
  const [githubError, setGithubError] = useState("");

  // Load once on mount. Deliberately not keyed on `t` so a language switch
  // never re-fetches and overwrites in-progress edits. The `cancelled` flag
  // guards against StrictMode's double effect and state updates after unmount.
  useEffect(() => {
    let cancelled = false;
    api.admin
      .getSettings()
      .then((s) => {
        if (cancelled) return;
        proxy.load(s.network_proxy_url || "");
        githubProxy.load(s.network_github_proxy_url || "");
        setGithubUseGlobal(s.network_github_proxy_use_global ?? false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        // Both cards must surface a load failure, otherwise the GitHub card
        // renders empty defaults that could be mistaken for "no config".
        const msg = translateApiError(t, err);
        proxy.setError(msg);
        githubProxy.setError(msg);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const toggleGithubProxy = async () => {
    const next = !githubUseGlobal;
    setGithubUseGlobal(next);
    setGithubSaving(true);
    setGithubError("");
    try {
      await api.admin.updateSettings({ network_github_proxy_use_global: next });
    } catch (err: unknown) {
      setGithubUseGlobal(!next);
      setGithubError(translateApiError(t, err));
    } finally {
      setGithubSaving(false);
    }
  };

  // "Use global proxy" only has an effect when a global proxy is actually
  // configured. With no global proxy the switch is disabled and shown as off,
  // and the GitHub card falls back to its own proxy URL.
  const globalConfigured = proxy.init !== "";
  const useGlobalActive = githubUseGlobal && globalConfigured;

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <Globe className="w-4 h-4" /> {t("network.proxyServer")}
        </h3>
        <p className="text-xs text-zinc-500">{t("network.proxyServerDesc")}</p>
        <div>
          <p className="text-xs text-zinc-400 mb-1">{t("network.proxyUrl")}</p>
          <Input
            value={proxy.value}
            onChange={(e) => proxy.change(e.target.value)}
            placeholder={t("network.proxyUrlPlaceholder")}
            disabled={proxy.saving}
          />
        </div>
        {proxy.error && <p className="text-xs text-red-400">{proxy.error}</p>}
        {proxy.dirty && (
          <div className="flex items-center gap-3">
            <Button size="sm" onClick={proxy.save} disabled={proxy.saving}>
              {proxy.saving ? t("admin.saving") : t("admin.save")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="bg-zinc-800"
              onClick={proxy.revert}
              disabled={proxy.saving}
            >
              {t("admin.revert")}
            </Button>
          </div>
        )}
      </Card>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <Network className="w-4 h-4" /> {t("network.githubProxy")}
        </h3>
        <p className="text-xs text-zinc-500">{t("network.githubProxyDesc")}</p>
        <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
          <div>
            <p className="text-sm font-medium">{t("network.useGlobalProxy")}</p>
            <p className="text-xs text-zinc-400">
              {globalConfigured
                ? t("network.useGlobalProxyDesc")
                : t("network.useGlobalNeedsProxy")}
            </p>
          </div>
          <button
            type="button"
            role="switch"
            aria-label={t("network.useGlobalProxy")}
            aria-checked={useGlobalActive}
            onClick={toggleGithubProxy}
            disabled={githubSaving || !globalConfigured}
            className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${useGlobalActive ? "bg-green-600" : "bg-zinc-700"}`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${useGlobalActive ? "translate-x-6" : ""}`}
            />
          </button>
        </div>
        {!useGlobalActive && (
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("network.githubProxyUrl")}</p>
            <Input
              value={githubProxy.value}
              onChange={(e) => githubProxy.change(e.target.value)}
              placeholder={t("network.githubProxyUrlPlaceholder")}
              disabled={githubProxy.saving}
            />
          </div>
        )}
        {!useGlobalActive && githubProxy.dirty && (
          <div className="flex items-center gap-3">
            <Button size="sm" onClick={githubProxy.save} disabled={githubProxy.saving}>
              {githubProxy.saving ? t("admin.saving") : t("admin.save")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="bg-zinc-800"
              onClick={githubProxy.revert}
              disabled={githubProxy.saving}
            >
              {t("admin.revert")}
            </Button>
          </div>
        )}
        {githubError && <p className="text-xs text-red-400">{githubError}</p>}
        {githubProxy.error && <p className="text-xs text-red-400">{githubProxy.error}</p>}
      </Card>
    </div>
  );
}
