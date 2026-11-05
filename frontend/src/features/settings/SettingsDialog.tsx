import { AlertTriangle, FolderOpen } from "lucide-react";
import { useEffect, useId, useState, type ReactNode } from "react";
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
import { LogLevel, Theme, type AppInfo, type Config, type SaveResult } from "@/services/backend";
import { applyDraft, numericFields, toDraft, type NumericDraft, type NumericPath } from "./settings-form";

interface SettingsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  config: Config;
  info: AppInfo;
  onSave: (config: Config) => Promise<SaveResult>;
  onLoadDefaults: () => Promise<Config>;
  onOpenLogFolder: () => Promise<void>;
}

const fieldMeta = Object.fromEntries(numericFields.map((f) => [f.path, f])) as Record<
  NumericPath,
  (typeof numericFields)[number]
>;

export function SettingsDialog({ open, onOpenChange, config, info, onSave, onLoadDefaults, onOpenLogFolder }: SettingsDialogProps) {
  const [draft, setDraft] = useState<Config>(config);
  const [numbers, setNumbers] = useState<NumericDraft>(() => toDraft(config));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // Start from the saved settings each time the dialog opens.
  useEffect(() => {
    if (open) {
      setDraft(config);
      setNumbers(toDraft(config));
      setErrors({});
      setFormError(null);
    }
  }, [open, config]);

  const save = async () => {
    setFormError(null);
    const { config: next, errors: localErrors } = applyDraft(draft, numbers);
    if (Object.keys(localErrors).length > 0) {
      setErrors(localErrors);
      return;
    }
    setSaving(true);
    try {
      const res = await onSave(next);
      if (res.ok) {
        onOpenChange(false);
      } else {
        setErrors(res.fieldErrors);
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const resetToDefaults = async () => {
    try {
      const defaults = await onLoadDefaults();
      setDraft(defaults);
      setNumbers(toDraft(defaults));
      setErrors({});
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    }
  };

  const numberField = (path: NumericPath) => (
    <NumberField
      key={path}
      label={fieldMeta[path].label}
      unit={fieldMeta[path].unit}
      value={numbers[path]}
      error={errors[path]}
      onChange={(v) => setNumbers((n) => ({ ...n, [path]: v }))}
    />
  );

  const setSection = <K extends keyof Config>(section: K, patch: Partial<Config[K]>) =>
    setDraft((d) => ({ ...d, [section]: { ...(d[section] as object), ...patch } }));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-xl">
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle className="text-base">Settings</DialogTitle>
          <DialogDescription className="text-xs">
            Stored locally in <code className="selectable font-mono text-[11px]">{info.configPath}</code>
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto">
          <form
            id="settings-form"
            className="space-y-6 px-5 py-4"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <Section title="Appearance">
              <Row label="Theme" error={errors["appearance.theme"]}>
                <Select
                  value={draft.appearance.theme}
                  onValueChange={(v) => setSection("appearance", { theme: v as Theme })}
                >
                  <SelectTrigger size="sm" className="w-40 text-xs" aria-label="Theme">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={Theme.ThemeDark}>Dark</SelectItem>
                    <SelectItem value={Theme.ThemeLight}>Light</SelectItem>
                    <SelectItem value={Theme.ThemeSystem}>Match system</SelectItem>
                  </SelectContent>
                </Select>
              </Row>
              {numberField("appearance.editorFontSize")}
            </Section>

            <Section title="Execution" description="Defaults applied to every query. They can be tightened per connection later.">
              <SwitchRow
                label="Read-only mode"
                description="Only statements verified as read-only are executed."
                checked={draft.execution.readOnly}
                onChange={(readOnly) => setSection("execution", { readOnly })}
              />
              {!draft.execution.readOnly && (
                <p role="alert" className="flex gap-2 rounded-md border border-warning/40 bg-warning/10 p-2 text-xs">
                  <AlertTriangle className="size-4 shrink-0 text-warning" />
                  Statements that modify data will be allowed. A read-only database login is the only reliable protection.
                </p>
              )}
              {numberField("execution.timeoutSeconds")}
              {numberField("execution.maxRows")}
              {numberField("execution.maxResultMB")}
              {numberField("execution.connectTimeoutSeconds")}
            </Section>

            <Section
              title="Debugging probes"
              description="Probe queries are generated read-only queries that collect evidence. They are always shown to you before they run."
            >
              {numberField("execution.maxProbesPerRun")}
              {numberField("execution.probeTimeoutSeconds")}
            </Section>

            <Section title="History">
              <SwitchRow
                label="Keep debugging history"
                description="Stores SQL text, timings, row counts and findings on this computer."
                checked={draft.history.enabled}
                onChange={(enabled) => setSection("history", { enabled })}
              />
              {numberField("history.retentionDays")}
              <SwitchRow
                label="Store result samples"
                description="Saves a few result rows with each session. Results may contain sensitive data."
                checked={draft.history.storeResultSamples}
                onChange={(storeResultSamples) => setSection("history", { storeResultSamples })}
              />
            </Section>

            <Section title="Logging">
              <Row label="Log level" error={errors["logging.level"]}>
                <Select value={draft.logging.level} onValueChange={(v) => setSection("logging", { level: v as LogLevel })}>
                  <SelectTrigger size="sm" className="w-40 text-xs" aria-label="Log level">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={LogLevel.LogDebug}>Debug</SelectItem>
                    <SelectItem value={LogLevel.LogInfo}>Info</SelectItem>
                    <SelectItem value={LogLevel.LogWarn}>Warning</SelectItem>
                    <SelectItem value={LogLevel.LogError}>Error</SelectItem>
                  </SelectContent>
                </Select>
              </Row>
              <SwitchRow
                label="Include SQL text in debug logs"
                description="Passwords are always masked, but SQL may contain sensitive literals."
                checked={draft.logging.logSqlText}
                onChange={(logSqlText) => setSection("logging", { logSqlText })}
              />
              {info.logPath && (
                <div className="flex items-center justify-between gap-3">
                  <code className="selectable truncate font-mono text-[11px] text-muted-foreground" title={info.logPath}>
                    {info.logPath}
                  </code>
                  <Button type="button" variant="outline" size="sm" className="h-7 shrink-0 text-xs" onClick={() => void onOpenLogFolder()}>
                    <FolderOpen className="size-3.5" /> Open folder
                  </Button>
                </div>
              )}
            </Section>
          </form>
        </div>

        <DialogFooter className="items-center border-t px-5 py-3 sm:justify-between">
          <Button type="button" variant="ghost" size="sm" className="text-xs" onClick={() => void resetToDefaults()}>
            Reset to defaults
          </Button>
          <div className="flex items-center gap-2">
            {formError && (
              <span role="alert" className="max-w-56 truncate text-xs text-destructive" title={formError}>
                {formError}
              </span>
            )}
            <Button type="button" variant="outline" size="sm" className="text-xs" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" form="settings-form" size="sm" className="text-xs" disabled={saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Section({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-3">
      <legend className="mb-2 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</legend>
      {description && <p className="-mt-1 text-xs text-muted-foreground">{description}</p>}
      {children}
    </fieldset>
  );
}

function Row({ label, error, children, htmlFor }: { label: string; error?: string; children: ReactNode; htmlFor?: string }) {
  return (
    <div>
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor={htmlFor} className="text-xs font-normal">
          {label}
        </Label>
        {children}
      </div>
      {error && (
        <p role="alert" className="mt-1 text-right text-xs text-destructive">
          {label} {error}
        </p>
      )}
    </div>
  );
}

function NumberField({
  label,
  unit,
  value,
  error,
  onChange,
}: {
  label: string;
  unit: string;
  value: string;
  error?: string;
  onChange: (v: string) => void;
}) {
  const id = useId();
  return (
    <Row label={label} error={error} htmlFor={id}>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          inputMode="numeric"
          value={value}
          aria-invalid={error ? true : undefined}
          onChange={(e) => onChange(e.target.value)}
          className="h-7 w-24 text-right font-mono text-xs"
        />
        <span className="w-14 text-xs text-muted-foreground">{unit}</span>
      </div>
    </Row>
  );
}

function SwitchRow({
  label,
  description,
  checked,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  const id = useId();
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <Label htmlFor={id} className="text-xs font-normal">
          {label}
        </Label>
        <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </div>
  );
}
