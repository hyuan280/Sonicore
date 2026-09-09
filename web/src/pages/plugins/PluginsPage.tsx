import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, NavLink, Outlet } from "react-router-dom";
import { MoreVertical, Puzzle, Store, LibraryBig, Settings2 } from "lucide-react";
import { isAdmin, useAuth } from "../../stores/auth";
import { ROUTES, PLUGIN_TABS, pluginsPath } from "../../lib/constants";
import { DropdownMenu, MenuItem } from "../../components/ui/menu";
import { RepoSettingsModal } from "./RepoSettingsModal";
import type { PluginsOutletContext } from "../../types";

export default function PluginsPage() {
  const user = useAuth((s) => s.user);
  if (!user || !isAdmin()) return <Navigate to={ROUTES.profile} replace />;
  return <PluginsPageInner />;
}

function PluginsPageInner() {
  const { t } = useTranslation();
  const [repoSettingsOpen, setRepoSettingsOpen] = useState(false);
  const [toolbar, setToolbar] = useState<ReactNode | null>(null);

  const tabs = [
    {
      to: pluginsPath(PLUGIN_TABS.installed),
      icon: LibraryBig,
      label: t("plugins.tabs.installed"),
    },
    {
      to: pluginsPath(PLUGIN_TABS.market),
      icon: Store,
      label: t("plugins.tabs.market"),
    },
  ];

  const outletContext: PluginsOutletContext = { setToolbar };

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold flex items-center gap-2">
          <Puzzle className="w-6 h-6" />
          {t("plugins.title")}
        </h1>
        <DropdownMenu
          trigger={(toggle, open, ariaProps) => (
            <button
              type="button"
              onClick={toggle}
              {...ariaProps}
              aria-label={t("plugins.menuLabel")}
              className={`p-2 rounded-lg cursor-pointer transition-colors ${
                open ? "bg-zinc-800 text-white" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
              }`}
            >
              <MoreVertical className="w-5 h-5" />
            </button>
          )}
        >
          <MenuItem
            icon={<Settings2 className="w-4 h-4" />}
            onClick={() => setRepoSettingsOpen(true)}
          >
            {t("plugins.repoSettings")}
          </MenuItem>
        </DropdownMenu>
      </div>

      <div className="flex items-center gap-1 border-b border-zinc-800 overflow-x-auto pb-1">
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
        <div className="ml-auto flex items-center gap-2 pl-3 shrink-0">{toolbar}</div>
      </div>

      <Outlet context={outletContext} />

      {repoSettingsOpen && <RepoSettingsModal onClose={() => setRepoSettingsOpen(false)} />}
    </div>
  );
}
