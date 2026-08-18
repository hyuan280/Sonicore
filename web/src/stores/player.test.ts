import { describe, expect, it, beforeEach, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: {
    user: {
      updateSettings: vi.fn(),
      saveQueue: vi.fn(),
      getQueue: vi.fn(),
      getSettings: vi.fn(),
    },
    data: {
      lyrics: vi.fn(),
      updateLyricsOffset: vi.fn(),
    },
  },
}));

import { api } from "../api/client";
import { usePlayer } from "./player";
import type { PlayerTrack } from "./player";

const mockedUser = vi.mocked(api.user);
const mockedData = vi.mocked(api.data);

function track(id: string): PlayerTrack {
  return { id, title: `Track ${id}`, duration: 100, suffix: "mp3" };
}

const initialState = {
  track: null as PlayerTrack | null,
  queue: [] as PlayerTrack[],
  queueIdx: 0,
  shuffleOrder: [] as number[],
  shuffleIdx: 0,
  playing: false,
  position: 0,
  volume: 0.8,
  mode: "normal" as "normal" | "all" | "one" | "shuffle",
  playEpoch: 0,
  currentPlaylistId: null as string | null,
  lyrics: "",
  lyricsFormat: "",
  lyricsOffset: 0,
};

function isPermutation(order: number[], length: number): boolean {
  if (order.length !== length) return false;
  const sorted = [...order].sort((a, b) => a - b);
  return sorted.every((v, i) => v === i);
}

describe("player store", () => {
  beforeEach(() => {
    usePlayer.setState(initialState);
    localStorage.clear();
    vi.clearAllMocks();
    mockedUser.updateSettings.mockResolvedValue(null);
    mockedUser.saveQueue.mockResolvedValue(null);
    mockedData.lyrics.mockResolvedValue({ lyrics: "", format: "lrc", lyrics_offset: 0 });
    mockedData.updateLyricsOffset.mockResolvedValue(null);
  });

  describe("setQueue", () => {
    it("sets queue with start index in normal mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")], 1);
      const s = usePlayer.getState();
      expect(s.queue.map((t) => t.id)).toEqual(["a", "b", "c"]);
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("b");
      expect(s.playing).toBe(true);
      expect(s.position).toBe(0);
      expect(s.playEpoch).toBe(1);
    });

    it("persists to localStorage and server", () => {
      usePlayer.getState().setQueue([track("a")]);
      expect(localStorage.getItem("playerState")).toContain('"volume"');
      expect(localStorage.getItem("playerQueue")).toContain('"tracks"');
      expect(mockedUser.saveQueue).toHaveBeenCalled();
      expect(mockedUser.updateSettings).toHaveBeenCalled();
    });

    it("generates shuffle order and starts at order head in shuffle mode", () => {
      usePlayer.setState({ mode: "shuffle" });
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      const s = usePlayer.getState();
      expect(isPermutation(s.shuffleOrder, 3)).toBe(true);
      expect(s.shuffleIdx).toBe(0);
      expect(s.queueIdx).toBe(s.shuffleOrder[0]);
      expect(s.track?.id).toBe(s.queue[s.queueIdx].id);
    });

    it("clears lyrics state when switching tracks", () => {
      usePlayer.setState({ lyrics: "old", lyricsFormat: "lrc", lyricsOffset: 2 });
      usePlayer.getState().setQueue([track("a")]);
      const s = usePlayer.getState();
      expect(s.lyrics).toBe("");
      expect(s.lyricsFormat).toBe("");
      expect(s.lyricsOffset).toBe(0);
    });

    it("records playlist id", () => {
      usePlayer.getState().setQueue([track("a")], 0, "pl1");
      expect(usePlayer.getState().currentPlaylistId).toBe("pl1");
    });

    it("fetches lyrics for first track", () => {
      usePlayer.getState().setQueue([track("a")]);
      expect(mockedData.lyrics).toHaveBeenCalledWith("a");
    });
  });

  describe("play / playIndex", () => {
    it("play sets track playing and bumps epoch", () => {
      usePlayer.getState().play(track("x"));
      const s = usePlayer.getState();
      expect(s.track?.id).toBe("x");
      expect(s.playing).toBe(true);
      expect(s.position).toBe(0);
      expect(s.playEpoch).toBe(1);
      expect(mockedData.lyrics).toHaveBeenCalledWith("x");
    });

    it("playIndex ignores out-of-range indices", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().playIndex(5);
      expect(usePlayer.getState().track?.id).toBe("a");
    });

    it("playIndex aligns shuffle index in shuffle mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.setState({ mode: "shuffle", shuffleOrder: [1, 2, 0], shuffleIdx: 0 });
      usePlayer.getState().playIndex(2);
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(2);
      expect(s.shuffleIdx).toBe(1);
    });
  });

  describe("advanceTrack", () => {
    it("is a no-op on empty queue", () => {
      usePlayer.getState().advanceTrack();
      expect(usePlayer.getState().playEpoch).toBe(0);
    });

    it("replays current track in one mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.setState({ mode: "one" });
      usePlayer.getState().advanceTrack();
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(0);
      expect(s.track?.id).toBe("a");
      expect(s.playing).toBe(true);
      expect(s.position).toBe(0);
    });

    it("advances through shuffle order", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.setState({ mode: "shuffle", shuffleOrder: [2, 0, 1], shuffleIdx: 1, queueIdx: 0 });
      usePlayer.getState().advanceTrack();
      const s = usePlayer.getState();
      expect(s.shuffleIdx).toBe(2);
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("b");
    });

    it("stops at end of queue in normal mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.getState().playIndex(1);
      usePlayer.getState().advanceTrack();
      const s = usePlayer.getState();
      expect(s.playing).toBe(false);
      expect(s.queueIdx).toBe(1);
    });

    it("wraps around in all mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.setState({ mode: "all" });
      usePlayer.getState().playIndex(1);
      usePlayer.getState().advanceTrack();
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(0);
      expect(s.track?.id).toBe("a");
      expect(s.playing).toBe(true);
    });

    it("advances to next in normal mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.getState().advanceTrack();
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("b");
      expect(s.playing).toBe(true);
    });
  });

  describe("next / prev", () => {
    it("next wraps at end in normal mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.getState().playIndex(1);
      usePlayer.getState().next();
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(0);
      expect(s.track?.id).toBe("a");
    });

    it("next follows shuffle order", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.setState({ mode: "shuffle", shuffleOrder: [2, 0, 1], shuffleIdx: 1, queueIdx: 0 });
      usePlayer.getState().next();
      const s = usePlayer.getState();
      expect(s.shuffleIdx).toBe(2);
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("b");
    });

    it("prev wraps to last track", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.getState().prev();
      const s = usePlayer.getState();
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("b");
    });

    it("prev goes back through shuffle order", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.setState({ mode: "shuffle", shuffleOrder: [2, 0, 1], shuffleIdx: 1, queueIdx: 0 });
      usePlayer.getState().prev();
      const s = usePlayer.getState();
      expect(s.shuffleIdx).toBe(0);
      expect(s.queueIdx).toBe(2);
      expect(s.track?.id).toBe("c");
    });

    it("next is a no-op on empty queue", () => {
      usePlayer.getState().next();
      expect(usePlayer.getState().track).toBeNull();
    });
  });

  describe("togglePlay", () => {
    it("pauses when playing", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().togglePlay();
      expect(usePlayer.getState().playing).toBe(false);
    });

    it("resumes when paused with a track", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.setState({ playing: false });
      usePlayer.getState().togglePlay();
      expect(usePlayer.getState().playing).toBe(true);
    });

    it("starts first queue track when nothing playing", () => {
      usePlayer.setState({ queue: [track("a"), track("b")] });
      usePlayer.getState().togglePlay();
      const s = usePlayer.getState();
      expect(s.playing).toBe(true);
      expect(s.track?.id).toBe("a");
      expect(s.queueIdx).toBe(0);
    });

    it("starts shuffle head when in shuffle mode", () => {
      usePlayer.setState({
        mode: "shuffle",
        shuffleOrder: [1, 0],
        shuffleIdx: 0,
        queue: [track("a"), track("b")],
      });
      usePlayer.getState().togglePlay();
      expect(usePlayer.getState().track?.id).toBe("b");
    });
  });

  describe("cycleMode", () => {
    it("cycles normal → all → one → shuffle → normal", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      const s = () => usePlayer.getState();
      s().cycleMode();
      expect(s().mode).toBe("all");
      s().cycleMode();
      expect(s().mode).toBe("one");
      s().cycleMode();
      expect(s().mode).toBe("shuffle");
      s().cycleMode();
      expect(s().mode).toBe("normal");
    });

    it("builds a shuffle order aligned with current position on entering shuffle", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.getState().playIndex(2);
      usePlayer.setState({ mode: "one" });
      usePlayer.getState().cycleMode();
      const s = usePlayer.getState();
      expect(s.mode).toBe("shuffle");
      expect(isPermutation(s.shuffleOrder, 3)).toBe(true);
      expect(s.shuffleOrder[s.shuffleIdx]).toBe(s.queueIdx);
    });

    it("clears shuffle order when leaving shuffle mode", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.setState({ mode: "shuffle", shuffleOrder: [1, 0], shuffleIdx: 1 });
      usePlayer.getState().cycleMode();
      const s = usePlayer.getState();
      expect(s.mode).toBe("normal");
      expect(s.shuffleOrder).toEqual([]);
      expect(s.shuffleIdx).toBe(0);
    });

    it("works with empty queue entering shuffle", () => {
      usePlayer.setState({ mode: "one" });
      usePlayer.getState().cycleMode();
      expect(usePlayer.getState().mode).toBe("shuffle");
      expect(usePlayer.getState().shuffleOrder).toEqual([]);
    });
  });

  describe("addToQueue", () => {
    it("appends new tracks", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().addToQueue([track("b"), track("c")]);
      expect(usePlayer.getState().queue.map((t) => t.id)).toEqual(["a", "b", "c"]);
    });

    it("deduplicates tracks already in queue", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().addToQueue([track("a"), track("b")]);
      expect(usePlayer.getState().queue.map((t) => t.id)).toEqual(["a", "b"]);
    });

    it("regenerates a valid shuffle order in shuffle mode", () => {
      usePlayer.setState({
        mode: "shuffle",
        shuffleOrder: [0],
        shuffleIdx: 0,
        queue: [track("a")],
      });
      usePlayer.getState().addToQueue([track("b")]);
      const s = usePlayer.getState();
      expect(isPermutation(s.shuffleOrder, 2)).toBe(true);
      expect(s.queue.map((t) => t.id)).toEqual(["a", "b"]);
    });
  });

  describe("removeFromQueue", () => {
    it("shifts queueIdx down when removing an earlier index", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.getState().playIndex(2);
      usePlayer.getState().removeFromQueue(0);
      const s = usePlayer.getState();
      expect(s.queue.map((t) => t.id)).toEqual(["b", "c"]);
      expect(s.queueIdx).toBe(1);
      expect(s.track?.id).toBe("c");
    });

    it("moves to next track when removing the current one", () => {
      usePlayer.getState().setQueue([track("a"), track("b"), track("c")]);
      usePlayer.getState().playIndex(1);
      usePlayer.getState().removeFromQueue(1);
      const s = usePlayer.getState();
      expect(s.track?.id).toBe("c");
      expect(s.queueIdx).toBe(1);
      expect(s.playEpoch).toBe(3);
    });

    it("resets state when removing the last track", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().removeFromQueue(0);
      const s = usePlayer.getState();
      expect(s.queue).toEqual([]);
      expect(s.track).toBeNull();
      expect(s.playing).toBe(false);
      expect(s.queueIdx).toBe(0);
    });

    it("ignores out-of-range index", () => {
      usePlayer.getState().setQueue([track("a")]);
      usePlayer.getState().removeFromQueue(3);
      expect(usePlayer.getState().queue.map((t) => t.id)).toEqual(["a"]);
    });

    it("rewrites shuffle order after removal", () => {
      usePlayer.setState({
        mode: "shuffle",
        shuffleOrder: [1, 2, 0],
        shuffleIdx: 0,
        queue: [track("a"), track("b"), track("c")],
        queueIdx: 1,
        track: track("b"),
      });
      usePlayer.getState().removeFromQueue(0);
      expect(usePlayer.getState().shuffleOrder).toEqual([0, 1]);
    });
  });

  describe("clearQueue", () => {
    it("resets queue and playback", () => {
      usePlayer.getState().setQueue([track("a"), track("b")]);
      usePlayer.getState().clearQueue();
      const s = usePlayer.getState();
      expect(s.queue).toEqual([]);
      expect(s.track).toBeNull();
      expect(s.playing).toBe(false);
      expect(s.currentPlaylistId).toBeNull();
      expect(s.shuffleOrder).toEqual([]);
    });
  });

  describe("lyrics", () => {
    it("fetchLyrics stores result", async () => {
      mockedData.lyrics.mockResolvedValue({
        lyrics: "[00:00.00]hi",
        format: "lrc",
        lyrics_offset: 0.5,
      });
      await usePlayer.getState().fetchLyrics("a");
      const s = usePlayer.getState();
      expect(s.lyrics).toBe("[00:00.00]hi");
      expect(s.lyricsFormat).toBe("lrc");
      expect(s.lyricsOffset).toBe(0.5);
    });

    it("fetchLyrics clears state on failure", async () => {
      mockedData.lyrics.mockRejectedValue(new Error("boom"));
      usePlayer.setState({ lyrics: "old", lyricsFormat: "lrc" });
      await usePlayer.getState().fetchLyrics("a");
      expect(usePlayer.getState().lyrics).toBe("");
      expect(usePlayer.getState().lyricsFormat).toBe("");
    });

    it("adjustLyricsOffset rounds to one decimal and persists", async () => {
      usePlayer.setState({ track: track("a"), lyricsOffset: 0.5 });
      await usePlayer.getState().adjustLyricsOffset(0.3);
      expect(usePlayer.getState().lyricsOffset).toBe(0.8);
      expect(mockedData.updateLyricsOffset).toHaveBeenCalledWith("a", 0.8);
    });

    it("adjustLyricsOffset is a no-op without a track", async () => {
      await usePlayer.getState().adjustLyricsOffset(0.5);
      expect(mockedData.updateLyricsOffset).not.toHaveBeenCalled();
    });

    it("adjustLyricsOffset rolls back on server failure", async () => {
      mockedData.updateLyricsOffset.mockRejectedValue(new Error("net"));
      usePlayer.setState({ track: track("a"), lyricsOffset: 0.5 });
      await usePlayer.getState().adjustLyricsOffset(0.3);
      expect(usePlayer.getState().lyricsOffset).toBe(0.5);
    });
  });

  describe("volume / position", () => {
    it("setVolume persists state", () => {
      usePlayer.getState().setVolume(0.3);
      expect(usePlayer.getState().volume).toBe(0.3);
      expect(mockedUser.updateSettings).toHaveBeenCalled();
    });

    it("setPosition updates position without persistence", () => {
      usePlayer.getState().setPosition(42);
      expect(usePlayer.getState().position).toBe(42);
      expect(mockedUser.updateSettings).not.toHaveBeenCalled();
    });
  });
});
