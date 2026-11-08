/**
 * The only module that talks to the Go backend. Components and stores import from here,
 * never from ../bindings directly, so the transport can change without touching the UI.
 */
import { Events } from "@wailsio/runtime";
import { QryvantaCloseRequested, QryvantaExecution } from "../../bindings/events";
import {
  AppService,
  ConnectionService,
  FileService,
  QueryService,
  SchemaService,
  WindowService,
} from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop";
import type { Column as ResultColumn, Message, Summary, Value } from "../../bindings/github.com/TuanFaied/qryvanta/internal/executor/models";
import { Outcome } from "../../bindings/github.com/TuanFaied/qryvanta/internal/executor/models";
import type {
  ExecutionEvent,
  FileDocument,
  MetadataError,
  OpenFileResult,
  SaveFileResult,
} from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import { Encoding } from "../../bindings/github.com/TuanFaied/qryvanta/internal/sqlfile/models";
import type { Status as ParserStatus } from "../../bindings/github.com/TuanFaied/qryvanta/internal/parser/models";
import type { Column, Database, $Object as SchemaObject } from "../../bindings/github.com/TuanFaied/qryvanta/internal/schema/models";
import { ObjectKind } from "../../bindings/github.com/TuanFaied/qryvanta/internal/schema/models";
import type {
  AppInfo,
  ConnectionDTO,
  ConnectionError,
  ConnectionInput,
  SaveConfigResult,
} from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import { ErrorCode, PasswordAction } from "../../bindings/github.com/TuanFaied/qryvanta/internal/desktop/models";
import type { Config } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import { KeywordCase, LogLevel, Theme } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import type { Formatting } from "../../bindings/github.com/TuanFaied/qryvanta/internal/config/models";
import type { Profile, ServerInfo, WriteAccess } from "../../bindings/github.com/TuanFaied/qryvanta/internal/connection/models";
import { AccessLevel, AuthMethod, EncryptMode } from "../../bindings/github.com/TuanFaied/qryvanta/internal/connection/models";

export type { AppInfo, Config, ConnectionDTO, ConnectionError, ConnectionInput, Profile, SaveConfigResult, ServerInfo };
export type { Column, Database, MetadataError, SchemaObject, WriteAccess };
export type { FileDocument, Formatting, OpenFileResult, SaveFileResult };
export type { ExecutionEvent, Message, ResultColumn, Summary, Value };
export type { ParserStatus };
export { Outcome };
export { AccessLevel, AuthMethod, EncryptMode, Encoding, ErrorCode, KeywordCase, LogLevel, ObjectKind, PasswordAction, Theme };

/** A metadata call's result: data, or the error to show on the tree node. */
export interface MetadataResult<T> {
  data: T;
  error: MetadataError | null;
}

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
  access: WriteAccess | null;
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

  /** Starts the T-SQL parser if needed and reports whether it works. */
  parserStatus: (): Promise<ParserStatus> => call("Check the parser", () => AppService.ParserStatus()),

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

  checkWriteAccess: (id: string, database: string): Promise<WriteAccess> =>
    call("Check write access", async () => {
      const res = await ConnectionService.CheckWriteAccess(id, database);
      return res
        ? { ...res, reasons: res.reasons ?? [] }
        : { database, level: AccessLevel.AccessUnknown, reasons: [], error: "No result" };
    }),

  listDatabases: (connectionId: string): Promise<MetadataResult<Database[]>> =>
    call("Load databases", async () => {
      const res = await SchemaService.Databases(connectionId);
      return { data: res.databases ?? [], error: res.error };
    }),

  listObjects: (connectionId: string, database: string): Promise<MetadataResult<SchemaObject[]>> =>
    call("Load objects", async () => {
      const res = await SchemaService.Objects(connectionId, database);
      return { data: res.objects ?? [], error: res.error };
    }),

  listColumns: (connectionId: string, database: string, schema: string, object: string): Promise<MetadataResult<Column[]>> =>
    call("Load columns", async () => {
      const res = await SchemaService.Columns(connectionId, database, schema, object);
      return { data: res.columns ?? [], error: res.error };
    }),

  /** Shows the native open dialog and reads the chosen .sql file. */
  openFile: (): Promise<OpenFileResult> => call("Open file", () => FileService.Open()),

  /** Saves to a file that was opened or chosen with Save As earlier. An empty encoding means UTF-8 with BOM. */
  saveFile: (path: string, content: string, encoding: Encoding | ""): Promise<SaveFileResult> =>
    call("Save file", () => FileService.Save(path, content, encoding as Encoding)),

  /** Shows the native save dialog and writes the file. */
  saveFileAs: (suggestedName: string, content: string, encoding: Encoding | ""): Promise<SaveFileResult> =>
    call("Save file", () => FileService.SaveAs(suggestedName, content, encoding as Encoding)),

  /** Tells the backend whether closing the window would lose work. */
  setUnsavedChanges: (unsaved: boolean): Promise<void> =>
    call("Track unsaved changes", () => WindowService.SetUnsavedChanges(unsaved)),

  /** Closes the window without asking about unsaved changes again. */
  closeWindow: (): Promise<void> => call("Close window", () => WindowService.Close()),

  /**
   * Runs sql on a connection. Results arrive as execution events tagged with runId (see
   * onExecutionEvent); the promise resolves with the summary when the run has ended.
   */
  execute: (runId: string, connectionId: string, database: string, sql: string): Promise<Summary> =>
    call("Execute", () => QueryService.Execute(runId, connectionId, database, sql)),

  /** Cancels a run on the server. Its summary still arrives, with outcome "cancelled". */
  cancelRun: (runId: string): Promise<boolean> => call("Cancel execution", () => QueryService.Cancel(runId)),

  /** Subscribes to result streaming. Returns an unsubscribe function. */
  onExecutionEvent: (handler: (event: ExecutionEvent) => void): (() => void) =>
    Events.On(QryvantaExecution, (e) => handler(e.data)),

  /** Calls handler when the user tries to close the window with unsaved changes. */
  onCloseRequested: (handler: () => void): (() => void) => Events.On(QryvantaCloseRequested, () => handler()),
};

export type Backend = typeof backend;
