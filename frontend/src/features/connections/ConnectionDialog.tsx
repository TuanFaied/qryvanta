import { CheckCircle2, Loader2, PlugZap, XCircle } from "lucide-react";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import {
  AuthMethod,
  EncryptMode,
  type ConnectionDTO,
  type ConnectionInput,
  type SaveConnectionResult,
  type TestConnectionResult,
  type WriteAccess,
} from "@/services/backend";
import { describeAccess } from "./access";
import { emptyForm, errorHint, formFromConnection, toInput, type ConnectionForm } from "./connection-form";

export interface ConnectionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Connection to edit, or null to create one. */
  editing: ConnectionDTO | null;
  onSave: (input: ConnectionInput) => Promise<SaveConnectionResult>;
  onTest: (input: ConnectionInput, signal: AbortSignal) => Promise<TestConnectionResult>;
}

type TestState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "done"; result: TestConnectionResult };

const encryptOptions = [
  { value: EncryptMode.EncryptMandatory, label: "Mandatory", hint: "Encrypt the connection (recommended)" },
  { value: EncryptMode.EncryptStrict, label: "Strict (TDS 8)", hint: "SQL Server 2022+, always validates the certificate" },
  { value: EncryptMode.EncryptOptional, label: "Optional", hint: "Encrypt only if the server requires it" },
];

export function ConnectionDialog({ open, onOpenChange, editing, onSave, onTest }: ConnectionDialogProps) {
  const [form, setForm] = useState<ConnectionForm>(emptyForm);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [test, setTest] = useState<TestState>({ kind: "idle" });
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!open) return;
    setForm(editing ? formFromConnection(editing) : emptyForm());
    setErrors({});
    setFormError(null);
    setTest({ kind: "idle" });
  }, [open, editing]);

  // Cancel a running test when the dialog closes.
  useEffect(() => {
    if (!open) abortRef.current?.abort();
  }, [open]);

  const update = (patch: Partial<ConnectionForm>) => {
    setForm((f) => ({ ...f, ...patch }));
    // A test result describes the old settings; clear it once anything changes.
    setTest((t) => (t.kind === "done" ? { kind: "idle" } : t));
  };

  const runTest = async () => {
    const { input, errors: local } = toInput(form);
    setErrors(local);
    if (Object.keys(local).length > 0) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setTest({ kind: "running" });
    try {
      const result = await onTest(input, controller.signal);
      if (controller.signal.aborted) return;
      setErrors(result.fieldErrors);
      setTest(Object.keys(result.fieldErrors).length > 0 ? { kind: "idle" } : { kind: "done", result });
    } catch (err) {
      if (!controller.signal.aborted) {
        setFormError(err instanceof Error ? err.message : String(err));
        setTest({ kind: "idle" });
      }
    } finally {
      if (abortRef.current === controller) abortRef.current = null;
    }
  };

  // Return to the idle state at once; the cancelled backend call settles in the background.
  const cancelTest = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setTest({ kind: "idle" });
  };

  const save = async () => {
    setFormError(null);
    const { input, errors: local } = toInput(form);
    if (Object.keys(local).length > 0) {
      setErrors(local);
      return;
    }
    setSaving(true);
    try {
      const res = await onSave(input);
      if (res.ok) onOpenChange(false);
      else setErrors(res.fieldErrors);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const sqlAuth = form.auth === AuthMethod.AuthSQL;
  const strict = form.encrypt === EncryptMode.EncryptStrict;

  let passwordPlaceholder = "Password";
  if (form.hasStoredPassword && !form.clearPassword) passwordPlaceholder = "Saved. Type to replace it.";
  else if (form.clearPassword) passwordPlaceholder = "The saved password will be removed";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 p-0 sm:max-w-xl">
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle className="text-base">{editing ? `Edit connection` : "New connection"}</DialogTitle>
          <DialogDescription className="text-xs">
            Microsoft SQL Server or Azure SQL. Passwords are stored in Windows Credential Manager, never in Qryvanta's
            files.
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto">
          <form
            id="connection-form"
            className="space-y-5 px-5 py-4"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <Section title="Server">
              <TextField label="Name" value={form.name} error={errors.name} placeholder="Sales reporting (prod)" onChange={(name) => update({ name })} autoFocus />
              <div className="grid grid-cols-[1fr_7rem] gap-3">
                <TextField
                  label="Server"
                  value={form.server}
                  error={errors.server}
                  placeholder={String.raw`db01.corp.local or localhost\SQLEXPRESS`}
                  onChange={(server) => update({ server })}
                  mono
                />
                <TextField label="Port" value={form.port} error={errors.port} placeholder="1433" onChange={(port) => update({ port })} mono />
              </div>
              <TextField
                label="Database"
                value={form.database}
                error={errors.database}
                placeholder="Leave empty for the login's default"
                onChange={(database) => update({ database })}
                mono
              />
            </Section>

            <Section title="Authentication">
              <Field label="Method" error={errors.auth}>
                {(id) => (
                  <Select value={form.auth} onValueChange={(v) => update({ auth: v as AuthMethod })}>
                    <SelectTrigger id={id} size="sm" className="w-full text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={AuthMethod.AuthSQL}>SQL Server authentication</SelectItem>
                      <SelectItem value={AuthMethod.AuthWindows}>Windows authentication (current user)</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              </Field>
              {sqlAuth ? (
                <div className="grid grid-cols-2 gap-3">
                  <TextField label="User name" value={form.username} error={errors.username} onChange={(username) => update({ username })} autoComplete="off" />
                  <div className="space-y-1.5">
                    <TextField
                      label="Password"
                      type="password"
                      value={form.password}
                      placeholder={passwordPlaceholder}
                      disabled={form.clearPassword}
                      onChange={(password) => update({ password })}
                      autoComplete="new-password"
                    />
                    {form.hasStoredPassword && (
                      <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <input
                          type="checkbox"
                          checked={form.clearPassword}
                          onChange={(e) => update({ clearPassword: e.target.checked, password: "" })}
                          className="accent-primary"
                        />
                        Remove saved password
                      </label>
                    )}
                  </div>
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">Qryvanta connects as the Windows user running the app. No password is stored.</p>
              )}
            </Section>

            <Section title="Security">
              <Field label="Encryption" error={errors.encrypt}>
                {(id) => (
                  <Select value={form.encrypt} onValueChange={(v) => update({ encrypt: v as EncryptMode })}>
                    <SelectTrigger id={id} size="sm" className="w-full text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {encryptOptions.map((o) => (
                        <SelectItem key={o.value} value={o.value} className="text-xs">
                          {o.label} <span className="text-muted-foreground">({o.hint})</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </Field>
              <SwitchField
                label="Trust server certificate"
                description={
                  strict
                    ? "Not available with strict encryption, which always validates the certificate."
                    : "Skips certificate validation. Only use this for local development servers with self-signed certificates."
                }
                checked={form.trustServerCertificate && !strict}
                disabled={strict}
                error={errors.trustServerCertificate}
                onChange={(trustServerCertificate) => update({ trustServerCertificate })}
              />
              <SwitchField
                label="Read-only application intent"
                description="Routes to a readable secondary in an availability group. Requires a database name. This does not prevent writes; use a read-only login for that."
                checked={form.readOnlyIntent}
                onChange={(readOnlyIntent) => update({ readOnlyIntent })}
              />
              <TextField
                label="Connection timeout (seconds)"
                value={form.timeout}
                error={errors.connectTimeoutSeconds}
                placeholder="App default"
                onChange={(timeout) => update({ timeout })}
                mono
              />
            </Section>

            <TestResultPanel test={test} form={form} />
          </form>
        </div>

        <DialogFooter className="items-center border-t px-5 py-3 sm:justify-between">
          <div className="flex items-center gap-2">
            {test.kind === "running" ? (
              <>
                <Button type="button" variant="outline" size="sm" className="text-xs" disabled>
                  <Loader2 className="size-3.5 animate-spin" /> Testing…
                </Button>
                <Button type="button" variant="ghost" size="sm" className="text-xs" onClick={cancelTest}>
                  Cancel test
                </Button>
              </>
            ) : (
              <Button type="button" variant="outline" size="sm" className="text-xs" onClick={() => void runTest()}>
                <PlugZap className="size-3.5" /> Test connection
              </Button>
            )}
          </div>
          <div className="flex items-center gap-2">
            {formError && (
              <span role="alert" className="max-w-56 truncate text-xs text-destructive" title={formError}>
                {formError}
              </span>
            )}
            <Button type="button" variant="outline" size="sm" className="text-xs" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" form="connection-form" size="sm" className="text-xs" disabled={saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TestResultPanel({ test, form }: { test: TestState; form: ConnectionForm }) {
  if (test.kind !== "done") return null;
  const { result } = test;
  if (result.ok) {
    const s = result.server;
    return (
      <div role="status" className="rounded-md border border-success/40 bg-success/10 p-3 text-xs">
        <p className="flex items-center gap-1.5 font-medium">
          <CheckCircle2 className="size-4 text-success" /> Connected in {result.latencyMs} ms
        </p>
        <dl className="selectable mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
          <dt className="text-muted-foreground">Server</dt>
          <dd>{s.serverName}</dd>
          <dt className="text-muted-foreground">Version</dt>
          <dd>
            {s.productVersion} {s.productLevel}
            {s.cloud ? " (Azure)" : ""}
          </dd>
          <dt className="text-muted-foreground">Edition</dt>
          <dd>{s.edition}</dd>
          <dt className="text-muted-foreground">Database</dt>
          <dd>{s.database}</dd>
          <dt className="text-muted-foreground">Login</dt>
          <dd>{s.login}</dd>
          {result.access && (
            <>
              <dt className="text-muted-foreground">Write access</dt>
              <dd>
                <AccessSummary access={result.access} />
              </dd>
            </>
          )}
        </dl>
      </div>
    );
  }
  const err = result.error;
  if (!err) return null;
  return (
    <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-xs">
      <p className="flex items-center gap-1.5 font-medium">
        <XCircle className="size-4 text-destructive" /> Connection failed
        {err.serverErrorNumber ? <span className="font-normal text-muted-foreground">SQL Server error {err.serverErrorNumber}</span> : null}
      </p>
      <p className="mt-1.5">{errorHint(err, form)}</p>
      <p className="selectable mt-1.5 font-mono text-[11px] break-words text-muted-foreground">{err.message}</p>
    </div>
  );
}

function AccessSummary({ access }: { access: WriteAccess }) {
  const d = describeAccess(access);
  return (
    <div>
      <span
        className={cn(
          "font-medium",
          d.tone === "ok" && "text-success",
          d.tone === "warn" && "text-warning",
          d.tone === "danger" && "text-destructive",
        )}
      >
        {d.label}
      </span>
      <p className="mt-0.5 text-muted-foreground">{d.detail}</p>
      {(access.reasons ?? []).length > 0 && (
        <ul className="mt-1 list-disc pl-4 text-muted-foreground">
          {(access.reasons ?? []).slice(0, 4).map((r) => (
            <li key={r.text}>{r.text}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-3">
      <legend className="mb-2 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</legend>
      {children}
    </fieldset>
  );
}

function Field({ label, error, children }: { label: string; error?: string | undefined; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs font-normal">
        {label}
      </Label>
      {children(id)}
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {label} {error}
        </p>
      )}
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
  error,
  placeholder,
  type = "text",
  mono,
  disabled,
  autoFocus,
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  error?: string | undefined;
  placeholder?: string;
  type?: "text" | "password";
  mono?: boolean;
  disabled?: boolean;
  autoFocus?: boolean;
  autoComplete?: string;
}) {
  return (
    <Field label={label} error={error}>
      {(id) => (
        <Input
          id={id}
          type={type}
          value={value}
          placeholder={placeholder}
          disabled={disabled}
          autoFocus={autoFocus}
          autoComplete={autoComplete}
          spellCheck={false}
          aria-invalid={error ? true : undefined}
          onChange={(e) => onChange(e.target.value)}
          className={cn("h-8 text-xs", mono && "font-mono")}
        />
      )}
    </Field>
  );
}

function SwitchField({
  label,
  description,
  checked,
  onChange,
  disabled,
  error,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  error?: string | undefined;
}) {
  const id = useId();
  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <Label htmlFor={id} className="text-xs font-normal">
            {label}
          </Label>
          <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
        </div>
        <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} />
      </div>
      {error && (
        <p role="alert" className="mt-1 text-xs text-destructive">
          {label} {error}
        </p>
      )}
    </div>
  );
}
