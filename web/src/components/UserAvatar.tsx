import { useEffect, useRef, useState } from "react";
import { UserRound } from "lucide-react";
import { api } from "../api/client";
import {
  AVATAR_CHANGED_EVENT,
  deleteCachedAvatar,
  getCachedAvatar,
  setCachedAvatar,
} from "../lib/avatarCache";

// Self-only component: GET /api/user/avatar always serves the current
// logged-in user's avatar, so this component must not be used to display
// other users' avatars.
export default function UserAvatar({
  avatarFormat,
  className = "w-8 h-8",
}: {
  avatarFormat?: string;
  className?: string;
}) {
  const [url, setUrl] = useState<string | null>(null);
  const urlRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const apply = (blob: Blob | null) => {
      if (cancelled) return;
      if (urlRef.current) URL.revokeObjectURL(urlRef.current);
      const next = blob ? URL.createObjectURL(blob) : null;
      urlRef.current = next;
      setUrl(next);
    };
    const load = () => {
      if (!avatarFormat) {
        apply(null);
        return;
      }
      let pending = getCachedAvatar(avatarFormat);
      if (!pending) {
        pending = api.user.getAvatar().then((blob) => (blob && blob.size > 0 ? blob : null));
        setCachedAvatar(avatarFormat, pending);
      }
      pending
        .then((blob) => {
          // A stale request must not override a newer cached fetch (e.g. one
          // started by an avatar-changed event).
          if (getCachedAvatar(avatarFormat) !== pending) return;
          apply(blob);
        })
        .catch(() => {
          if (getCachedAvatar(avatarFormat) !== pending) return;
          deleteCachedAvatar(avatarFormat);
          apply(null);
        });
    };
    const handler = () => {
      if (avatarFormat) deleteCachedAvatar(avatarFormat);
      load();
    };
    load();
    window.addEventListener(AVATAR_CHANGED_EVENT, handler);
    return () => {
      cancelled = true;
      window.removeEventListener(AVATAR_CHANGED_EVENT, handler);
      if (urlRef.current) {
        URL.revokeObjectURL(urlRef.current);
        urlRef.current = null;
      }
      // Drop the (now revoked) object URL immediately so the placeholder
      // shows instead of a broken image while the next fetch is in flight.
      setUrl(null);
    };
  }, [avatarFormat]);

  if (!url) {
    return (
      <div className={`${className} bg-zinc-800 flex items-center justify-center shrink-0`}>
        <UserRound className="w-1/2 h-1/2 text-zinc-500" />
      </div>
    );
  }
  return <img src={url} alt="" className={`${className} object-cover shrink-0`} />;
}
