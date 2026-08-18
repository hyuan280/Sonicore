import { describe, expect, it, beforeEach, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: { libraries: { list: vi.fn() } },
}));

import { api } from "../api/client";
import { useLibrary } from "./library";

const mockedList = vi.mocked(api.libraries.list);

describe("useLibrary", () => {
  beforeEach(() => {
    useLibrary.setState({ libraries: [], loading: false });
    mockedList.mockReset();
  });

  it("loads libraries into state", async () => {
    const libs = [{ id: "l1", name: "Music", path: "/music" }];
    mockedList.mockResolvedValue(libs);
    await useLibrary.getState().load();
    expect(useLibrary.getState().libraries).toEqual(libs);
    expect(useLibrary.getState().loading).toBe(false);
  });

  it("sets loading while in flight", async () => {
    let resolve!: (v: unknown[]) => void;
    mockedList.mockReturnValue(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const p = useLibrary.getState().load();
    expect(useLibrary.getState().loading).toBe(true);
    resolve([{ id: "l1" }]);
    await p;
    expect(useLibrary.getState().loading).toBe(false);
  });
});
