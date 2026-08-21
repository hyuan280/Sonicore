import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../i18n/errorCodes";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { getRole } from "../stores/auth";
import { Card } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { MetadataProviderCard } from "../components/MetadataProviderCard";
import { Shield, Users, Trash2, Eye, EyeOff } from "lucide-react";

export default function AdminPage() {
  const { t } = useTranslation();
  const roleLabels: Record<string, string> = {
    super_admin: t("admin.roleSuperAdmin"),
    admin: t("admin.roleAdmin"),
    user: t("admin.roleUser"),
  };
  const navigate = useNavigate();
  const [users, setUsers] = useState<any[]>([]);
  const [allowRegistration, setAllowRegistration] = useState(true);
  const [mbEnabled, setMbEnabled] = useState(false);
  const [mbApiUrl, setMbApiUrl] = useState("");
  const [mbRateLimit, setMbRateLimit] = useState("1");
  const [mbSaving, setMbSaving] = useState(false);
  const [mbModified, setMbModified] = useState(false);
  const [mbInit, setMbInit] = useState({ enabled: false, apiUrl: "", rateLimit: "1" });
  const [mbError, setMbError] = useState("");
  const [neEnabled, setNeEnabled] = useState(false);
  const [neCookie, setNeCookie] = useState("");
  const [neRateLimit, setNeRateLimit] = useState("1");
  const [neSaving, setNeSaving] = useState(false);
  const [neModified, setNeModified] = useState(false);
  const [neInit, setNeInit] = useState({ enabled: false, cookieSet: false, rateLimit: "1" });
  const [neError, setNeError] = useState("");
  const [neShowCookie, setNeShowCookie] = useState(false);
  const [logLevel, setLogLevel] = useState("info");
  const [logLevelInit, setLogLevelInit] = useState("info");
  const [logLevelSaving, setLogLevelSaving] = useState(false);
  const [logLevelError, setLogLevelError] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (getRole() !== "super_admin" && getRole() !== "admin") {
      navigate("/settings");
      return;
    }
    loadData();
  }, []);

  async function loadData() {
    try {
      const [u, s] = await Promise.all([api.admin.users(), api.admin.getSettings()]);
      setUsers(u.users);
      setAllowRegistration(s.allow_registration);
      setMbEnabled(s.metadata_musicbrainz_enabled);
      setMbApiUrl(s.metadata_musicbrainz_api_url || "");
      setMbRateLimit(s.metadata_musicbrainz_rate_limit || "1");
      setMbInit({
        enabled: s.metadata_musicbrainz_enabled,
        apiUrl: s.metadata_musicbrainz_api_url || "",
        rateLimit: s.metadata_musicbrainz_rate_limit || "1",
      });
      setNeEnabled(s.metadata_netease_enabled);
      setNeCookie("");
      setNeRateLimit(s.platforms_netease_rate_limit || "1");
      setNeInit({
        enabled: s.metadata_netease_enabled,
        cookieSet: !!s.platforms_netease_cookie_set,
        rateLimit: s.platforms_netease_rate_limit || "1",
      });
      setLogLevel(s.log_level || "info");
      setLogLevelInit(s.log_level || "info");
    } catch (err: any) {
      setError(translateApiError(t, err));
    }
  }

  async function updateRole(id: string, role: string) {
    try {
      await api.admin.updateRole(id, role);
      loadData();
    } catch (err: any) {
      setError(translateApiError(t, err));
    }
  }

  async function toggleRegistration() {
    try {
      await api.admin.updateSettings({ allow_registration: !allowRegistration });
      setAllowRegistration(!allowRegistration);
    } catch (err: any) {
      setError(translateApiError(t, err));
    }
  }

  // NetEase card state is dirty when the toggle, a pending cookie, or the rate
  // limit differs from the loaded initial values. The pending values are
  // passed in because React state is async: checking the previous render's
  // state right after an onChange would miss the just-typed change (e.g.
  // clearing the rate limit would never show the save button).
  const neDirty = (enabled: boolean, cookie: string, rateLimit: string) =>
    enabled !== neInit.enabled || cookie !== "" || rateLimit !== neInit.rateLimit;

  // Discards the stored NetEase cookie immediately (not part of the save
  // flow): confirmation first, then a direct clear request to the backend.
  async function discardNeteaseCookie() {
    if (!confirm(t("admin.neteaseCookieDiscardConfirm"))) return;
    setNeSaving(true);
    setNeError("");
    try {
      await api.admin.updateSettings({ platforms_netease_cookie_clear: true });
      setNeInit((prev) => ({ ...prev, cookieSet: false }));
      setNeCookie("");
      setNeShowCookie(false);
      setNeModified(neDirty(neEnabled, "", neRateLimit));
    } catch (err) {
      setNeError(translateApiError(t, err));
    }
    setNeSaving(false);
  }

  const currentRole = getRole();

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">{t("admin.administration")}</h1>

      {error && <p className="text-sm text-red-400">{error}</p>}

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <Shield className="w-4 h-4" /> {t("admin.serverSettings")}
        </h3>
        <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
          <div>
            <p className="text-sm font-medium">{t("admin.allowRegistration")}</p>
            <p className="text-xs text-zinc-400">{t("admin.allowRegistrationDesc")}</p>
          </div>
          <button
            onClick={toggleRegistration}
            className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer ${allowRegistration ? "bg-green-600" : "bg-zinc-700"}`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${allowRegistration ? "translate-x-6" : ""}`}
            />
          </button>
        </div>
        <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
          <div>
            <p className="text-sm font-medium">{t("admin.logLevel")}</p>
            <p className="text-xs text-zinc-400">{t("admin.logLevelDesc")}</p>
          </div>
          <select
            value={logLevel}
            disabled={logLevelSaving}
            onChange={async (e) => {
              const next = e.target.value;
              setLogLevel(next);
              setLogLevelSaving(true);
              setLogLevelError("");
              try {
                await api.admin.updateSettings({ log_level: next });
                setLogLevelInit(next);
              } catch (err) {
                setLogLevel(logLevelInit);
                setLogLevelError(translateApiError(t, err));
              }
              setLogLevelSaving(false);
            }}
            className="rounded-lg bg-zinc-900 border border-zinc-700 px-3 py-2 text-sm focus:outline-none focus:border-green-500 disabled:opacity-50"
          >
            <option value="debug">DEBUG</option>
            <option value="info">INFO</option>
            <option value="warn">WARN</option>
            <option value="error">ERROR</option>
          </select>
        </div>
        {logLevelError && <p className="text-sm text-red-400">{logLevelError}</p>}
      </Card>

      <MetadataProviderCard
        icon={<img src="/musicbrainz.svg" className="w-4 h-4" />}
        title={t("admin.musicbrainz")}
        enableLabel={t("admin.enableMusicBrainz")}
        enableDesc={t("admin.enableMusicBrainzDesc")}
        enabled={mbEnabled}
        onEnabledChange={(next) => {
          setMbEnabled(next);
          setMbModified(true);
        }}
        rateLimit={mbRateLimit}
        onRateLimitChange={(next) => {
          setMbRateLimit(next);
          setMbModified(true);
        }}
        saving={mbSaving}
        modified={mbModified}
        onSave={async () => {
          setMbSaving(true);
          setMbError("");
          try {
            // An empty API URL or rate limit is sent verbatim so clearing a
            // field resets it to the server's config default (the backend
            // treats an empty stored value as "no override").
            await api.admin.updateSettings({
              metadata_musicbrainz_enabled: mbEnabled,
              metadata_musicbrainz_api_url: mbApiUrl,
              metadata_musicbrainz_rate_limit: mbRateLimit,
            });
            setMbModified(false);
            setMbInit({ enabled: mbEnabled, apiUrl: mbApiUrl, rateLimit: mbRateLimit });
          } catch (err) {
            setMbError(translateApiError(t, err));
          }
          setMbSaving(false);
        }}
        onRevert={() => {
          setMbEnabled(mbInit.enabled);
          setMbApiUrl(mbInit.apiUrl);
          setMbRateLimit(mbInit.rateLimit);
          setMbModified(false);
          setMbError("");
        }}
        error={mbError}
      >
        <div>
          <p className="text-xs text-zinc-400 mb-1">{t("admin.apiUrl")}</p>
          <Input
            value={mbApiUrl}
            onChange={(e) => {
              setMbApiUrl(e.target.value);
              setMbModified(true);
            }}
            placeholder="https://musicbrainz.org/ws/2"
          />
        </div>
      </MetadataProviderCard>

      <MetadataProviderCard
        icon={<img src="/netease-cloud-music.svg" alt="" className="w-4 h-4" />}
        title={t("admin.netease")}
        enableLabel={t("admin.enableNetease")}
        enableDesc={t("admin.enableNeteaseDesc")}
        enabled={neEnabled}
        onEnabledChange={(next) => {
          setNeEnabled(next);
          setNeModified(neDirty(next, neCookie, neRateLimit));
          setNeError("");
        }}
        rateLimit={neRateLimit}
        onRateLimitChange={(next) => {
          setNeRateLimit(next);
          setNeModified(neDirty(neEnabled, neCookie, next));
          setNeError("");
        }}
        saving={neSaving}
        modified={neModified}
        onSave={async () => {
          setNeSaving(true);
          setNeError("");
          try {
            const payload: Record<string, unknown> = { metadata_netease_enabled: neEnabled };
            if (neCookie !== "") payload.platforms_netease_cookie = neCookie;
            // An empty rate limit is sent verbatim so clearing the field
            // resets the provider to the config default.
            payload.platforms_netease_rate_limit = neRateLimit;
            await api.admin.updateSettings(payload);
            setNeModified(false);
            setNeInit({
              enabled: neEnabled,
              cookieSet: neCookie !== "" || neInit.cookieSet,
              rateLimit: neRateLimit,
            });
            setNeCookie("");
            setNeShowCookie(false);
          } catch (err) {
            setNeError(translateApiError(t, err));
          }
          setNeSaving(false);
        }}
        onRevert={() => {
          setNeEnabled(neInit.enabled);
          setNeCookie("");
          setNeRateLimit(neInit.rateLimit);
          setNeShowCookie(false);
          setNeModified(false);
          setNeError("");
        }}
        error={neError}
      >
        <div>
          <p className="text-xs text-zinc-400 mb-1">{t("admin.neteaseCookie")}</p>
          <div className="relative">
            <input
              type={neShowCookie ? "text" : "password"}
              value={neCookie}
              autoComplete="off"
              onChange={(e) => {
                const next = e.target.value;
                setNeCookie(next);
                if (next === "") setNeShowCookie(false);
                setNeModified(neDirty(neEnabled, next, neRateLimit));
                setNeError("");
              }}
              placeholder={neInit.cookieSet ? t("admin.neteaseCookieConfigured") : "MUSIC_U=..."}
              disabled={neSaving}
              className="w-full rounded-lg bg-zinc-900 border border-zinc-700 px-3 py-2 pr-40 text-sm focus:outline-none focus:border-green-500 disabled:opacity-50"
            />
            {neCookie !== "" && (
              <button
                onClick={() => setNeShowCookie(!neShowCookie)}
                aria-label={t("admin.neteaseCookieShow")}
                disabled={neSaving}
                className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1.5 rounded text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
              >
                {neShowCookie ? (
                  <EyeOff className="w-3.5 h-3.5" />
                ) : (
                  <Eye className="w-3.5 h-3.5" />
                )}
              </button>
            )}
            {neInit.cookieSet && neCookie === "" && (
              <button
                onClick={discardNeteaseCookie}
                aria-label={t("admin.neteaseCookieDiscard")}
                disabled={neSaving}
                className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center gap-1 px-1.5 py-1 rounded text-xs text-red-400 hover:text-red-300 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
              >
                <span className="whitespace-nowrap">{t("admin.neteaseCookieDiscard")}</span>
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        </div>
      </MetadataProviderCard>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2">
          <Users className="w-4 h-4" /> {t("admin.users")}
        </h3>
        <div className="space-y-1">
          {users.map((u) => (
            <div
              key={u.id}
              className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50 text-sm"
            >
              <div className="flex-1 min-w-0">
                <span className="truncate block">{u.username}</span>
                <span className="text-zinc-500 text-xs">{u.email}</span>
              </div>
              <span className="text-zinc-400 w-20 text-center">
                {roleLabels[u.role] ?? t("admin.roleUser")}
              </span>
              <div className="flex gap-2 w-36 justify-end">
                {currentRole === "super_admin" && u.role !== "super_admin" && (
                  <>
                    {u.role !== "admin" && (
                      <button
                        onClick={() => updateRole(u.id, "admin")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer"
                      >
                        {t("admin.promote")}
                      </button>
                    )}
                    {u.role !== "user" && (
                      <button
                        onClick={() => updateRole(u.id, "user")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer"
                      >
                        {t("admin.demote")}
                      </button>
                    )}
                  </>
                )}
                {currentRole === "admin" && u.role === "user" && (
                  <button
                    onClick={() => updateRole(u.id, "admin")}
                    className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer"
                  >
                    {t("admin.promote")}
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      </Card>
    </div>
  );
}
