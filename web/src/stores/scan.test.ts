import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: {
    libraries: {
      scan: vi.fn(),
      scanStatus: vi.fn(),
      list: vi.fn(),
    },
  },
}));

import { api } from "../api/client";
import { useScan } from "./scan";
import type { ScanStatus } from "../types";

const mocked = vi.mocked(api.libraries);

function status(over: Partial<ScanStatus>): ScanStatus {
  return {
    library_id: "lib-1",
    status: "running",
    total_files: 10,
    scanned: 0,
    new_tracks: 0,
    updated_tracks: 0,
    deleted_tracks: 0,
    errors: 0,
    ...over,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  for (const id of ["lib-1", "lib-2"]) useScan.getState().forget(id);
  useScan.setState({ scans: {}, revision: 0, failed: {} });
  mocked.scan.mockReset();
  mocked.scanStatus.mockReset();
  mocked.list.mockReset();
  mocked.list.mockResolvedValue([]);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("scan store", () => {
  it("tracks progress and bumps revision when a scan completes", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus
      .mockResolvedValueOnce(status({ scanned: 3 }))
      .mockResolvedValueOnce(status({ status: "idle", scanned: 10 }));

    await useScan.getState().start("lib-1", "missing");
    expect(useScan.getState().scans["lib-1"]).toBeTruthy();

    await vi.advanceTimersByTimeAsync(0);
    expect(useScan.getState().scans["lib-1"].scanned).toBe(3);

    await vi.advanceTimersByTimeAsync(1000);
    expect(useScan.getState().scans["lib-1"]).toBeUndefined();
    expect(useScan.getState().revision).toBe(1);
  });

  it("does not bump revision when polling gives up, and flags that library", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus.mockRejectedValue(new Error("down"));

    await useScan.getState().start("lib-1", "missing");
    await vi.advanceTimersByTimeAsync(1000 * 31);

    expect(useScan.getState().scans["lib-1"]).toBeUndefined();
    expect(useScan.getState().revision).toBe(0);
    expect(useScan.getState().failed["lib-1"]).toBe(true);
  });

  it("treats a terminal failed status as failure, not completion", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus.mockResolvedValueOnce(status({ status: "failed" }));

    await useScan.getState().start("lib-1", "missing");
    await vi.advanceTimersByTimeAsync(0);

    expect(useScan.getState().scans["lib-1"]).toBeUndefined();
    expect(useScan.getState().revision).toBe(0);
    expect(useScan.getState().failed["lib-1"]).toBe(true);
  });

  it("clears the failure flag and refreshes when the backend recovers", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus.mockRejectedValue(new Error("down"));
    await useScan.getState().start("lib-1", "missing");
    await vi.advanceTimersByTimeAsync(1000 * 31);
    expect(useScan.getState().failed["lib-1"]).toBe(true);

    mocked.scanStatus.mockReset();
    mocked.scanStatus.mockResolvedValue(status({ status: "idle" }));
    await vi.advanceTimersByTimeAsync(10000);

    expect(useScan.getState().failed["lib-1"]).toBeUndefined();
    expect(useScan.getState().revision).toBe(1);
  });

  it("recover clears the failure flag for a library", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus.mockRejectedValue(new Error("down"));
    await useScan.getState().start("lib-1", "missing");
    await vi.advanceTimersByTimeAsync(1000 * 31);
    expect(useScan.getState().failed["lib-1"]).toBe(true);

    useScan.getState().recover("lib-1", status({ status: "idle" }));
    expect(useScan.getState().failed["lib-1"]).toBeUndefined();
  });

  it("track stores the provided progress and defers the next poll", async () => {
    mocked.scanStatus.mockResolvedValue(status({ scanned: 5 }));

    useScan.getState().track("lib-1", { scanned: 5, total: 10, errors: 0 });
    expect(useScan.getState().scans["lib-1"].scanned).toBe(5);
    expect(mocked.scanStatus).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1000);
    expect(mocked.scanStatus).toHaveBeenCalledTimes(1);
  });

  it("forget stops tracking without bumping revision", async () => {
    mocked.scan.mockResolvedValue(undefined);
    mocked.scanStatus.mockResolvedValue(status({ scanned: 1 }));

    await useScan.getState().start("lib-2", "missing");
    await vi.advanceTimersByTimeAsync(0);

    useScan.getState().forget("lib-2");
    expect(useScan.getState().scans["lib-2"]).toBeUndefined();

    await vi.advanceTimersByTimeAsync(5000);
    expect(useScan.getState().revision).toBe(0);
  });

  it("surfaces a start failure and clears optimistic progress", async () => {
    mocked.scan.mockRejectedValue(new Error("scan already running for library lib-1"));

    await expect(useScan.getState().start("lib-1", "missing")).rejects.toThrow("already running");
    expect(useScan.getState().scans["lib-1"]).toBeUndefined();
  });
});
