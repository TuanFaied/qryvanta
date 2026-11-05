/**
 * The only module that talks to the Go backend. Components and stores import from here,
 * never from ../bindings directly, so the transport can change without touching the UI.
 */
import { AppService } from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop";
import type { AppInfo, SaveConfigResult } from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import type { Config } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import { LogLevel, Theme } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";

export type { AppInfo, Config, SaveConfigResult };
export { LogLevel, Theme };

/** Normalised result of saving settings. */
export interface SaveResult {
  ok: boolean;
  config: Config;
  fieldErrors: Record<string, string>;
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
      const fieldErrors: Record<string, string> = {};
      for (const [k, v] of Object.entries(res.fieldErrors ?? {})) {
        if (v !== undefined) fieldErrors[k] = v;
      }
      return { ok: res.ok, config: res.config, fieldErrors };
    }),

  openLogFolder: (): Promise<void> => call("Open log folder", () => AppService.OpenLogFolder()),
};

export type Backend = typeof backend;
