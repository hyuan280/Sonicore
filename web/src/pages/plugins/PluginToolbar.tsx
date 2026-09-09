import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Filter, Search, ArrowUpDown, X } from "lucide-react";
import {
  DropdownMenu,
  MenuCheckbox,
  MenuDivider,
  MenuLabel,
  MenuRadio,
} from "../../components/ui/menu";
import { cn } from "../../lib/utils";

export interface PluginFilterGroup {
  key: string;
  label: string;
  options: string[];
}

export interface PluginSortOption {
  key: string;
  label: string;
}

interface PluginToolbarProps {
  filterGroups: PluginFilterGroup[];
  filters: Record<string, string[]>;
  onFilterToggle: (group: string, value: string) => void;
  onFiltersClear: () => void;
  searchValue: string;
  onSearch: (value: string) => void;
  searchPlaceholder: string;
  sortOptions: PluginSortOption[];
  sortBy: string;
  onSortBy: (key: string) => void;
}

function toolbarButton(active: boolean) {
  return cn(
    "relative p-2 rounded-lg cursor-pointer transition-colors",
    active ? "bg-zinc-800 text-green-500" : "text-zinc-400 hover:text-white hover:bg-zinc-800",
  );
}

export default function PluginToolbar({
  filterGroups,
  filters,
  onFilterToggle,
  onFiltersClear,
  searchValue,
  onSearch,
  searchPlaceholder,
  sortOptions,
  sortBy,
  onSortBy,
}: PluginToolbarProps) {
  const { t } = useTranslation();
  const [searchOpen, setSearchOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (searchOpen) searchRef.current?.focus();
  }, [searchOpen]);

  const activeFilterCount = Object.values(filters).reduce((n, v) => n + v.length, 0);
  const filterActive = activeFilterCount > 0;
  const sortActive = sortBy !== "";

  return (
    <div className="flex items-center gap-2 shrink-0">
      {searchOpen ? (
        <div className="flex items-center gap-1">
          <input
            ref={searchRef}
            type="text"
            value={searchValue}
            onChange={(e) => onSearch(e.target.value)}
            placeholder={searchPlaceholder}
            aria-label={t("plugins.search")}
            className="w-44 px-3 py-1.5 text-sm bg-zinc-800 text-zinc-300 border border-zinc-700 rounded-lg outline-none focus:border-green-500 placeholder-zinc-500"
          />
          <button
            type="button"
            onClick={() => {
              setSearchOpen(false);
              onSearch("");
            }}
            aria-label={t("plugins.closeSearch")}
            className="p-1.5 rounded-lg text-zinc-400 hover:text-white hover:bg-zinc-800 cursor-pointer"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setSearchOpen(true)}
          aria-label={t("plugins.search")}
          className={toolbarButton(searchValue !== "")}
        >
          <Search className="w-4 h-4" />
        </button>
      )}

      <DropdownMenu
        align="right"
        trigger={(toggle, _open, ariaProps) => (
          <button
            type="button"
            onClick={toggle}
            {...ariaProps}
            aria-label={t("plugins.filter")}
            className={toolbarButton(filterActive)}
          >
            <Filter className="w-4 h-4" />
            {activeFilterCount > 0 && (
              <span className="absolute -top-1 -right-1 w-4 h-4 rounded-full bg-green-600 text-white text-[10px] flex items-center justify-center">
                {activeFilterCount}
              </span>
            )}
          </button>
        )}
      >
        {filterGroups.map((group, i) => (
          <div key={group.key}>
            {i > 0 && <MenuDivider />}
            <MenuLabel>{group.label}</MenuLabel>
            {group.options.map((opt) => (
              <MenuCheckbox
                key={opt}
                label={opt}
                checked={(filters[group.key] || []).includes(opt)}
                onToggle={() => onFilterToggle(group.key, opt)}
              />
            ))}
          </div>
        ))}
        <MenuDivider />
        <MenuLabel>
          <button
            type="button"
            onClick={onFiltersClear}
            className="text-xs text-zinc-400 hover:text-white cursor-pointer"
          >
            {t("plugins.clearFilters")}
          </button>
        </MenuLabel>
      </DropdownMenu>

      <DropdownMenu
        align="right"
        trigger={(toggle, _open, ariaProps) => (
          <button
            type="button"
            onClick={toggle}
            {...ariaProps}
            aria-label={t("plugins.sort")}
            className={toolbarButton(sortActive)}
          >
            <ArrowUpDown className="w-4 h-4" />
          </button>
        )}
      >
        {sortOptions.map((opt) => (
          <MenuRadio
            key={opt.key}
            label={opt.label}
            checked={sortBy === opt.key}
            onSelect={() => onSortBy(opt.key)}
          />
        ))}
        <MenuDivider />
        <MenuLabel>
          <button
            type="button"
            onClick={() => onSortBy("")}
            className="text-xs text-zinc-400 hover:text-white cursor-pointer"
          >
            {t("plugins.clearSort")}
          </button>
        </MenuLabel>
      </DropdownMenu>
    </div>
  );
}
