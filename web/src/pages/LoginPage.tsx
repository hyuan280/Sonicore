import { useState, useEffect } from "react"
import { useNavigate } from "react-router-dom"
import { useAuth } from "../stores/auth"
import { Button } from "../components/ui/button"
import { Input } from "../components/ui/input"
import Logo from "../components/Logo"

export default function LoginPage() {
  const [tab, setTab] = useState<"login" | "register">("login")
  const [username, setUsername] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const { login, register, allowRegistration, loadRegistrationStatus } = useAuth()
  const navigate = useNavigate()

  useEffect(() => { loadRegistrationStatus() }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault(); setError("")
    try {
      if (tab === "login") await login(username, password)
      else await register(username, email, password)
      navigate("/libraries")
    } catch (err: any) { setError(err.error || "login failed") }
  }

  const tabs = allowRegistration ? (["login", "register"] as const) : (["login"] as const)

  return (
    <div className="min-h-screen flex items-center justify-center bg-black">
      <div className="w-full max-w-sm space-y-6 px-4">
        <div className="text-center space-y-3">
          <Logo className="w-12 h-12 mx-auto" />
          <h1 className="text-3xl font-bold">Sonicore</h1>
          <p className="text-zinc-400 text-sm">your music library, the core of sound</p>
        </div>

        {tabs.length > 1 && (
          <div className="flex rounded-lg bg-zinc-900 p-1">
            {tabs.map(t => (
              <button key={t} onClick={() => setTab(t)}
                className={`flex-1 py-2 text-sm rounded-md transition-colors cursor-pointer ${tab === t ? "bg-green-600 text-white" : "text-zinc-400 hover:text-white"}`}>
                {t === "login" ? "Sign In" : "Register"}
              </button>
            ))}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <Input placeholder="Username" value={username} onChange={e => setUsername(e.target.value)} required />
          {tab === "register" && <Input type="email" placeholder="Email" value={email} onChange={e => setEmail(e.target.value)} required />}
          <Input type="password" placeholder="Password" value={password} onChange={e => setPassword(e.target.value)} required />
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <Button type="submit" className="w-full">{tab === "login" ? "Sign In" : "Create Account"}</Button>
        </form>
      </div>
    </div>
  )
}
