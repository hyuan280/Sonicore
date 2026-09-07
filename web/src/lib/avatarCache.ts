// Shared avatar cache for UserAvatar. The cache must live outside the
// component (module scope) so it survives re-mounts, and outside the auth
// store so the store does not depend on a component.

export const AVATAR_CHANGED_EVENT = "sonicore:avatar-changed";

const cache = new Map<string, Promise<Blob | null>>();
const MAX_CACHE_ENTRIES = 16;

export function getCachedAvatar(key: string): Promise<Blob | null> | undefined {
  return cache.get(key);
}

export function setCachedAvatar(key: string, pending: Promise<Blob | null>) {
  cache.set(key, pending);
  if (cache.size > MAX_CACHE_ENTRIES) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
}

export function deleteCachedAvatar(key: string) {
  cache.delete(key);
}

// clearAvatarCache drops every cached avatar and notifies mounted
// UserAvatar components (sidebar, profile page, ...) to refetch. Called on
// login/register/logout so avatars never leak across accounts.
export function clearAvatarCache() {
  cache.clear();
  window.dispatchEvent(new Event(AVATAR_CHANGED_EVENT));
}
