import { Component, lazy, type ReactNode, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Routes, Route, Navigate, Link, useLocation, Outlet } from "react-router-dom";
import { useAuth, isAdmin } from "./stores/auth";
import { useLibrary } from "./stores/library";
import { useJukebox } from "./stores/jukebox";
import { usePlaylists } from "./stores/playlists";
import { usePlayer } from "./stores/player";
import { api } from "./api/client";
import {
  APP_VERSION,
  ROUTES,
  SETTINGS_TABS,
  SETTINGS_TAB_STORAGE_KEY,
  settingsPath,
  PLUGIN_TABS,
  pluginsPath,
  type SettingsTab,
} from "./lib/constants";
import {
  Turntable,
  Music,
  Disc2,
  Mic2,
  ListMusic,
  Heart,
  History,
  Settings,
  ChevronRight,
  Compass,
  UserRound,
  Puzzle,
  ListTodo,
} from "lucide-react";
import i18n from "./i18n";
import Logo from "./components/Logo";
import PlayerBar from "./components/PlayerBar";
import UserAvatar from "./components/UserAvatar";
import { restorePlayerState } from "./stores/player";

const pageFallback = (
  <div className="flex items-center justify-center h-full">
    <div className="animate-spin rounded-full h-8 w-8 border-2 border-zinc-700 border-t-green-500" />
  </div>
);

const PageSuspense = ({ children }: { children: ReactNode }) => (
  <Suspense fallback={pageFallback}>{children}</Suspense>
);

class ErrorBoundary extends Component<{ children: ReactNode }, { hasError: boolean }> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }
  static getDerivedStateFromError() {
    return { hasError: true };
  }
  componentDidCatch(error: Error) {
    console.error("Page error:", error);
  }
  render() {
    if (this.state.hasError) {
      return (
        <div className="flex items-center justify-center h-full">
          <div className="text-center space-y-3">
            <p className="text-zinc-400">{i18n.t("common.errorLoadingPage")}</p>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 rounded-lg bg-green-600 text-white text-sm hover:bg-green-500 transition-colors cursor-pointer"
            >
              {i18n.t("common.retry")}
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

const LoginPage = lazy(() => import("./pages/LoginPage"));
const SongsPage = lazy(() => import("./pages/SongsPage"));
const AlbumsPage = lazy(() => import("./pages/AlbumsPage"));
const AlbumDetailPage = lazy(() => import("./pages/AlbumDetailPage"));
const ArtistsPage = lazy(() => import("./pages/ArtistsPage"));
const ArtistDetailPage = lazy(() => import("./pages/ArtistDetailPage"));
const PlaylistsPage = lazy(() => import("./pages/PlaylistsPage"));
const FavoritesPage = lazy(() => import("./pages/FavoritesPage"));
const HistoryPage = lazy(() => import("./pages/HistoryPage"));
const PlayerPage = lazy(() => import("./pages/PlayerPage"));
const JukeboxPage = lazy(() => import("./pages/JukeboxPage"));
const JukeboxDetailPage = lazy(() => import("./pages/JukeboxDetailPage"));
const PlaylistDetailPage = lazy(() => import("./pages/PlaylistDetailPage"));
const SettingsPage = lazy(() => import("./pages/SettingsPage"));
const ProfilePage = lazy(() => import("./pages/ProfilePage"));
const SystemTab = lazy(() => import("./pages/settings/SystemTab"));
const LibrariesTab = lazy(() => import("./pages/settings/LibrariesTab"));
const DevicesTab = lazy(() => import("./pages/settings/DevicesTab"));
const SourcesTab = lazy(() => import("./pages/settings/SourcesTab"));
const NotificationsTab = lazy(() => import("./pages/settings/NotificationsTab"));
const UsersTab = lazy(() => import("./pages/settings/UsersTab"));
const DiscoverPage = lazy(() => import("./pages/DiscoverPage"));
const DiscoverChartPage = lazy(() => import("./pages/DiscoverChartPage"));
const DiscoverSearchPage = lazy(() => import("./pages/DiscoverSearchPage"));
const DiscoverArtistPage = lazy(() => import("./pages/DiscoverArtistPage"));
const DiscoverTrackPage = lazy(() => import("./pages/DiscoverTrackPage"));
const PluginsPage = lazy(() => import("./pages/plugins/PluginsPage"));
const InstalledPluginsTab = lazy(() => import("./pages/plugins/InstalledTab"));
const PluginMarketTab = lazy(() => import("./pages/plugins/MarketTab"));
const UninstalledPluginsTab = lazy(() => import("./pages/plugins/UninstalledTab"));
const TasksPage = lazy(() => import("./pages/tasks/TasksPage"));

function Sidebar() {
  const location = useLocation();
  const { list: jukeboxes, loadList: loadJukeboxes } = useJukebox();
  const { list: playlists, load: loadPlaylists } = usePlaylists();
  const [plOpen, setPlOpen] = useState(false);
  const [jbxOpen, setJbxOpen] = useState(false);
  const { t } = useTranslation();
  const navItems = [
    { to: ROUTES.songs, icon: Music, label: t("nav.songs") },
    { to: ROUTES.albums, icon: Disc2, label: t("nav.albums") },
    { to: ROUTES.artists, icon: Mic2, label: t("nav.artists") },
    { type: "divider" as const },
    { to: ROUTES.playlists, icon: ListMusic, label: t("nav.playlists") },
  ];
  const navItemsAfter = [
    { to: ROUTES.favorites, icon: Heart, label: t("nav.favorites") },
    { to: ROUTES.history, icon: History, label: t("nav.history") },
  ];

  useEffect(() => {
    if (plOpen) loadPlaylists();
  }, [plOpen, loadPlaylists]);

  useEffect(() => {
    loadJukeboxes();
  }, []);

  const inPlaylist = location.pathname.startsWith(ROUTES.playlists);
  const inJukebox = location.pathname.startsWith(ROUTES.jukebox);
  const currentPlaylistId = usePlayer((s) => s.currentPlaylistId);
  const playing = usePlayer((s) => s.playing);
  const anyJukeboxPlaying = jukeboxes.some((j) => j.is_playing);
  const anyPlaylistActive = currentPlaylistId !== null && playing;
  const isAdminUser = isAdmin();

  return (
    <aside className="w-56 border-r border-zinc-800 flex flex-col bg-zinc-900/50 h-full pb-16">
      <Link
        to={ROUTES.songs}
        className="flex items-center gap-2 px-4 py-4 border-b border-zinc-800"
      >
        <Logo />
        <span className="font-bold">Sonicore</span>
        <span className="text-xs text-zinc-500 self-end pb-0.5 ml-auto">{APP_VERSION}</span>
      </Link>

      <nav className="flex-1 py-2 space-y-1 px-2 overflow-y-auto">
        {navItems.map((item, i) => {
          if ("type" in item) return <div key={i} className="border-t border-zinc-800 my-2" />;
          const active =
            item.to === ROUTES.playlists ? inPlaylist : location.pathname.startsWith(item.to);
          if (item.to === ROUTES.playlists) {
            return (
              <div key={item.to}>
                <Link
                  to={ROUTES.playlists}
                  onClick={() => {
                    if (!plOpen) setPlOpen(true);
                  }}
                  className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors cursor-pointer ${
                    active
                      ? "bg-green-600/20 text-green-500"
                      : "text-zinc-400 hover:text-white hover:bg-zinc-800"
                  }`}
                >
                  <item.icon className="w-4 h-4" />
                  <span className="flex-1">{item.label}</span>
                  {anyPlaylistActive && (
                    <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />
                  )}
                  <ChevronRight
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setPlOpen(!plOpen);
                    }}
                    className={`w-3.5 h-3.5 transition-transform ${plOpen ? "rotate-90" : ""}`}
                  />
                </Link>
                {plOpen && (
                  <div className="ml-2 mt-1 space-y-0.5">
                    {playlists.map((p) => (
                      <Link
                        key={p.id}
                        to={`${ROUTES.playlists}/${p.id}`}
                        className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm transition-colors ${
                          location.pathname === `${ROUTES.playlists}/${p.id}`
                            ? "text-green-500 bg-green-600/10"
                            : "text-zinc-500 hover:text-white hover:bg-zinc-800"
                        }`}
                      >
                        <span className="flex-1 truncate">{p.name}</span>
                        {currentPlaylistId === p.id && playing && (
                          <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />
                        )}
                      </Link>
                    ))}
                  </div>
                )}
              </div>
            );
          }
          return (
            <Link
              key={item.to}
              to={item.to}
              className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
                active
                  ? "bg-green-600/20 text-green-500"
                  : "text-zinc-400 hover:text-white hover:bg-zinc-800"
              }`}
            >
              <item.icon className="w-4 h-4" />
              {item.label}
            </Link>
          );
        })}

        <div>
          <Link
            to={ROUTES.jukebox}
            onClick={() => {
              if (!jbxOpen) setJbxOpen(true);
            }}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors cursor-pointer ${
              inJukebox
                ? "bg-green-600/20 text-green-500"
                : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}
          >
            <Turntable className="w-4 h-4" />
            <span className="flex-1">{t("nav.jukeboxes")}</span>
            {anyJukeboxPlaying && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
            <ChevronRight
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setJbxOpen(!jbxOpen);
              }}
              className={`w-3.5 h-3.5 transition-transform ${jbxOpen ? "rotate-90" : ""}`}
            />
          </Link>
          {jbxOpen && (
            <div className="ml-2 mt-1 space-y-0.5">
              {jukeboxes.map((j) => (
                <Link
                  key={j.id}
                  to={`${ROUTES.jukebox}/${j.id}`}
                  className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm transition-colors ${
                    location.pathname === `${ROUTES.jukebox}/${j.id}`
                      ? "text-green-500 bg-green-600/10"
                      : "text-zinc-500 hover:text-white hover:bg-zinc-800"
                  }`}
                >
                  <span className="flex-1 truncate">{j.name}</span>
                  {j.is_playing && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
                </Link>
              ))}
            </div>
          )}
        </div>

        <div className="border-t border-zinc-800 my-2" />
        <Link
          to={ROUTES.discover}
          className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
            location.pathname.startsWith(ROUTES.discover)
              ? "bg-green-600/20 text-green-500"
              : "text-zinc-400 hover:text-white hover:bg-zinc-800"
          }`}
        >
          <Compass className="w-4 h-4" />
          {t("nav.discover")}
        </Link>

        <div className="border-t border-zinc-800 my-2" />
        {navItemsAfter.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname.startsWith(item.to)
                ? "bg-green-600/20 text-green-500"
                : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}
          >
            <item.icon className="w-4 h-4" />
            {item.label}
          </Link>
        ))}

        <div className="border-t border-zinc-800 my-2" />
        {isAdminUser && (
          <Link
            to={ROUTES.tasks}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname.startsWith(ROUTES.tasks)
                ? "bg-green-600/20 text-green-500"
                : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}
          >
            <ListTodo className="w-4 h-4" />
            {t("nav.tasks")}
          </Link>
        )}
        {isAdminUser && (
          <Link
            to={pluginsPath(PLUGIN_TABS.installed)}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname.startsWith(ROUTES.plugins)
                ? "bg-green-600/20 text-green-500"
                : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}
          >
            <Puzzle className="w-4 h-4" />
            {t("nav.plugins")}
          </Link>
        )}
        {isAdminUser && (
          <Link
            to={ROUTES.settings}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname.startsWith(ROUTES.settings)
                ? "bg-green-600/20 text-green-500"
                : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}
          >
            <Settings className="w-4 h-4" />
            {t("nav.settings")}
          </Link>
        )}
      </nav>

      <div className="border-t border-zinc-800 mx-2" />
      <div className="px-2 py-2 space-y-1">
        <ProfileEntry />
      </div>
    </aside>
  );
}

function ProfileEntry() {
  const location = useLocation();
  const { user } = useAuth();
  if (!user) return null;
  const active = location.pathname === ROUTES.profile;
  return (
    <Link
      to={ROUTES.profile}
      title={user.username}
      className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
        active
          ? "bg-green-600/20 text-green-500"
          : "text-zinc-400 hover:text-white hover:bg-zinc-800"
      }`}
    >
      {user.avatar_format ? (
        <UserAvatar avatarFormat={user.avatar_format} className="w-4 h-4 rounded-md" />
      ) : (
        <UserRound className="w-4 h-4 shrink-0" />
      )}
      <span className="flex-1 truncate">{user.username}</span>
    </Link>
  );
}

function SettingsIndex() {
  // Resume the last visited tab; fall back to the system tab.
  const saved = localStorage.getItem(SETTINGS_TAB_STORAGE_KEY);
  const tab = Object.values(SETTINGS_TABS).includes(saved as SettingsTab)
    ? (saved as SettingsTab)
    : SETTINGS_TABS.system;
  return <Navigate to={settingsPath(tab)} replace />;
}

function Layout() {
  const location = useLocation();
  return (
    <div className="h-screen flex flex-col bg-black">
      <div className="flex flex-1 overflow-hidden">
        <Sidebar />
        <main className="flex-1 overflow-y-auto pb-16">
          <ErrorBoundary key={location.pathname}>
            <PageSuspense>
              <Outlet />
            </PageSuspense>
          </ErrorBoundary>
        </main>
      </div>
      <PlayerBar />
    </div>
  );
}

export default function App() {
  const { token, loadUser } = useAuth();
  const { load: loadLibs, libraries } = useLibrary();

  useEffect(() => {
    if (token) {
      loadUser();
      loadLibs();
      restorePlayerState();
    }
  }, [token]);

  useEffect(() => {
    if (!token) return;
    const renew = () => {
      const st = localStorage.getItem("session_token") || undefined;
      api.auth
        .renewSession(st)
        .then((d) => {
          if (d.session_token) localStorage.setItem("session_token", d.session_token);
        })
        .catch((e) => console.error("session renew failed", e));
    };
    renew();
    const id = setInterval(renew, 60 * 1000);
    const onVisible = () => {
      if (document.visibilityState === "visible") renew();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [token]);

  const hasLibraries = libraries.length > 0;

  return (
    <Routes>
      <Route
        path={ROUTES.login}
        element={
          token ? (
            <Navigate to={hasLibraries ? ROUTES.songs : ROUTES.settings} replace />
          ) : (
            <ErrorBoundary>
              <PageSuspense>
                <LoginPage />
              </PageSuspense>
            </ErrorBoundary>
          )
        }
      />
      <Route
        path={ROUTES.root}
        element={token ? <Layout /> : <Navigate to={ROUTES.login} replace />}
      >
        <Route index element={<Navigate to={ROUTES.songs} replace />} />
        <Route path={ROUTES.songs} element={<SongsPage />} />
        <Route path={ROUTES.albums} element={<AlbumsPage />} />
        <Route path={`${ROUTES.albums}/:albumId`} element={<AlbumDetailPage />} />
        <Route path={ROUTES.artists} element={<ArtistsPage />} />
        <Route path={`${ROUTES.artists}/:artistId`} element={<ArtistDetailPage />} />
        <Route path={ROUTES.playlists} element={<PlaylistsPage />} />
        <Route path={`${ROUTES.playlists}/:id`} element={<PlaylistDetailPage />} />
        <Route path={ROUTES.favorites} element={<FavoritesPage />} />
        <Route path={ROUTES.history} element={<HistoryPage />} />
        <Route path={ROUTES.discover} element={<DiscoverPage />} />
        <Route
          path={`${ROUTES.discover}/charts/:platform/:chartId`}
          element={<DiscoverChartPage />}
        />
        <Route path={`${ROUTES.discover}/search/:platform`} element={<DiscoverSearchPage />} />
        <Route
          path={`${ROUTES.discover}/artists/:platform/:artistId`}
          element={<DiscoverArtistPage />}
        />
        <Route
          path={`${ROUTES.discover}/tracks/:platform/:trackId`}
          element={<DiscoverTrackPage />}
        />
        <Route path={ROUTES.jukebox} element={<JukeboxPage />} />
        <Route path={`${ROUTES.jukebox}/:id`} element={<JukeboxDetailPage />} />
        <Route path={ROUTES.player} element={<PlayerPage />} />
        <Route path={ROUTES.settings} element={<SettingsPage />}>
          <Route index element={<SettingsIndex />} />
          <Route path={SETTINGS_TABS.system} element={<SystemTab />} />
          <Route path={SETTINGS_TABS.libraries} element={<LibrariesTab />} />
          <Route path={SETTINGS_TABS.devices} element={<DevicesTab />} />
          <Route path={SETTINGS_TABS.sources} element={<SourcesTab />} />
          <Route path={SETTINGS_TABS.notifications} element={<NotificationsTab />} />
          <Route path={SETTINGS_TABS.users} element={<UsersTab />} />
        </Route>
        <Route path={ROUTES.tasks} element={<TasksPage />} />
        <Route path={ROUTES.plugins} element={<PluginsPage />}>
          <Route index element={<Navigate to={pluginsPath(PLUGIN_TABS.installed)} replace />} />
          <Route path={PLUGIN_TABS.installed} element={<InstalledPluginsTab />} />
          <Route path={PLUGIN_TABS.market} element={<PluginMarketTab />} />
          <Route path={PLUGIN_TABS.uninstalled} element={<UninstalledPluginsTab />} />
        </Route>
        <Route path={ROUTES.profile} element={<ProfilePage />} />
        <Route path={ROUTES.admin} element={<Navigate to={ROUTES.settings} replace />} />
      </Route>
      <Route path="*" element={<Navigate to={ROUTES.root} replace />} />
    </Routes>
  );
}
