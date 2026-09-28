import { create } from "zustand";
import { api } from "../api/client";
import { useLibrary } from "./library";
import type { ScanStatus } from "../types";

export interface ScanProgress {
  scanned: number;
  total: number;
  errors?: number;
}

interface ScanState {
  // Active scans keyed by library id. Progress is tracked here (module scope)
  // rather than in a component so it survives navigating away from the library
  // page while a scan runs in the background.
  scans: Record<string, ScanProgress>;
  // Bumped whenever a scan genuinely finishes, so data views (songs, albums,
  // library list, library track modal) can refetch the freshly scanned content.
  revision: number;
  // failed marks libraries whose polling gave up after repeated status
  // failures, so the UI can tell "scan finished" apart from "we lost track of
  // the scan" and point at the affected library.
  failed: Record<string, boolean>;
  // track resumes polling a scan already running (e.g. after a remount). The
  // progress fetched by the caller is stored immediately so the UI never shows
  // an empty row, and the next poll is deferred one tick to avoid a duplicate
  // request.
  track: (libraryId: string, progress?: ScanProgress) => void;
  // start begins a scan and polls it to completion.
  start: (libraryId: string, mode?: string) => Promise<void>;
  // forget stops tracking a library without bumping revision (deleted library).
  forget: (libraryId: string) => void;
  // recover reconciles a freshly-read status for a library: it clears any
  // polling-failure flag and, when tracking had been given up, resumes progress
  // (running) or refetches the data views (a scan that ended during the outage).
  recover: (libraryId: string, status: ScanStatus) => void;
}

// Give up polling after this many consecutive status failures (~30s).
const MAX_POLL_RETRIES = 30;
// After giving up, keep re-checking occasionally so a recovered backend clears
// the failure flag and any data missed during the outage is refetched.
const RECHECK_INTERVAL = 10_000;

function toProgress(status: ScanStatus): ScanProgress {
  return { scanned: status.scanned, total: status.total_files, errors: status.errors };
}

export const useScan = create<ScanState>((set) => {
  // Timers/flags live outside the store state: they are not rendered and must
  // keep running after the component that started the scan unmounts.
  const timers: Record<string, ReturnType<typeof setTimeout>> = {};
  const recheckTimers: Record<string, ReturnType<typeof setTimeout>> = {};
  const active: Record<string, boolean> = {};
  const retries: Record<string, number> = {};
  // gen is a per-library generation token compared by poll/recheck closures.
  // The counter is monotonic and never reset, so a token is never reused even
  // after stop() deletes the entry: a stale in-flight response can therefore
  // never pass the check and write progress or start a second loop.
  const gen: Record<string, number> = {};
  let genCounter = 0;

  function bumpGen(libraryId: string): number {
    gen[libraryId] = ++genCounter;
    return gen[libraryId];
  }

  function stop(libraryId: string) {
    clearTimeout(timers[libraryId]);
    delete timers[libraryId];
    delete retries[libraryId];
    delete active[libraryId];
    delete gen[libraryId];
  }

  function clearRecheck(libraryId: string) {
    clearTimeout(recheckTimers[libraryId]);
    delete recheckTimers[libraryId];
  }

  function scheduleRecheck(libraryId: string) {
    clearRecheck(libraryId);
    recheckTimers[libraryId] = setTimeout(() => void recheck(libraryId), RECHECK_INTERVAL);
  }

  // recheck runs only while a library is flagged failed: a successful read
  // hands off to recover, which clears the flag and resumes/refreshes.
  async function recheck(libraryId: string) {
    if (!useScan.getState().failed[libraryId]) return;
    let status: ScanStatus;
    try {
      status = (await api.libraries.scanStatus(libraryId)) as ScanStatus;
    } catch {
      scheduleRecheck(libraryId);
      return;
    }
    // The library may have been forgotten (deleted) or healed while the request
    // was in flight; never write state or start a loop for it in that case.
    if (!useScan.getState().failed[libraryId]) return;
    if (status.status === "failed" || status.status === "cancelled") {
      // Terminal failure: keep the flag, nothing new to refresh.
      return;
    }
    useScan.getState().recover(libraryId, status);
  }

  // beginTracking installs a library's polling state (shared by start/track so
  // they cannot drift apart) and returns the generation token for this run.
  function beginTracking(libraryId: string, progress: ScanProgress): number {
    const myGen = bumpGen(libraryId);
    active[libraryId] = true;
    retries[libraryId] = 0;
    clearTimeout(timers[libraryId]);
    clearRecheck(libraryId);
    set((s) => {
      const failed = { ...s.failed };
      delete failed[libraryId];
      return { scans: { ...s.scans, [libraryId]: progress }, failed };
    });
    return myGen;
  }

  function finish(libraryId: string, myGen: number, ok: boolean) {
    if (gen[libraryId] !== myGen) return;
    stop(libraryId);
    set((s) => {
      const scans = { ...s.scans };
      delete scans[libraryId];
      if (ok) {
        const failed = { ...s.failed };
        delete failed[libraryId];
        return { scans, failed, revision: s.revision + 1 };
      }
      // Polling gave up: surface it for THIS library and do not pretend new
      // content arrived (no revision bump).
      return { scans, failed: { ...s.failed, [libraryId]: true } };
    });
    if (ok) {
      void useLibrary.getState().load();
    } else {
      scheduleRecheck(libraryId);
    }
  }

  async function poll(libraryId: string, myGen: number) {
    if (!active[libraryId] || gen[libraryId] !== myGen) return;
    try {
      const status = (await api.libraries.scanStatus(libraryId)) as ScanStatus;
      if (gen[libraryId] !== myGen) return;
      retries[libraryId] = 0;
      if (status.status === "running") {
        set((s) => ({ scans: { ...s.scans, [libraryId]: toProgress(status) } }));
        timers[libraryId] = setTimeout(() => void poll(libraryId, myGen), 1000);
      } else if (status.status === "failed" || status.status === "cancelled") {
        // The backend reports a terminal failure: do not treat it as success.
        finish(libraryId, myGen, false);
      } else {
        // "completed" (or "idle" once the entry has been cleared) is success.
        finish(libraryId, myGen, true);
      }
    } catch {
      if (gen[libraryId] !== myGen) return;
      retries[libraryId] = (retries[libraryId] || 0) + 1;
      if (retries[libraryId] >= MAX_POLL_RETRIES) {
        finish(libraryId, myGen, false);
        return;
      }
      timers[libraryId] = setTimeout(() => void poll(libraryId, myGen), 1000);
    }
  }

  return {
    scans: {},
    revision: 0,
    failed: {},

    track: (libraryId, progress) => {
      if (active[libraryId]) return;
      const myGen = beginTracking(libraryId, progress ?? { scanned: 0, total: 0 });
      if (progress) {
        // The caller just fetched this status, so wait a beat before the next
        // check instead of duplicating the request immediately.
        timers[libraryId] = setTimeout(() => void poll(libraryId, myGen), 1000);
      } else {
        void poll(libraryId, myGen);
      }
    },

    start: async (libraryId, mode) => {
      // A failed marker (e.g. a previous give-up) must survive a start that the
      // backend rejects (409 "already running"), so remember it and restore.
      const prevFailed = useScan.getState().failed[libraryId] === true;
      // Invalidate any loop already polling this library (e.g. resumed by
      // track) before installing the new one, so only one poll loop survives.
      const myGen = beginTracking(libraryId, { scanned: 0, total: 0 });
      try {
        await api.libraries.scan(libraryId, mode);
      } catch (err) {
        if (gen[libraryId] === myGen) {
          stop(libraryId);
          set((s) => {
            const scans = { ...s.scans };
            delete scans[libraryId];
            const failed = { ...s.failed };
            if (prevFailed) failed[libraryId] = true;
            else delete failed[libraryId];
            return { scans, failed };
          });
          if (prevFailed) scheduleRecheck(libraryId);
        }
        throw err;
      }
      void poll(libraryId, myGen);
    },

    forget: (libraryId) => {
      // Invalidate first so an in-flight poll response cannot resurrect it.
      bumpGen(libraryId);
      stop(libraryId);
      clearRecheck(libraryId);
      set((s) => {
        const scans = { ...s.scans };
        const failed = { ...s.failed };
        delete scans[libraryId];
        delete failed[libraryId];
        return { scans, failed };
      });
    },

    recover: (libraryId, status) => {
      const wasFailed = useScan.getState().failed[libraryId] === true;
      clearRecheck(libraryId);
      set((s) => {
        if (!s.failed[libraryId]) return s;
        const failed = { ...s.failed };
        delete failed[libraryId];
        return { failed };
      });
      if (status.status === "running") {
        useScan.getState().track(libraryId, toProgress(status));
      } else if (wasFailed && status.status !== "failed" && status.status !== "cancelled") {
        // The scan ended while we had lost track of it: refetch data views once.
        set((s) => ({ revision: s.revision + 1 }));
        void useLibrary.getState().load();
      }
    },
  };
});
