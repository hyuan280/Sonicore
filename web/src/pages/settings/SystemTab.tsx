import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Card } from "../../components/ui/card";
import { api } from "../../api/client";
import {
  Shield,
  Turntable,
  ChevronDown,
  Loader2,
  KeyRound,
  Eye,
  EyeOff,
  Trash2,
} from "lucide-react";
import type { JukeboxInfo } from "../../stores/jukebox";

export default function SystemTab() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [allowRegistration, setAllowRegistration] = useState(true);
  const [registrationSaving, setRegistrationSaving] = useState(false);
  const [subsonicJukeboxId, setSubsonicJukeboxId] = useState("");
  const [logLevel, setLogLevel] = useState("info");
  const [logLevelInit, setLogLevelInit] = useState("info");
  const [logLevelSaving, setLogLevelSaving] = useState(false);
  const [logLevelError, setLogLevelError] = useState("");
  const [githubTokenSet, setGithubTokenSet] = useState(false);
  const [githubTokenError, setGithubTokenError] = useState(false);
  const [error, setError] = useState("");

  // Load once on mount. Deliberately not keyed on `t` so a language switch
  // never re-fetches the settings. Controls stay disabled until the values
  // arrive so a click can never save defaults over the real configuration.
  useEffect(() => {
    api.admin
      .getSettings()
      .then((s) => {
        setAllowRegistration(s.allow_registration ?? false);
        setSubsonicJukeboxId(s.subsonic_jukebox_id || "");
        setLogLevel(s.log_level || "info");
        setLogLevelInit(s.log_level || "info");
        setGithubTokenSet(!!s.plugins_github_token_set);
        setGithubTokenError(!!s.plugins_github_token_error);
      })
      .catch((err: unknown) => setError(translateApiError(t, err)))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function toggleRegistration() {
    setRegistrationSaving(true);
    try {
      const next = !allowRegistration;
      await api.admin.updateSettings({ allow_registration: next });
      setAllowRegistration(next);
      setError("");
    } catch (err) {
      setError(translateApiError(t, err));
    } finally {
      setRegistrationSaving(false);
    }
  }

  return (
    <div className="space-y-4">
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
            type="button"
            role="switch"
            aria-label={t("admin.allowRegistration")}
            aria-checked={allowRegistration}
            onClick={toggleRegistration}
            disabled={registrationSaving || loading}
            className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer disabled:opacity-50 ${allowRegistration ? "bg-green-600" : "bg-zinc-700"}`}
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
            disabled={logLevelSaving || loading}
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

      <SubsonicJukeboxSetting
        subsonicJukeboxId={subsonicJukeboxId}
        onSaved={setSubsonicJukeboxId}
      />

      <GitHubTokenSetting
        tokenSet={githubTokenSet}
        tokenError={githubTokenError}
        onSaved={setGithubTokenSet}
      />
    </div>
  );
}

function GitHubTokenSetting({
  tokenSet,
  tokenError,
  onSaved,
}: {
  tokenSet: boolean;
  tokenError: boolean;
  onSaved: (set: boolean) => void;
}) {
  const { t } = useTranslation();
  const [token, setToken] = useState("");
  const [show, setShow] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    const trimmed = token.trim();
    if (trimmed === "") {
      setError(t("admin.githubTokenRequired"));
      return;
    }
    setSaving(true);
    setError("");
    try {
      await api.admin.updateSettings({ plugins_github_token: trimmed });
      setToken("");
      setShow(false);
      onSaved(true);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setSaving(false);
    }
  };

  const discard = async () => {
    if (!confirm(t("admin.githubTokenDiscardConfirm"))) return;
    setSaving(true);
    setError("");
    try {
      await api.admin.updateSettings({ plugins_github_token_clear: true });
      setToken("");
      setShow(false);
      onSaved(false);
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="p-4 space-y-3">
      <h2 className="font-medium flex items-center gap-2">
        <KeyRound className="w-4 h-4" /> {t("admin.githubToken")}
        {tokenSet && (
          <span className="text-xs text-green-400 ml-auto">{t("admin.githubTokenSet")}</span>
        )}
      </h2>
      <p className="text-xs text-zinc-500">{t("admin.githubTokenDesc")}</p>
      {tokenError && <p className="text-xs text-amber-400">{t("admin.githubTokenBroken")}</p>}
      {error && <p className="text-xs text-red-400">{error}</p>}
      <div className="relative">
        <input
          type={show ? "text" : "password"}
          value={token}
          autoComplete="off"
          onChange={(e) => {
            const next = e.target.value;
            setToken(next);
            if (next === "") setShow(false);
            setError("");
          }}
          placeholder={tokenSet ? t("admin.githubTokenConfigured") : "ghp_..."}
          disabled={saving}
          className="w-full rounded-lg bg-zinc-900 border border-zinc-700 px-3 py-2 pr-24 text-sm focus:outline-none focus:border-green-500 disabled:opacity-50"
        />
        {token !== "" && (
          <button
            type="button"
            onClick={() => setShow(!show)}
            aria-label={t("admin.githubTokenShow")}
            disabled={saving}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1.5 rounded text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
          >
            {show ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
          </button>
        )}
        {tokenSet && token === "" && (
          <button
            type="button"
            onClick={discard}
            aria-label={t("admin.githubTokenDiscard")}
            disabled={saving}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center gap-1 px-1.5 py-1 rounded text-xs text-red-400 hover:text-red-300 hover:bg-zinc-800 cursor-pointer disabled:opacity-50"
          >
            <span className="whitespace-nowrap">{t("admin.githubTokenDiscard")}</span>
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
      {token !== "" && (
        <div className="flex justify-end">
          <button
            type="button"
            onClick={save}
            disabled={saving}
            className="inline-flex items-center gap-2 rounded-lg bg-green-600 hover:bg-green-500 px-3 py-2 text-sm font-medium text-white cursor-pointer disabled:opacity-50"
          >
            {saving && <Loader2 className="w-4 h-4 animate-spin" />}
            {t("settings.update")}
          </button>
        </div>
      )}
    </Card>
  );
}

function SubsonicJukeboxSetting({
  subsonicJukeboxId,
  onSaved,
}: {
  subsonicJukeboxId: string;
  onSaved: (id: string) => void;
}) {
  const { t } = useTranslation();
  const [jukeboxes, setJukeboxes] = useState<JukeboxInfo[]>([]);
  const [selected, setSelected] = useState(subsonicJukeboxId);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);

  // The parent loads the settings asynchronously; sync the selection once
  // the id arrives.
  useEffect(() => {
    setSelected(subsonicJukeboxId);
  }, [subsonicJukeboxId]);

  // Load only the jukebox list here — settings come from the parent. Not
  // keyed on `t` so a language switch never re-fetches.
  useEffect(() => {
    api.jukebox
      .list()
      .then((jbList) => {
        setJukeboxes(jbList.jukeboxes || []);
      })
      .catch(() => setError(t("settings.failedToLoadJukebox")))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const save = async (id: string) => {
    const prev = selected;
    setSaving(true);
    setSelected(id);
    setOpen(false);
    try {
      await api.admin.updateSettings({ subsonic_jukebox_id: id || "" });
      // Keep the parent's copy in sync so a future re-render can never
      // roll the UI back to the old value.
      onSaved(id || "");
      setError("");
    } catch {
      setSelected(prev);
      setError(t("settings.failedToSaveJukebox"));
    } finally {
      setSaving(false);
    }
  };

  const selectedJb = selected ? jukeboxes.find((j) => j.id === selected) : null;

  // Close the dropdown on outside click or Escape.
  const dropdownRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  return (
    <Card className="p-4 space-y-3">
      <h2 className="font-medium flex items-center gap-2">
        <img src="/subsonic.png" className="w-4 h-4" /> {t("settings.subsonicJukebox")}
        {selected && (
          <span className="text-xs text-green-400 ml-auto">{t("settings.subsonicHint")}</span>
        )}
      </h2>
      {error && <p className="text-xs text-red-400">{error}</p>}
      {loading ? (
        <Loader2 className="w-4 h-4 animate-spin text-zinc-500" />
      ) : jukeboxes.length === 0 ? (
        <p className="text-xs text-zinc-500">{t("settings.noJukeboxesHint")}</p>
      ) : (
        <div className="relative" ref={dropdownRef}>
          <button
            type="button"
            aria-haspopup="listbox"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            disabled={saving}
            className="w-full flex items-center text-sm cursor-pointer rounded-lg px-3 py-2.5 bg-zinc-800"
          >
            <Turntable
              className={`w-4 h-4 shrink-0 mr-2 ${selected ? "text-green-400" : "text-zinc-500"}`}
            />
            <span className="flex-1 text-left min-w-0">
              {selectedJb ? (
                <>
                  <div className="text-white text-sm">{selectedJb.name}</div>
                  <div className="text-xs text-zinc-500">
                    {selectedJb.device_name || t("jukebox.noDevice")}
                  </div>
                </>
              ) : (
                <>
                  <div className="text-white text-sm">{t("settings.disabled")}</div>
                  <div className="text-xs text-zinc-500">{t("jukebox.noDevice")}</div>
                </>
              )}
            </span>
            <ChevronDown
              className={`w-4 h-4 ml-2 text-zinc-500 shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
            />
          </button>
          {open && (
            <div
              role="listbox"
              aria-label={t("settings.subsonicJukebox")}
              className="absolute top-full left-0 right-0 mt-1 bg-zinc-800 rounded-lg shadow-xl z-50 py-1 border border-zinc-700/50"
            >
              <button
                type="button"
                role="option"
                aria-selected={!selected}
                onClick={() => save("")}
                className={`w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 ${!selected ? "text-white" : "text-zinc-400 hover:text-white"}`}
              >
                <Turntable className="w-4 h-4 shrink-0 text-zinc-500" />
                <div className="min-w-0">
                  <div className="text-sm">{t("settings.disabled")}</div>
                  <div className="text-xs text-zinc-500">{t("jukebox.noDevice")}</div>
                </div>
              </button>
              {jukeboxes.map((j) => (
                <button
                  key={j.id}
                  type="button"
                  role="option"
                  aria-selected={j.id === selected}
                  onClick={() => save(j.id)}
                  className={`w-full text-left px-3 py-2 text-sm cursor-pointer flex items-center gap-2 ${j.id === selected ? "text-white" : "text-zinc-400 hover:text-white"}`}
                >
                  <Turntable
                    className={`w-4 h-4 shrink-0 ${j.id === selected ? "text-green-400" : "text-zinc-500"}`}
                  />
                  <div className="min-w-0">
                    <div className="font-medium">{j.name}</div>
                    <div className="text-xs text-zinc-500">{j.device_name || j.device_id}</div>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
