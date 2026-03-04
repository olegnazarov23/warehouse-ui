import { writable, derived } from "svelte/store";
import type {
  ConnectionConfig,
  ConnectionStatus,
  DriverType,
  SavedConnection,
} from "../types";

export const connectionStatus = writable<ConnectionStatus>({
  connected: false,
  id: "",
  name: "",
  driver_type: "postgres" as DriverType,
  database: "",
});

export const savedConnections = writable<SavedConnection[]>([]);

export const isConnected = derived(
  connectionStatus,
  ($s) => $s.connected
);

export const currentDriverType = derived(
  connectionStatus,
  ($s) => $s.driver_type
);

export function emptyConfig(type: DriverType = "postgres"): ConnectionConfig {
  return {
    id: "",
    type,
    name: "",
    host: "",
    database: "",
    username: "",
    password: "",
    ssl_mode: "disable",
    options: {},
  };
}
