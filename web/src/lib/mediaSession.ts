import type { PlayerTrack } from "../stores/player";
import { coverImageUrl, performerNames } from "./utils";

export function mediaSessionSupported(): boolean {
  return typeof navigator !== "undefined" && "mediaSession" in navigator;
}

export function setMediaSessionMetadata(track: PlayerTrack | null) {
  if (!mediaSessionSupported()) return;
  if (!track) {
    navigator.mediaSession.metadata = null;
    return;
  }
  const artwork = track.cover_image_id
    ? [
        {
          src: new URL(coverImageUrl(track.cover_image_id, 512), window.location.href).href,
          sizes: "512x512",
          type: "image/jpeg",
        },
      ]
    : [];
  navigator.mediaSession.metadata = new MediaMetadata({
    title: track.title,
    artist: performerNames(track.artists) || "",
    album: track.album || "",
    artwork,
  });
}

export function setMediaSessionPlaybackState(state: "playing" | "paused" | "none") {
  if (!mediaSessionSupported()) return;
  navigator.mediaSession.playbackState = state;
}

export function setMediaSessionPositionState(position: number, duration: number, playbackRate = 1) {
  if (!mediaSessionSupported()) return;
  if (!isFinite(duration) || duration <= 0) return;
  try {
    navigator.mediaSession.setPositionState({
      duration,
      position: Math.max(0, Math.min(position, duration)),
      playbackRate,
    });
  } catch {}
}

export interface MediaSessionActionHandlers {
  play?: () => void;
  pause?: () => void;
  next?: () => void;
  prev?: () => void;
  seekTo?: (time: number) => void;
}

export function bindMediaSessionActions(handlers: MediaSessionActionHandlers) {
  if (!mediaSessionSupported()) return;
  const ms = navigator.mediaSession;
  ms.setActionHandler("play", handlers.play || null);
  ms.setActionHandler("pause", handlers.pause || null);
  ms.setActionHandler("nexttrack", handlers.next || null);
  ms.setActionHandler("previoustrack", handlers.prev || null);
  ms.setActionHandler(
    "seekto",
    handlers.seekTo
      ? (d) => {
          if (d.seekTime != null) handlers.seekTo!(d.seekTime);
        }
      : null,
  );
}

export function clearMediaSessionActions() {
  if (!mediaSessionSupported()) return;
  const ms = navigator.mediaSession;
  for (const action of ["play", "pause", "nexttrack", "previoustrack", "seekto"] as const) {
    try {
      ms.setActionHandler(action, null);
    } catch {}
  }
}
