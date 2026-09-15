import { useEffect, useState } from "react";
import { api } from "../api/client";
import { buildRepoOfficialMap } from "../pages/plugins/PluginCard";

// useRepoOfficialMap fetches the configured plugin repositories and returns a
// repo-name → official map, used to derive the verified badge. A failure
// degrades to an empty map (plugins fall back to "unverified") rather than
// surfacing an error or blocking the plugin list.
export function useRepoOfficialMap(): Map<string, boolean> {
  const [repoOfficial, setRepoOfficial] = useState<Map<string, boolean>>(new Map());

  useEffect(() => {
    let active = true;
    api.plugins
      .repos()
      .then((repos) => {
        if (active) setRepoOfficial(buildRepoOfficialMap(repos));
      })
      .catch(() => {
        if (active) setRepoOfficial(new Map());
      });
    return () => {
      active = false;
    };
  }, []);

  return repoOfficial;
}
