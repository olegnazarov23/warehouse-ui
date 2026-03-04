import { writable, derived } from "svelte/store";
import type { DiscoveryResult, SampleQuery } from "../types";

interface DiscoveryState {
  result: DiscoveryResult | null;
  sampleQueries: SampleQuery[];
  loading: boolean;
  progress: string;
}

export const discovery = writable<DiscoveryState>({
  result: null,
  sampleQueries: [],
  loading: false,
  progress: "",
});

export const hasDiscovery = derived(
  discovery,
  ($d) => $d.result !== null && $d.result.databases.length > 0
);

export function setDiscoveryLoading(loading: boolean, progress = "") {
  discovery.update((s) => ({ ...s, loading, progress }));
}

export function setDiscoveryResult(result: DiscoveryResult) {
  discovery.update((s) => ({ ...s, result, loading: false, progress: "" }));
}

export function setSampleQueries(queries: SampleQuery[]) {
  discovery.update((s) => ({ ...s, sampleQueries: queries }));
}
