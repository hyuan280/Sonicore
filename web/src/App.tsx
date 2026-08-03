import { useEffect, useState } from "react"
import { Routes, Route, Navigate, Link, useLocation, Outlet } from "react-router-dom"
import { useAuth } from "./stores/auth"
import { useLibrary } from "./stores/library"
import { useJukebox } from "./stores/jukebox"
import { usePlaylists } from "./stores/playlists"
import { usePlayer } from "./stores/player"
import { api } from "./api/client"
import { APP_VERSION } from "./lib/constants"
import {
  Turntable, Music, Disc2, Mic2, ListMusic, Heart, History, Settings, LogOut, Shield,
  ChevronRight,
} from "lucide-react"
import LoginPage from "./pages/LoginPage"
import Logo from "./components/Logo"
import SongsPage from "./pages/SongsPage"
import AlbumsPage from "./pages/AlbumsPage"
import AlbumDetailPage from "./pages/AlbumDetailPage"
import ArtistsPage from "./pages/ArtistsPage"
import ArtistDetailPage from "./pages/ArtistDetailPage"
import PlaylistsPage from "./pages/PlaylistsPage"
import FavoritesPage from "./pages/FavoritesPage"
import HistoryPage from "./pages/HistoryPage"
import PlayerPage from "./pages/PlayerPage"
import JukeboxPage from "./pages/JukeboxPage"
import JukeboxDetailPage from "./pages/JukeboxDetailPage"
import PlaylistDetailPage from "./pages/PlaylistDetailPage"
import PlayerBar from "./components/PlayerBar"
import { restorePlayerState } from "./stores/player"
import SettingsPage from "./pages/SettingsPage"
import AdminPage from "./pages/AdminPage"

const navItems = [
  { to: "/songs", icon: Music, label: "Songs" },
  { to: "/albums", icon: Disc2, label: "Albums" },
  { to: "/artists", icon: Mic2, label: "Artists" },
  { type: "divider" as const },
  { to: "/playlists", icon: ListMusic, label: "Playlists" },
]

const navItemsAfter = [
  { to: "/favorites", icon: Heart, label: "Favorites" },
  { to: "/history", icon: History, label: "History" },
]

function Sidebar() {
  const location = useLocation()
  const { list: jukeboxes, loadList: loadJukeboxes } = useJukebox()
  const { list: playlists, load: loadPlaylists } = usePlaylists()
  const [plOpen, setPlOpen] = useState(false)
  const [jbxOpen, setJbxOpen] = useState(false)

  useEffect(() => {
    if (plOpen) loadPlaylists()
  }, [plOpen, loadPlaylists])

  useEffect(() => {
    loadJukeboxes()
  }, [])

  const role = localStorage.getItem("role")
  const isAdmin = role === "admin" || role === "super_admin"
  const inPlaylist = location.pathname.startsWith("/playlists")
  const inJukebox = location.pathname.startsWith("/jukebox")
  const currentPlaylistId = usePlayer(s => s.currentPlaylistId)
  const playing = usePlayer(s => s.playing)
  const anyJukeboxPlaying = jukeboxes.some(j => j.is_playing)
  const anyPlaylistActive = currentPlaylistId !== null && playing

  return (
    <aside className="w-56 border-r border-zinc-800 flex flex-col bg-zinc-900/50 h-full pb-16">
      <Link to="/songs" className="flex items-center gap-2 px-4 py-4 border-b border-zinc-800">
        <Logo />
        <span className="font-bold">Sonicore</span>
        <span className="text-xs text-zinc-500 self-end pb-0.5 ml-auto">{APP_VERSION}</span>
      </Link>

      <nav className="flex-1 py-2 space-y-1 px-2 overflow-y-auto">
        {navItems.map((item, i) => {
          if ("type" in item) return <div key={i} className="border-t border-zinc-800 my-2" />
          const active = item.to === "/playlists" ? inPlaylist : location.pathname.startsWith(item.to)
          if (item.to === "/playlists") {
            return (
              <div key={item.to}>
                <Link to="/playlists" onClick={() => { if (!plOpen) setPlOpen(true) }}
                  className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors cursor-pointer ${
                    active ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
                  }`}>
                  <item.icon className="w-4 h-4" />
                  <span className="flex-1">{item.label}</span>
                  {anyPlaylistActive && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
                  <ChevronRight onClick={e => { e.preventDefault(); e.stopPropagation(); setPlOpen(!plOpen) }}
                    className={`w-3.5 h-3.5 transition-transform ${plOpen ? "rotate-90" : ""}`} />
                </Link>
                {plOpen && (
                  <div className="ml-2 mt-1 space-y-0.5">
                    {playlists.map(p => (
                      <Link key={p.id} to={`/playlists/${p.id}`}
                        className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm transition-colors ${
                          location.pathname === `/playlists/${p.id}` ? "text-green-500 bg-green-600/10" : "text-zinc-500 hover:text-white hover:bg-zinc-800"
                        }`}>
                        <span className="flex-1 truncate">{p.name}</span>
                        {currentPlaylistId === p.id && playing && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
                      </Link>
                    ))}
                  </div>
                )}
              </div>
            )
          }
          return (
            <Link key={item.to} to={item.to}
              className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
                active ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
              }`}>
              <item.icon className="w-4 h-4" />
              {item.label}
            </Link>
          )
        })}

        <div>
          <Link to="/jukebox" onClick={() => { if (!jbxOpen) setJbxOpen(true) }}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors cursor-pointer ${
              inJukebox ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}>
            <Turntable className="w-4 h-4" />
            <span className="flex-1">Jukeboxes</span>
            {anyJukeboxPlaying && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
            <ChevronRight onClick={e => { e.preventDefault(); e.stopPropagation(); setJbxOpen(!jbxOpen) }}
              className={`w-3.5 h-3.5 transition-transform ${jbxOpen ? "rotate-90" : ""}`} />
          </Link>
          {jbxOpen && (
            <div className="ml-2 mt-1 space-y-0.5">
              {jukeboxes.map(j => (
                <Link key={j.id} to={`/jukebox/${j.id}`}
                  className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm transition-colors ${
                    location.pathname === `/jukebox/${j.id}` ? "text-green-500 bg-green-600/10" : "text-zinc-500 hover:text-white hover:bg-zinc-800"
                  }`}>
                  <span className="flex-1 truncate">{j.name}</span>
                  {j.is_playing && <div className="w-2 h-2 rounded-full bg-green-500 shrink-0" />}
                </Link>
              ))}
            </div>
          )}
        </div>

        <div className="border-t border-zinc-800 my-2" />
        {navItemsAfter.map((item) => (
          <Link key={item.to} to={item.to}
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname.startsWith(item.to) ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}>
            <item.icon className="w-4 h-4" />
            {item.label}
          </Link>
        ))}

        <div className="border-t border-zinc-800 my-2" />
        <Link to="/settings"
          className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
            location.pathname === "/settings" ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
          }`}>
          <Settings className="w-4 h-4" />
          Settings
        </Link>
      </nav>

      {isAdmin && (
        <div className="border-t border-zinc-800 mx-2" />
      )}
      <div className="px-2 py-2 space-y-1">
        {isAdmin && (
          <Link to="/admin"
            className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
              location.pathname === "/admin" ? "bg-green-600/20 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800"
            }`}>
            <Shield className="w-4 h-4" />
            Administration
          </Link>
        )}
      </div>

      <div className="border-t border-zinc-800 mx-2" />
      <div className="px-2 py-2 space-y-1">
        <LogoutButton />
      </div>
    </aside>
  )
}

function LogoutButton() {
  const { logout, user } = useAuth()
  return (
    <button onClick={logout}
      className="flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors w-full text-left cursor-pointer text-zinc-400 hover:text-red-400 hover:bg-zinc-800">
      <LogOut className="w-4 h-4" />
      {user?.username || "Sign out"}
    </button>
  )
}

function Layout() {
  return (
    <div className="h-screen flex flex-col bg-black">
      <div className="flex flex-1 overflow-hidden">
        <Sidebar />
        <main className="flex-1 overflow-y-auto pb-16"><Outlet /></main>
      </div>
      <PlayerBar />
    </div>
  )
}

export default function App() {
  const { token, loadUser } = useAuth()
  const { load: loadLibs, libraries } = useLibrary()

  useEffect(() => {
    if (token) {
      loadUser()
      loadLibs()
      restorePlayerState()
    }
  }, [token])

  const hasLibraries = libraries.length > 0

  return (
    <Routes>
      <Route path="/login" element={token ? <Navigate to={hasLibraries ? "/songs" : "/settings"} replace /> : <LoginPage />} />
      <Route path="/" element={token ? <Layout /> : <Navigate to="/login" replace />}>
        <Route index element={<Navigate to="/songs" replace />} />
        <Route path="songs" element={<SongsPage />} />
        <Route path="albums" element={<AlbumsPage />} />
        <Route path="albums/:albumId" element={<AlbumDetailPage />} />
        <Route path="artists" element={<ArtistsPage />} />
        <Route path="artists/:artistId" element={<ArtistDetailPage />} />
        <Route path="playlists" element={<PlaylistsPage />} />
        <Route path="playlists/:id" element={<PlaylistDetailPage />} />
        <Route path="favorites" element={<FavoritesPage />} />
        <Route path="history" element={<HistoryPage />} />
        <Route path="jukebox" element={<JukeboxPage />} />
        <Route path="jukebox/:id" element={<JukeboxDetailPage />} />
        <Route path="player" element={<PlayerPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="admin" element={<AdminPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
