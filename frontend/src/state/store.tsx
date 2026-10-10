import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  type ReactNode,
} from "react";
import { api, type Backend, type Instance, type Job, type Model, type ApiKey, type Status } from "../api/client";

export interface State {
  status: Status | null;
  backends: Backend[];
  models: Model[];
  instances: Instance[];
  jobs: Job[];
  keys: ApiKey[];
  loading: boolean;
  error: string | null;
}

type Action =
  | { type: "loading" }
  | { type: "status"; status: Status }
  | { type: "backends"; backends: Backend[] }
  | { type: "models"; models: Model[] }
  | { type: "instances"; instances: Instance[] }
  | { type: "jobs"; jobs: Job[] }
  | { type: "keys"; keys: ApiKey[] }
  | { type: "error"; error: string };

const initial: State = {
  status: null,
  backends: [],
  models: [],
  instances: [],
  jobs: [],
  keys: [],
  loading: false,
  error: null,
};

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "loading":
      return { ...state, loading: true };
    case "status":
      return { ...state, status: action.status };
    case "backends":
      return { ...state, backends: action.backends };
    case "models":
      return { ...state, models: action.models };
    case "instances":
      return { ...state, instances: action.instances };
    case "jobs":
      return { ...state, jobs: action.jobs };
    case "keys":
      return { ...state, keys: action.keys };
    case "error":
      return { ...state, error: action.error };
    default:
      return state;
  }
}

interface StoreValue extends State {
  refresh: () => Promise<void>;
  refreshOne: (kind: "backends" | "models" | "instances" | "keys") => Promise<void>;
}

const StoreCtx = createContext<StoreValue | null>(null);

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initial);

  const refresh = useCallback(async () => {
    dispatch({ type: "loading" });
    try {
      const [status, backends, models, instances, jobs, keys] = await Promise.all([
        api.status(),
        api.backends(),
        api.models(),
        api.instances(),
        api.jobs(),
        api.keys(),
      ]);
      dispatch({ type: "status", status });
      dispatch({ type: "backends", backends });
      dispatch({ type: "models", models });
      dispatch({ type: "instances", instances });
      dispatch({ type: "jobs", jobs });
      dispatch({ type: "keys", keys });
      dispatch({ type: "error", error: "" });
    } catch (e) {
      dispatch({ type: "error", error: e instanceof Error ? e.message : String(e) });
    }
  }, []);

  const refreshOne = useCallback(
    async (kind: "backends" | "models" | "instances" | "keys") => {
      try {
        if (kind === "backends") dispatch({ type: "backends", backends: await api.backends() });
        else if (kind === "models") dispatch({ type: "models", models: await api.models() });
        else if (kind === "instances") dispatch({ type: "instances", instances: await api.instances() });
        else dispatch({ type: "keys", keys: await api.keys() });
      } catch {
        /* keep last good value */
      }
    },
    []
  );

  // Initial load + periodic refresh.
  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 10000);
    return () => clearInterval(t);
  }, [refresh]);

  // Live job progress via SSE: refresh jobs (and models/backends/instances,
  // which change as jobs complete) on any job event.
  useEffect(() => {
    let es: EventSource | null = null;
    let retry: ReturnType<typeof setTimeout>;
    const connect = () => {
      es = new EventSource("/api/v1/jobs/events");
      const onJob = () => {
        api.jobs().then((j) => dispatch({ type: "jobs", jobs: j })).catch(() => {});
        refreshOne("models");
        refreshOne("backends");
        refreshOne("instances");
      };
      es.addEventListener("snapshot", onJob);
      es.addEventListener("progress", onJob);
      es.addEventListener("done", onJob);
      es.addEventListener("error", onJob);
      es.onerror = () => {
        es?.close();
        retry = setTimeout(connect, 3000);
      };
    };
    connect();
    return () => {
      es?.close();
      clearTimeout(retry);
    };
  }, [refreshOne]);

  const value = useMemo<StoreValue>(
    () => ({ ...state, refresh, refreshOne }),
    [state, refresh, refreshOne]
  );

  return <StoreCtx.Provider value={value}>{children}</StoreCtx.Provider>;
}

export function useStore(): StoreValue {
  const ctx = useContext(StoreCtx);
  if (!ctx) throw new Error("useStore must be used within StoreProvider");
  return ctx;
}
