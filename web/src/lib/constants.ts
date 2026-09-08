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
  profile: "/profile",
  admin: "/admin",
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
