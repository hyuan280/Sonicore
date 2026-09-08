import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, NavLink, Outlet, useLocation } from "react-router-dom";
import { isAdmin, useAuth } from "../stores/auth";
import { Server, SquareLibrary, Speaker, Database, Bell, Users } from "lucide-react";
import { ROUTES, SETTINGS_TABS, SETTINGS_TAB_STORAGE_KEY, settingsPath } from "../lib/constants";
import LanguageSwitcher from "../components/LanguageSwitcher";

export default function SettingsPage() {
  // Subscribe to the auth store so the guard re-runs when the role changes
  // (e.g. the admin is demoted or the user info is reloaded).
  const user = useAuth((s) => s.user);
  if (!user || !isAdmin()) return <Navigate to={ROUTES.profile} replace />;
  return <SettingsPageInner />;
}

function SettingsPageInner() {
  const { t } = useTranslation();
  const location = useLocation();

  // Remember the current tab so re-entering settings resumes it.
  useEffect(() => {
    for (const tab of Object.values(SETTINGS_TABS)) {
      if (settingsPath(tab) === location.pathname) {
        localStorage.setItem(SETTINGS_TAB_STORAGE_KEY, tab);
        break;
      }
    }
  }, [location.pathname]);

  const tabs = [
    {
      to: settingsPath(SETTINGS_TABS.system),
      icon: Server,
      label: t("settings.tabs.system"),
    },
    {
      to: settingsPath(SETTINGS_TABS.libraries),
      icon: SquareLibrary,
      label: t("settings.tabs.libraries"),
    },
    {
      to: settingsPath(SETTINGS_TABS.devices),
      icon: Speaker,
      label: t("settings.tabs.devices"),
    },
    {
      to: settingsPath(SETTINGS_TABS.sources),
      icon: Database,
      label: t("settings.tabs.sources"),
    },
    {
      to: settingsPath(SETTINGS_TABS.notifications),
      icon: Bell,
      label: t("settings.tabs.notifications"),
    },
    {
      to: settingsPath(SETTINGS_TABS.users),
      icon: Users,
      label: t("settings.tabs.users"),
    },
  ];

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">{t("settings.settings")}</h1>
        <LanguageSwitcher />
      </div>

      <div className="flex gap-1 border-b border-zinc-800 overflow-x-auto pb-1">
        {tabs.map((tab) => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className={({ isActive }) =>
              `flex items-center gap-2 px-3 py-2 text-sm border-b-2 -mb-px whitespace-nowrap transition-colors ${
                isActive
                  ? "border-green-500 text-green-500"
                  : "border-transparent text-zinc-400 hover:text-white"
              }`
            }
          >
            <tab.icon className="w-4 h-4" />
            {tab.label}
          </NavLink>
        ))}
      </div>

      <Outlet />
    </div>
  );
}
