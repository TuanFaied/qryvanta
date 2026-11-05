import {
  AuthMethod,
  EncryptMode,
  ErrorCode,
  PasswordAction,
  type ConnectionDTO,
  type ConnectionError,
  type ConnectionInput,
  type Profile,
} from "@/services/backend";

/** Editable state of the connection dialog. Numbers are kept as text while typing. */
export interface ConnectionForm {
  id: string;
  name: string;
  server: string;
  port: string;
  database: string;
  auth: AuthMethod;
  username: string;
  /** New password typed in the dialog. Empty means "keep what is stored". */
  password: string;
  /** Remove the stored password on save. */
  clearPassword: boolean;
  /** Whether a password is already stored (edit mode only). */
  hasStoredPassword: boolean;
  encrypt: EncryptMode;
  trustServerCertificate: boolean;
  readOnlyIntent: boolean;
  timeout: string;
  createdAt: string;
  updatedAt: string;
}

export function emptyForm(): ConnectionForm {
  return {
    id: "",
    name: "",
    server: "",
    port: "",
    database: "",
    auth: AuthMethod.AuthSQL,
    username: "",
    password: "",
    clearPassword: false,
    hasStoredPassword: false,
    encrypt: EncryptMode.EncryptMandatory,
    trustServerCertificate: false,
    readOnlyIntent: false,
    timeout: "",
    createdAt: "",
    updatedAt: "",
  };
}

export function formFromConnection(c: ConnectionDTO): ConnectionForm {
  const p = c.profile;
  return {
    id: p.id,
    name: p.name,
    server: p.server,
    port: p.port > 0 ? String(p.port) : "",
    database: p.database,
    auth: p.auth || AuthMethod.AuthSQL,
    username: p.username,
    password: "",
    clearPassword: false,
    hasStoredPassword: c.hasPassword,
    encrypt: p.encrypt || EncryptMode.EncryptMandatory,
    trustServerCertificate: p.trustServerCertificate,
    readOnlyIntent: p.readOnlyIntent,
    timeout: p.connectTimeoutSeconds > 0 ? String(p.connectTimeoutSeconds) : "",
    createdAt: p.createdAt,
    updatedAt: p.updatedAt,
  };
}

/** Parses an optional whole number; empty text means 0 (use the default). */
function parseOptionalInt(text: string): number | null {
  const t = text.trim();
  if (t === "") return 0;
  return /^\d+$/.test(t) ? Number.parseInt(t, 10) : null;
}

/**
 * Builds the backend input. Only number parsing is checked here; every other rule is
 * validated by the backend, which owns them.
 */
export function toInput(form: ConnectionForm): { input: ConnectionInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const port = parseOptionalInt(form.port);
  if (port === null) errors.port = "must be a whole number";
  const timeout = parseOptionalInt(form.timeout);
  if (timeout === null) errors.connectTimeoutSeconds = "must be a whole number of seconds";

  const sql = form.auth === AuthMethod.AuthSQL;
  let passwordAction = PasswordAction.PasswordKeep;
  if (sql && form.password !== "") passwordAction = PasswordAction.PasswordSet;
  else if (sql && form.clearPassword) passwordAction = PasswordAction.PasswordClear;

  const profile: Profile = {
    id: form.id,
    name: form.name,
    server: form.server,
    port: port ?? 0,
    database: form.database,
    auth: form.auth,
    username: sql ? form.username : "",
    encrypt: form.encrypt,
    trustServerCertificate: form.encrypt === EncryptMode.EncryptStrict ? false : form.trustServerCertificate,
    connectTimeoutSeconds: timeout ?? 0,
    readOnlyIntent: form.readOnlyIntent,
    createdAt: form.createdAt || new Date(0).toISOString(),
    updatedAt: form.updatedAt || new Date(0).toISOString(),
  };
  return {
    input: { profile, passwordAction, password: passwordAction === PasswordAction.PasswordSet ? form.password : "" },
    errors,
  };
}

/** A short explanation of what to try next, for each failure type. */
export function errorHint(err: ConnectionError, form: Pick<ConnectionForm, "server" | "database">): string {
  switch (err.code) {
    case ErrorCode.ErrPasswordRequired:
      return "Enter the password for this login.";
    case ErrorCode.ErrLoginFailed:
      if (err.serverErrorNumber === 4060) {
        return `The login works, but cannot open database "${form.database}". Check the name, or leave Database empty to use the login's default.`;
      }
      return "SQL Server rejected the login. Check the user name and password, and that the login is enabled.";
    case ErrorCode.ErrCertificate:
      return 'The server\'s TLS certificate is not trusted by this computer. For a local development server with a self-signed certificate, turn on "Trust server certificate". For production, install a trusted certificate on the server.';
    case ErrorCode.ErrTimeout:
      return form.server.includes("\\")
        ? "No response in time. Check the server name and firewall, and that the SQL Server Browser service is running for named instances."
        : "No response in time. Check the server name, port and firewall.";
    case ErrorCode.ErrNetwork:
      return "Could not reach the server. Check the server name and port, and that SQL Server accepts connections.";
    case ErrorCode.ErrCancelled:
      return "The test was cancelled.";
    case ErrorCode.ErrNotFound:
      return "This connection was deleted.";
    default:
      return "SQL Server returned an error.";
  }
}
