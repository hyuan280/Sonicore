import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Input } from "../../components/ui/input";
import { api } from "../../api/client";
import { MetadataProviderCard } from "../../components/MetadataProviderCard";
import { Loader2, Trash2, Eye, EyeOff } from "lucide-react";

export default function SourcesTab() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [mbEnabled, setMbEnabled] = useState(false);
  const [mbApiUrl, setMbApiUrl] = useState("");
  const [mbRateLimit, setMbRateLimit] = useState("1");
  const [mbSaving, setMbSaving] = useState(false);
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

  // Load once on mount. Deliberately not keyed on `t` so a language switch
  // never re-fetches and overwrites in-progress edits.
  useEffect(() => {
    api.admin
      .getSettings()
      .then((s) => {
        setMbEnabled(s.metadata_musicbrainz_enabled ?? false);
        setMbApiUrl(s.metadata_musicbrainz_api_url || "");
        setMbRateLimit(s.metadata_musicbrainz_rate_limit || "1");
        setMbInit({
          enabled: s.metadata_musicbrainz_enabled ?? false,
          apiUrl: s.metadata_musicbrainz_api_url || "",
          rateLimit: s.metadata_musicbrainz_rate_limit || "1",
        });
        setNeEnabled(s.metadata_netease_enabled ?? false);
        setNeCookie("");
        setNeRateLimit(s.platforms_netease_rate_limit || "1");
        setNeInit({
          enabled: s.metadata_netease_enabled ?? false,
          cookieSet: !!s.platforms_netease_cookie_set,
          rateLimit: s.platforms_netease_rate_limit || "1",
        });
      })
      .catch((err: unknown) => {
        const message = translateApiError(t, err);
        setMbError(message);
        setNeError(message);
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // NetEase card state is dirty when the toggle, a pending cookie, or the rate
  // limit differs from the loaded initial values. The pending values are
  // passed in because React state is async: checking the previous render's
  // state right after an onChange would miss the just-typed change (e.g.
  // clearing the rate limit would never show the save button).
  const neDirty = (enabled: boolean, cookie: string, rateLimit: string) =>
    enabled !== neInit.enabled || cookie !== "" || rateLimit !== neInit.rateLimit;

  // MusicBrainz card dirty state, derived from the current values vs the
  // loaded initial values (mirrors neDirty without the cookie dimension).
  const mbDirty =
    mbEnabled !== mbInit.enabled || mbApiUrl !== mbInit.apiUrl || mbRateLimit !== mbInit.rateLimit;

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

  return (
    <div className="space-y-4">
      {loading && (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      )}
      {!loading && (
        <MetadataProviderCard
          icon={<img src="/musicbrainz.svg" alt="" className="w-4 h-4" />}
          title={t("admin.musicbrainz")}
          enableLabel={t("admin.enableMusicBrainz")}
          enableDesc={t("admin.enableMusicBrainzDesc")}
          enabled={mbEnabled}
          onEnabledChange={(next) => {
            setMbEnabled(next);
            setMbError("");
          }}
          rateLimit={mbRateLimit}
          onRateLimitChange={(next) => {
            setMbRateLimit(next);
            setMbError(next !== "" && !/^[1-9]\d*$/.test(next) ? t("admin.invalidRateLimit") : "");
          }}
          saving={mbSaving}
          modified={mbDirty}
          onSave={async () => {
            // MusicBrainz needs a rate limit >= 1; an empty value means
            // "use the server default".
            if (mbRateLimit !== "" && !/^[1-9]\d*$/.test(mbRateLimit)) {
              setMbError(t("admin.invalidRateLimit"));
              return;
            }
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
              setMbInit({ enabled: mbEnabled, apiUrl: mbApiUrl, rateLimit: mbRateLimit });
            } catch (err) {
              setMbError(translateApiError(t, err));
            } finally {
              setMbSaving(false);
            }
          }}
          onRevert={() => {
            setMbEnabled(mbInit.enabled);
            setMbApiUrl(mbInit.apiUrl);
            setMbRateLimit(mbInit.rateLimit);
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
                setMbError("");
              }}
              placeholder="https://musicbrainz.org/ws/2"
            />
          </div>
        </MetadataProviderCard>
      )}

      {!loading && (
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
            setNeError(next !== "" && !/^\d+$/.test(next) ? t("admin.invalidRateLimit") : "");
          }}
          saving={neSaving}
          modified={neModified}
          onSave={async () => {
            // NetEase accepts 0 (disables the limit) and any non-negative
            // integer; an empty value means "use the server default".
            if (neRateLimit !== "" && !/^\d+$/.test(neRateLimit)) {
              setNeError(t("admin.invalidRateLimit"));
              return;
            }
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
            } finally {
              setNeSaving(false);
            }
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
      )}
    </div>
  );
}
