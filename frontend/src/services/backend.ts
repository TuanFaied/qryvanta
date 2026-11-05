/**
 * The only module that talks to the Go backend. Components and stores import from here,
 * never from ../bindings directly, so the transport can change without touching the UI.
 */
import { AppService, ConnectionService } from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop";
import type {
  AppInfo,
  ConnectionDTO,
  ConnectionError,
  ConnectionInput,
  SaveConfigResult,
} from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import { ErrorCode, PasswordAction } from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import type { Config } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import { LogLevel, Theme } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import type { Profile, ServerInfo } from "../../bindings/github.com/TuanFaied/qryvanta/internal/connection/models";
import { AuthMethod, EncryptMode } from "../../bindings/github.com/TuanFaied/qryvanta/internal/connection/models";

export type { AppInfo, Config, ConnectionDTO, ConnectionError, ConnectionInput, Profile, SaveConfigResult, ServerInfo };
export { AuthMethod, EncryptMode, ErrorCode, LogLevel, PasswordAction, Theme };

/** Normalised result of saving settings. */
export interface SaveResult {
  ok: boolean;
  config: Config;
  fieldErrors: Record<string, string>;
}

export interface SaveConnectionResult {
  ok: boolean;
  connection: ConnectionDTO | null;
  fieldErrors: Record<string, string>;
}

export interface TestConnectionResult {
  ok: boolean;
  server: ServerInfo;
  latencyMs: number;
  error: ConnectionError | null;
  fieldErrors: Record<string, string>;
}

export interface ConnectResult {
  ok: boolean;
  server: ServerInfo;
  error: ConnectionError | null;
}

function toErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "Unexpected error from the Qryvanta backend.";
}

/** Error raised when a backend call fails. The message is safe to show to the user. */
export class BackendError extends Error {
  constructor(operation: string, cause: unknown) {
    super(`${operation}: ${toErrorMessage(cause)}`);
    this.name = "BackendError";
  }
}

async function call<T>(operation: string, fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (err) {
    throw new BackendError(operation, err);
  }
}

function cleanErrors(errors: { [k: string]: string | undefined } | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(errors ?? {})) {
    if (v !== undefined) out[k] = v;
  }
  return out;
}

/** A backend call that can be cancelled; cancelling also cancels the Go context. */
interface Cancellable<T> extends Promise<T> {
  cancel(): void;
}

/** Runs a cancellable binding, cancelling it when signal aborts. */
function withSignal<T>(promise: Cancellable<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  if (signal.aborted) promise.cancel();
  else signal.addEventListener("abort", () => promise.cancel(), { once: true });
  return promise;
}

export const backend = {
  getAppInfo: (): Promise<AppInfo> =>
    call("Load application info", async () => {
      const info = await AppService.Info();
      return { ...info, startupWarnings: info.startupWarnings ?? [] };
    }),

  getConfig: (): Promise<Config> => call("Load settings", () => AppService.GetConfig()),

  getDefaultConfig: (): Promise<Config> => call("Load default settings", () => AppService.DefaultConfig()),

  saveConfig: (config: Config): Promise<SaveResult> =>
    call("Save settings", async () => {
      const res = await AppService.UpdateConfig(config);
      return { ok: res.ok, config: res.config, fieldErrors: cleanErrors(res.fieldErrors) };
    }),

  openLogFolder: (): Promise<void> => call("Open log folder", () => AppService.OpenLogFolder()),

  listConnections: (): Promise<ConnectionDTO[]> =>
    call("Load connections", async () => (await ConnectionService.List()) ?? []),

  saveConnection: (input: ConnectionInput): Promise<SaveConnectionResult> =>
    call("Save connection", async () => {
      const res = await ConnectionService.Save(input);
      return { ok: res.ok, connection: res.connection, fieldErrors: cleanErrors(res.fieldErrors) };
    }),

  deleteConnection: (id: string): Promise<void> => call("Delete connection", () => ConnectionService.Delete(id)),

  testConnection: (input: ConnectionInput, signal?: AbortSignal): Promise<TestConnectionResult> =>
    call("Test connection", async () => {
      const res = await withSignal(ConnectionService.Test(input), signal);
      return { ...res, fieldErrors: cleanErrors(res.fieldErrors) };
    }),

  connect: (id: string, signal?: AbortSignal): Promise<ConnectResult> =>
    call("Connect", () => withSignal(ConnectionService.Connect(id), signal)),

  disconnect: (id: string): Promise<void> => call("Disconnect", () => ConnectionService.Disconnect(id)),
};

export type Backend = typeof backend;
