import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { useAuth } from "../../stores/auth";
import { Card } from "../../components/ui/card";
import { api } from "../../api/client";
import { UserRound } from "lucide-react";

interface AdminUser {
  id: string;
  username: string;
  email: string;
  role: "super_admin" | "admin" | "user";
  avatar_format?: string;
}

// Module-level promise cache: rendering N user cards must not fire N avatar
// requests. Each component still owns (and revokes) its object URL, the
// blob itself is shared.
const avatarBlobCache = new Map<string, Promise<Blob | null>>();

function getAvatarBlob(userId: string): Promise<Blob | null> {
  let pending = avatarBlobCache.get(userId);
  if (!pending) {
    pending = api.admin
      .getUserAvatar(userId)
      .then((blob) => (blob && blob.size > 0 ? blob : null))
      .catch(() => null);
    avatarBlobCache.set(userId, pending);
  }
  return pending;
}

// Fetches another user's avatar via the admin endpoint (the self-service
// /api/user/avatar always serves the current user).
function AdminUserAvatar({
  userId,
  hasAvatar,
  className = "w-10 h-10 rounded-lg",
}: {
  userId: string;
  hasAvatar?: boolean;
  className?: string;
}) {
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let objectUrl: string | null = null;
    if (!hasAvatar) {
      setUrl(null);
      return;
    }
    getAvatarBlob(userId).then((blob) => {
      if (cancelled) return;
      if (blob) {
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      } else {
        setUrl(null);
      }
    });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [userId, hasAvatar]);

  if (!url) {
    return (
      <div className={`${className} bg-zinc-800 flex items-center justify-center shrink-0`}>
        <UserRound className="w-1/2 h-1/2 text-zinc-500" />
      </div>
    );
  }
  return <img src={url} alt="" className={`${className} object-cover shrink-0`} />;
}

export default function UsersTab() {
  const { t } = useTranslation();
  const currentUser = useAuth((s) => s.user);
  const currentRole = currentUser?.role;
  const roleLabels: Record<string, string> = {
    super_admin: t("admin.roleSuperAdmin"),
    admin: t("admin.roleAdmin"),
    user: t("admin.roleUser"),
  };
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [error, setError] = useState("");

  const loadData = useCallback(async () => {
    try {
      const u = await api.admin.users();
      setUsers(Array.isArray(u?.users) ? u.users : []);
      setError("");
    } catch (err) {
      setError(translateApiError(t, err));
    }
  }, [t]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  async function updateRole(id: string, role: AdminUser["role"]) {
    setPendingId(id);
    setError("");
    try {
      await api.admin.updateRole(id, role);
      await loadData();
    } catch (err) {
      setError(translateApiError(t, err));
    } finally {
      setPendingId(null);
    }
  }

  return (
    <div className="space-y-4">
      {error && <p className="text-sm text-red-400">{error}</p>}
      {users.map((u) => (
        <Card key={u.id} className="p-4">
          <div className="flex items-center gap-3">
            <AdminUserAvatar userId={u.id} hasAvatar={!!u.avatar_format} />
            <div className="flex-1 min-w-0">
              <div className="text-sm font-medium truncate">{u.username}</div>
              <div className="text-xs text-zinc-500 truncate">{u.email}</div>
            </div>
            <span className="text-xs px-2 py-0.5 rounded-full bg-zinc-800 text-zinc-400 shrink-0">
              {roleLabels[u.role] ?? t("admin.roleUser")}
            </span>
            <div className="flex gap-2 w-36 justify-end shrink-0">
              {currentRole === "super_admin" && u.role !== "super_admin" && (
                <>
                  {u.role !== "admin" && (
                    <button
                      type="button"
                      onClick={() => updateRole(u.id, "admin")}
                      disabled={pendingId !== null}
                      className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50"
                    >
                      {t("admin.promote")}
                    </button>
                  )}
                  {u.role !== "user" && (
                    <button
                      type="button"
                      onClick={() => updateRole(u.id, "user")}
                      disabled={pendingId !== null}
                      className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50"
                    >
                      {t("admin.demote")}
                    </button>
                  )}
                </>
              )}
              {currentRole === "admin" && u.role === "user" && (
                <button
                  type="button"
                  onClick={() => updateRole(u.id, "admin")}
                  disabled={pendingId !== null}
                  className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer disabled:opacity-50"
                >
                  {t("admin.promote")}
                </button>
              )}
            </div>
          </div>
        </Card>
      ))}
    </div>
  );
}
