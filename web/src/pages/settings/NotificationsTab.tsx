import { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { useAuth } from "../../stores/auth";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Card } from "../../components/ui/card";
import { api } from "../../api/client";
import { Bell, Plus, Pen, Trash2, Settings, Loader2 } from "lucide-react";
import type { NotifTestOptions } from "../../types";

export default function NotificationsTab() {
  return (
    <div className="space-y-4">
      <NotificationChannelPrefs />
      <EmailNotificationSettings />
    </div>
  );
}

// Recipient roles shown in the notification scope editor.
// Keep in sync with domain.RecipientRole in internal/core/domain/notification.go
// ("operator", "admin", "all").
const NOTIFICATION_RECIPIENT_ROLES = ["operator", "admin", "all"];

function NotificationChannelPrefs() {
  const { t } = useTranslation();
  const [prefs, setPrefs] = useState<Record<string, { roles: string[]; channels: string[] }>>({});
  const [channels, setChannels] = useState<{ type: string; name: string; enabled: boolean }[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [formChannel, setFormChannel] = useState("");
  const [formCats, setFormCats] = useState<string[]>([]);
  const [scopeOpen, setScopeOpen] = useState(false);
  const [scopePrefs, setScopePrefs] = useState<Record<string, { roles: string[] }>>({});
  const [editChannel, setEditChannel] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  // Load once on mount. Deliberately not keyed on `t` so a language switch
  // never re-fetches and overwrites in-progress edits.
  useEffect(() => {
    Promise.all([api.notifications.getPreferences(), api.notifications.channels()])
      .then(([p, c]) => {
        setPrefs(p);
        setScopePrefs(p);
        setChannels(c.channels || []);
      })
      .catch((err: unknown) => setError(translateApiError(t, err)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const categories = Object.keys(prefs);

  function saveLabel() {
    if (saving) return t("common.saving");
    if (editChannel) return t("common.save");
    return t("settings.create");
  }

  function toggleFormCat(cat: string) {
    setFormCats((prev) => (prev.includes(cat) ? prev.filter((c) => c !== cat) : [...prev, cat]));
  }

  // Only show channels that are enabled and configured with at least one category
  function channelEntries(): { type: string; name: string; cats: string[] }[] {
    return channels
      .filter((ch) => ch.enabled)
      .map((ch) => {
        const cats = categories.filter((cat) => (prefs[cat]?.channels || []).includes(ch.type));
        return { ...ch, cats };
      })
      .filter((e) => e.cats.length > 0);
  }

  async function create() {
    if (!formChannel || formCats.length === 0) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const update: Record<string, { channels: string[] }> = {};
      for (const cat of categories) {
        const cur = [...(prefs[cat]?.channels || [])];
        if (formCats.includes(cat)) {
          if (!cur.includes(formChannel)) cur.push(formChannel);
        } else {
          const idx = cur.indexOf(formChannel);
          if (idx >= 0) cur.splice(idx, 1);
        }
        update[cat] = { channels: cur };
      }
      await api.notifications.updatePreferences(update);
      // Reload to get fresh state
      const fresh = await api.notifications.getPreferences();
      setPrefs(fresh);
      setShowForm(false);
      setFormChannel("");
      setFormCats([]);
      setEditChannel(null);
      setSuccess(t("settings.saved"));
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    }
    setSaving(false);
  }

  function toggleRole(cat: string, role: string) {
    setScopePrefs((prev) => {
      const p = { ...prev };
      const cur = { ...(p[cat] || { roles: [] }) };
      // The backend serializes an empty slice as `roles: null` — normalize
      // before calling includes to avoid a TypeError.
      cur.roles = cur.roles || [];
      if (cur.roles.includes(role)) {
        cur.roles = cur.roles.filter((r: string) => r !== role);
      } else {
        cur.roles = [...cur.roles, role];
      }
      p[cat] = cur;
      return p;
    });
  }

  async function saveScope() {
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const update: Record<string, { roles: string[] }> = {};
      for (const [cat, p] of Object.entries(scopePrefs)) {
        update[cat] = { roles: p.roles };
      }
      await api.notifications.updatePreferences(update);
      const fresh = await api.notifications.getPreferences();
      setPrefs(fresh);
      setScopePrefs(fresh);
      setScopeOpen(false);
      setSuccess(t("settings.saved"));
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    }
    setSaving(false);
  }

  async function remove(chType: string) {
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const update: Record<string, { channels: string[] }> = {};
      for (const cat of categories) {
        update[cat] = {
          channels: (prefs[cat]?.channels || []).filter((c: string) => c !== chType),
        };
      }
      await api.notifications.updatePreferences(update);
      const fresh = await api.notifications.getPreferences();
      setPrefs(fresh);
      setSuccess(t("settings.saved"));
      if (editChannel === chType || formChannel === chType) {
        setShowForm(false);
        setFormChannel("");
        setFormCats([]);
        setEditChannel(null);
      }
    } catch (err: unknown) {
      setError(translateApiError(t, err));
    }
    setSaving(false);
  }

  return (
    <Card className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="font-medium flex items-center gap-2">
          <Bell className="w-4 h-4" /> {t("settings.notificationPrefs")}
        </h3>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            disabled={saving}
            onClick={() => {
              setScopeOpen(true);
              setScopePrefs(prefs);
            }}
          >
            <Settings className="w-4 h-4" />
          </Button>
          <Button
            size="sm"
            disabled={saving}
            onClick={() => {
              setShowForm(!showForm);
              setFormChannel("");
              setFormCats([]);
              setEditChannel(null);
            }}
            className="px-2 py-1 text-xs"
          >
            <Plus className="w-3.5 h-3.5 mr-0.5" />
            {t("settings.add")}
          </Button>
        </div>
      </div>

      {scopeOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
          onClick={() => setScopeOpen(false)}
        >
          <div
            className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-lg font-bold">{t("settings.notifScopeSettings")}</h2>
            <div className="rounded-lg bg-zinc-800/50 overflow-hidden">
              <div className="grid grid-cols-[1fr_2fr] gap-0 text-xs text-zinc-500 font-medium border-b border-zinc-700">
                <div className="px-3 py-2">{t("settings.notifType")}</div>
                <div className="px-3 py-2">{t("settings.notifRecipients")}</div>
              </div>
              {Object.entries(scopePrefs).map(([cat, p]) => (
                <div
                  key={cat}
                  className="grid grid-cols-[1fr_2fr] gap-0 border-b border-zinc-700/50 last:border-b-0"
                >
                  <div className="px-3 py-2 flex items-center">
                    <span className="text-sm">{t("settings.notifCat_" + cat)}</span>
                  </div>
                  <div className="px-3 py-2 flex flex-wrap gap-1.5 items-center">
                    {NOTIFICATION_RECIPIENT_ROLES.map((role) => {
                      const active = (p.roles || []).includes(role);
                      return (
                        <button
                          key={role}
                          type="button"
                          onClick={() => toggleRole(cat, role)}
                          className={`px-2 py-0.5 rounded text-xs border cursor-pointer transition-colors ${
                            active
                              ? "bg-green-600/20 border-green-600 text-green-400"
                              : "bg-zinc-800 border-zinc-700 text-zinc-400 hover:border-zinc-500"
                          }`}
                        >
                          {t("settings.notifRole_" + role)}
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
            <div className="flex justify-end gap-3">
              <Button variant="ghost" size="sm" onClick={() => setScopeOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" size="sm" onClick={saveScope} disabled={saving}>
                {saving ? t("common.saving") : t("common.save")}
              </Button>
            </div>
          </div>
        </div>
      )}

      {showForm && (
        <div className="space-y-3 p-3 rounded-lg bg-zinc-800">
          {editChannel ? (
            <div className="text-sm font-medium">
              {channels.find((ch) => ch.type === editChannel)?.name || editChannel}
            </div>
          ) : (
            <select
              value={formChannel}
              onChange={(e) => {
                setFormChannel(e.target.value);
                if (e.target.value) {
                  const cats = categories.filter((cat) =>
                    (prefs[cat]?.channels || []).includes(e.target.value),
                  );
                  setFormCats(cats);
                } else {
                  setFormCats([]);
                }
              }}
              className="w-full rounded-lg bg-zinc-900 border border-zinc-700 px-3 py-2 text-sm focus:outline-none focus:border-green-500"
            >
              <option value="">{t("settings.notifSelectPlatform")}</option>
              {channels
                .filter((ch) => ch.enabled)
                .filter(
                  (ch) =>
                    editChannel === ch.type || !channelEntries().some((e) => e.type === ch.type),
                )
                .map((ch) => (
                  <option key={ch.type} value={ch.type}>
                    {ch.name}
                  </option>
                ))}
            </select>
          )}
          {formChannel && (
            <div className="flex flex-wrap gap-2">
              {categories.map((cat) => {
                const active = formCats.includes(cat);
                return (
                  <button
                    key={cat}
                    type="button"
                    onClick={() => toggleFormCat(cat)}
                    className={`px-3 py-1 rounded-lg text-xs border cursor-pointer transition-colors ${
                      active
                        ? "bg-green-600/20 border-green-600 text-green-400"
                        : "bg-zinc-800 border-zinc-700 text-zinc-400 hover:border-zinc-500"
                    }`}
                  >
                    {t("settings.notifCat_" + cat)}
                  </button>
                );
              })}
            </div>
          )}
          <Button
            size="sm"
            onClick={create}
            disabled={saving || !formChannel || formCats.length === 0}
          >
            {saveLabel()}
          </Button>
        </div>
      )}

      <div className="space-y-2">
        {channelEntries().length === 0 && (
          <p className="text-xs text-zinc-500 p-3">{t("settings.notifNoPlatforms")}</p>
        )}
        {channelEntries().map((entry) => (
          <div
            key={entry.type}
            className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50"
          >
            <div className="flex items-center gap-3 min-w-0">
              <div className="min-w-0">
                <p className="text-sm font-medium">{entry.name}</p>
                <p className="text-xs text-zinc-500">
                  {entry.cats.map((c) => t("settings.notifCat_" + c)).join(" · ")}
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <Button
                variant="ghost"
                size="sm"
                disabled={saving}
                onClick={() => {
                  setEditChannel(entry.type);
                  setFormChannel(entry.type);
                  setFormCats(entry.cats);
                  setShowForm(true);
                }}
              >
                <Pen className="w-4 h-4" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={saving}
                onClick={() => remove(entry.type)}
              >
                <Trash2 className="w-4 h-4 text-red-400" />
              </Button>
            </div>
          </div>
        ))}
      </div>

      {error && <p className="text-xs text-red-400">{error}</p>}
      {success && <p className="text-xs text-green-400">{success}</p>}
    </Card>
  );
}

function EmailNotificationSettings() {
  const { t } = useTranslation();
  const currentUser = useAuth((s) => s.user);
  const [notifEmailEnabled, setNotifEmailEnabled] = useState(false);
  const [notifSmtpHost, setNotifSmtpHost] = useState("");
  const [notifSmtpPort, setNotifSmtpPort] = useState("587");
  const [notifUsername, setNotifUsername] = useState("");
  const [notifPassword, setNotifPassword] = useState("");
  const [notifPasswordAction, setNotifPasswordAction] = useState<"keep" | "set" | "clear">("keep");
  const [notifPasswordOpen, setNotifPasswordOpen] = useState(false);
  const [notifFromAddr, setNotifFromAddr] = useState("");
  const [notifFromName, setNotifFromName] = useState("");
  const [notifTls, setNotifTls] = useState(true);
  const [notifInit, setNotifInit] = useState({
    enabled: false,
    smtpHost: "",
    smtpPort: "587",
    username: "",
    passwordSet: false,
    fromAddr: "",
    fromName: "",
    tls: true,
  });
  const [notifSaving, setNotifSaving] = useState(false);
  const [notifModified, setNotifModified] = useState(false);
  const [notifError, setNotifError] = useState("");
  const [notifTesting, setNotifTesting] = useState(false);
  const [notifSuccess, setNotifSuccess] = useState("");
  const [notifLoading, setNotifLoading] = useState(true);

  // Load once on mount. Deliberately not keyed on `t` so a language switch
  // never re-fetches and overwrites in-progress edits. Editing stays hidden
  // until the values arrive so defaults can never be saved over real config.
  useEffect(() => {
    api.admin
      .getSettings()
      .then((s) => {
        setNotifEmailEnabled(s.notification_email_enabled);
        setNotifSmtpHost(s.notification_email_smtp_host || "");
        setNotifSmtpPort(s.notification_email_smtp_port || "587");
        setNotifUsername(s.notification_email_username || "");
        setNotifPassword("");
        setNotifFromAddr(s.notification_email_from_address || "");
        setNotifFromName(s.notification_email_from_name || "");
        setNotifTls(s.notification_email_tls);
        setNotifInit({
          enabled: s.notification_email_enabled,
          smtpHost: s.notification_email_smtp_host || "",
          smtpPort: s.notification_email_smtp_port || "587",
          username: s.notification_email_username || "",
          passwordSet: s.notification_email_password_set,
          fromAddr: s.notification_email_from_address || "",
          fromName: s.notification_email_from_name || "",
          tls: s.notification_email_tls,
        });
      })
      .catch((err: unknown) => setNotifError(translateApiError(t, err)))
      .finally(() => setNotifLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const passwordRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (passwordRef.current && !passwordRef.current.contains(e.target as Node)) {
        setNotifPasswordOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  return (
    <Card className="space-y-3">
      <h3 className="font-medium flex items-center gap-2">
        <Bell className="w-4 h-4" /> {t("admin.notification")}
      </h3>

      {notifLoading ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      ) : (
        <div className="space-y-3 p-3 rounded-lg bg-zinc-800/50">
          {/* Email toggle */}
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">{t("admin.notificationEmail")}</p>
              <p className="text-xs text-zinc-400">{t("admin.notificationEmailDesc")}</p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={notifEmailEnabled}
              onClick={() => {
                setNotifEmailEnabled(!notifEmailEnabled);
                setNotifModified(true);
              }}
              disabled={notifSaving}
              className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer disabled:opacity-50 ${notifEmailEnabled ? "bg-green-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${notifEmailEnabled ? "translate-x-6" : ""}`}
              />
            </button>
          </div>

          {/* SMTP Host */}
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifSmtpHost")}</p>
            <Input
              value={notifSmtpHost}
              onChange={(e) => {
                setNotifSmtpHost(e.target.value);
                setNotifModified(true);
              }}
              placeholder="smtp.example.com"
              disabled={notifSaving}
            />
          </div>

          {/* SMTP Port */}
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifSmtpPort")}</p>
            <Input
              type="number"
              min={1}
              max={65535}
              value={notifSmtpPort}
              onChange={(e) => {
                setNotifSmtpPort(e.target.value);
                setNotifModified(true);
              }}
              placeholder="587"
              disabled={notifSaving}
            />
          </div>

          {/* Username */}
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifUsername")}</p>
            <Input
              value={notifUsername}
              onChange={(e) => {
                setNotifUsername(e.target.value);
                setNotifModified(true);
              }}
              placeholder=""
              disabled={notifSaving}
            />
          </div>

          {/* Password */}
          <div className="relative" ref={passwordRef}>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifPassword")}</p>
            <Input
              type="password"
              autoComplete="new-password"
              value={notifPasswordAction === "clear" ? "" : notifPassword}
              placeholder={(() => {
                if (notifPasswordAction === "clear") return t("admin.notifPasswordClear");
                return notifInit.passwordSet ? "********" : "";
              })()}
              onFocus={() => {
                if (notifPasswordAction !== "clear") setNotifPasswordAction("set");
                setNotifPasswordOpen(true);
              }}
              onChange={(e) => {
                setNotifPassword(e.target.value);
                setNotifPasswordAction("set");
                setNotifModified(true);
              }}
              disabled={notifSaving}
            />
            {notifPasswordOpen && (
              <div className="absolute z-10 mt-1 w-full rounded-lg bg-zinc-800 border border-zinc-700 shadow-lg overflow-hidden">
                <button
                  type="button"
                  className={`w-full text-left px-3 py-2 text-sm hover:bg-zinc-700 cursor-pointer ${notifPasswordAction === "keep" ? "text-green-400" : "text-zinc-300"}`}
                  onClick={() => {
                    setNotifPasswordAction("keep");
                    setNotifPasswordOpen(false);
                    setNotifPassword("");
                  }}
                >
                  {t("admin.notifPasswordKeep")}
                </button>
                <button
                  type="button"
                  className={`w-full text-left px-3 py-2 text-sm hover:bg-zinc-700 cursor-pointer ${notifPasswordAction === "clear" ? "text-red-400" : "text-zinc-300"}`}
                  onClick={() => {
                    setNotifPasswordAction("clear");
                    setNotifPasswordOpen(false);
                    setNotifPassword("");
                    setNotifModified(true);
                  }}
                >
                  {t("admin.notifPasswordClear")}
                </button>
              </div>
            )}
          </div>

          {/* From Address */}
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifFromAddress")}</p>
            <Input
              value={notifFromAddr}
              onChange={(e) => {
                setNotifFromAddr(e.target.value);
                setNotifModified(true);
              }}
              placeholder="sonicore@example.com"
              disabled={notifSaving}
            />
          </div>

          {/* From Name */}
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.notifFromName")}</p>
            <Input
              value={notifFromName}
              onChange={(e) => {
                setNotifFromName(e.target.value);
                setNotifModified(true);
              }}
              placeholder="Sonicore"
              disabled={notifSaving}
            />
          </div>

          {/* TLS toggle */}
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">{t("admin.notifTls")}</p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={notifTls}
              onClick={() => {
                setNotifTls(!notifTls);
                setNotifModified(true);
              }}
              disabled={notifSaving}
              className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer disabled:opacity-50 ${notifTls ? "bg-green-600" : "bg-zinc-700"}`}
            >
              <span
                className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${notifTls ? "translate-x-6" : ""}`}
              />
            </button>
          </div>

          {notifSuccess && <span className="text-xs text-green-400">{notifSuccess}</span>}
          {notifError && <span className="text-xs text-red-400">{notifError}</span>}

          {notifModified && (
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={async () => {
                  setNotifSaving(true);
                  setNotifError("");
                  setNotifSuccess("");
                  try {
                    const payload: Record<string, unknown> = {
                      notification_email_enabled: notifEmailEnabled,
                      notification_email_smtp_host: notifSmtpHost,
                      notification_email_smtp_port: notifSmtpPort,
                      notification_email_username: notifUsername,
                      notification_email_from_address: notifFromAddr,
                      notification_email_from_name: notifFromName,
                      notification_email_tls: notifTls,
                    };
                    if (notifPasswordAction === "set" && notifPassword !== "") {
                      payload.notification_email_password = notifPassword;
                    } else if (notifPasswordAction === "clear") {
                      payload.notification_email_password = "";
                    }
                    await api.admin.updateSettings(payload);
                    // buttons disappearing is the success feedback
                    setNotifModified(false);
                    const savedPasswordSet = (() => {
                      if (notifPasswordAction === "clear") return false;
                      if (notifPasswordAction === "set" && notifPassword !== "") return true;
                      return notifInit.passwordSet;
                    })();
                    setNotifInit({
                      enabled: notifEmailEnabled,
                      smtpHost: notifSmtpHost,
                      smtpPort: notifSmtpPort,
                      username: notifUsername,
                      passwordSet: savedPasswordSet,
                      fromAddr: notifFromAddr,
                      fromName: notifFromName,
                      tls: notifTls,
                    });
                    setNotifPassword("");
                    setNotifPasswordAction("keep");
                  } catch (err: unknown) {
                    setNotifError(translateApiError(t, err));
                  } finally {
                    setNotifSaving(false);
                  }
                }}
                disabled={notifSaving}
                className="px-3 py-1.5 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer"
              >
                {notifSaving ? t("admin.notifSaving") : t("admin.save")}
              </button>
              <button
                type="button"
                onClick={() => {
                  setNotifEmailEnabled(notifInit.enabled);
                  setNotifSmtpHost(notifInit.smtpHost);
                  setNotifSmtpPort(notifInit.smtpPort);
                  setNotifUsername(notifInit.username);
                  setNotifPassword("");
                  setNotifPasswordAction("keep");
                  setNotifPasswordOpen(false);
                  setNotifFromAddr(notifInit.fromAddr);
                  setNotifFromName(notifInit.fromName);
                  setNotifTls(notifInit.tls);
                  setNotifModified(false);
                  setNotifError("");
                  setNotifSuccess("");
                }}
                disabled={notifSaving}
                className="px-3 py-1.5 rounded-lg text-sm bg-zinc-700 text-white hover:bg-zinc-600 disabled:opacity-50 cursor-pointer"
              >
                {t("admin.revert")}
              </button>
              <button
                type="button"
                onClick={async () => {
                  setNotifTesting(true);
                  setNotifError("");
                  setNotifSuccess("");
                  try {
                    // The button is disabled when the user has no email, so
                    // this value is always a valid target here.
                    const userEmail = currentUser?.email ?? "";
                    const testOpts: NotifTestOptions = {
                      smtp_host: notifSmtpHost,
                      smtp_port: notifSmtpPort,
                      username: notifUsername,
                      from_address: notifFromAddr,
                      from_name: notifFromName,
                      tls: notifTls,
                    };
                    if (notifPasswordAction === "set" && notifPassword !== "") {
                      testOpts.password = notifPassword;
                    } else if (notifPasswordAction === "clear") {
                      testOpts.password = "";
                    }
                    await api.notifications.test("email", [userEmail], { ...testOpts });
                    setNotifSuccess(t("admin.notifTestSuccess"));
                  } catch (err: unknown) {
                    setNotifError(translateApiError(t, err));
                  } finally {
                    setNotifTesting(false);
                  }
                }}
                disabled={notifTesting || !currentUser?.email}
                className="px-3 py-1.5 rounded-lg text-sm bg-zinc-600 text-white hover:bg-zinc-500 disabled:opacity-50 cursor-pointer"
              >
                {notifTesting ? t("admin.notifTesting") : t("admin.notifTest")}
              </button>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
