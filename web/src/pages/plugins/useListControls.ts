import { useCallback, useMemo, useState } from "react";

// useListControls centralizes the search + filter + sort state and the derived
// item list shared by the plugin tabs. `matches` and `sorters` are pure and
// should be module-level constants so the derived list stays memoized and the
// sort keys used by `sortOptions` and the dispatch stay in one place.
export function useListControls<T>(
  items: T[],
  initialFilters: Record<string, string[]>,
  matches: (item: T, query: string, filters: Record<string, string[]>) => boolean,
  sorters: Record<string, (a: T, b: T) => number>,
) {
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Record<string, string[]>>(initialFilters);
  const [sortBy, setSortBy] = useState("");

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    const list = items.filter((item) => matches(item, query, filters));
    const sorter = sortBy ? sorters[sortBy] : undefined;
    return sorter ? [...list].sort(sorter) : list;
  }, [items, search, filters, sortBy, matches, sorters]);

  const toggleFilter = useCallback((group: string, value: string) => {
    setFilters((prev) => {
      const cur = prev[group] || [];
      const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
      return { ...prev, [group]: next };
    });
  }, []);

  return { search, setSearch, filters, setFilters, sortBy, setSortBy, filtered, toggleFilter };
}
