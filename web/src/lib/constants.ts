export const APP_VERSION = "v0.0.1";

export const ROUTES = {
  root: "/",
  login: "/login",
  songs: "/songs",
  albums: "/albums",
  artists: "/artists",
  playlists: "/playlists",
  favorites: "/favorites",
  history: "/history",
  discover: "/discover",
  jukebox: "/jukebox",
  player: "/player",
  settings: "/settings",
  plugins: "/plugins",
  tasks: "/tasks",
  profile: "/profile",
  admin: "/admin",
} as const;

export const PLUGIN_TABS = {
  installed: "installed",
  market: "market",
} as const;

export type PluginTab = (typeof PLUGIN_TABS)[keyof typeof PLUGIN_TABS];

export function pluginsPath(tab: PluginTab): string {
  return `${ROUTES.plugins}/${tab}`;
}

export const PLUGIN_SOURCE = {
  official: "official",
  third_party: "third_party",
} as const;

export const PLUGIN_INSTALLED_FILTER_KEYS = {
  status: "status",
  source: "source",
} as const;

export const PLUGIN_INSTALLED_SORT_KEYS = {
  name: "name",
  status: "status",
  updatedAt: "updated_at",
} as const;

export const SETTINGS_TABS = {
  system: "system",
  libraries: "libraries",
  devices: "devices",
  sources: "sources",
  notifications: "notifications",
  users: "users",
} as const;

export type SettingsTab = (typeof SETTINGS_TABS)[keyof typeof SETTINGS_TABS];

export const SETTINGS_TAB_STORAGE_KEY = "settingsTab";

export function settingsPath(tab: SettingsTab): string {
  return `${ROUTES.settings}/${tab}`;
}
