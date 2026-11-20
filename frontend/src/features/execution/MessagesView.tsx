import { MessageSquareText } from "lucide-react";
import { EmptyState } from "@/components/shell/EmptyState";
import { cn } from "@/lib/utils";
import { Outcome, type Message } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { formatDuration, rowCount } from "./RunStatus";

interface MessagesViewProps {
  run: Run | null;
  timeoutSeconds: number;
  /** Moves the editor cursor to a line of the tab the run came from. */
  onRevealLine: (line: number) => void;
}

/** "Batch 2 · line 5 · ran 3 times", above the messages of one GO batch. */
function BatchHeader({ run, batch }: { run: Run; batch: number }) {
  const executions = run.batches.filter((b) => b.batch === batch);
  const line = (executions[0]?.line ?? 1) + run.lineOffset;
  return (
    <p className="mt-2 mb-0.5 border-b border-border/60 pb-0.5 font-sans text-[11px] font-medium text-muted-foreground first:mt-0">
      Batch {batch + 1} · line {line}
      {executions.length > 1 && ` · ran ${executions.length} times`}
    </p>
  );
}

/**
 * "Msg 207, Level 16, State 1, Line 3", as SQL Server tools print errors. The line is the line
 * in the script that ran (the server counts from the start of each GO batch), as the CLI prints it.
 */
export function messageHeader(m: Message): string {
  const parts = [`Msg ${m.number}`, `Level ${m.severity}`, `State ${m.state}`];
  if (m.procedure) parts.push(`Procedure ${m.procedure}`);
  const line = m.scriptLine > 0 ? m.scriptLine : m.line;
  if (line > 0) parts.push(`Line ${line}`);
  return parts.join(", ");
}

/** Server messages and errors in order, followed by how the run ended. */
export function MessagesView({ run, timeoutSeconds, onRevealLine }: MessagesViewProps) {
  if (!run) {
    return (
      <EmptyState icon={MessageSquareText} title="No messages">
        Server messages, row counts and errors appear here.
      </EmptyState>
    );
  }
  const s = run.summary;
  // With GO batches, messages are grouped under the batch that produced them.
  const multiBatch = new Set(run.batches.map((b) => b.batch)).size > 1;

  return (
    <div className="selectable h-full overflow-auto bg-editor px-3 py-2 font-mono text-xs leading-relaxed" role="log" aria-label="Messages">
      {run.messages.map((m, i) => {
        const header =
          multiBatch && (i === 0 || run.messages[i - 1]!.batch !== m.batch) ? (
            <BatchHeader key={`b${i}`} run={run} batch={m.batch} />
          ) : null;
        if (m.kind !== "error") {
          return [
            header,
            <p key={i} className="whitespace-pre-wrap">
              {m.text}
            </p>,
          ];
        }
        // The server counts lines from the start of the batch; ScriptLine is the line in the
        // script that ran. Lines inside a procedure refer to the procedure, not to the editor.
        const scriptLine = m.scriptLine > 0 ? m.scriptLine : m.line > 0 && !m.procedure ? m.line : 0;
        const editorLine = scriptLine > 0 ? scriptLine + run.lineOffset : 0;
        return [
          header,
          <div key={i} className="my-1 text-destructive">
            {editorLine > 0 ? (
              <button
                type="button"
                className="text-left underline-offset-2 hover:underline"
                title={`Go to line ${editorLine} in the editor`}
                onClick={() => onRevealLine(editorLine)}
              >
                {messageHeader(m)}
              </button>
            ) : (
              <p>{messageHeader(m)}</p>
            )}
            <p className="whitespace-pre-wrap">{m.text}</p>
          </div>,
        ];
      })}

      {s?.rowsAffected?.map((n, i) => (
        <p key={`ra${i}`}>({rowCount(n)} affected)</p>
      ))}

      {run.status === "finished" && (
        <div className="mt-2 space-y-1 border-t pt-2 font-sans text-muted-foreground">
          {run.error && <p className="text-destructive">{run.error}</p>}
          {s?.outcome === Outcome.OutcomeFailed && s.error && <p className="text-destructive">{s.error}</p>}
          {s?.outcome === Outcome.OutcomeCancelled && <p>The query was cancelled on the server.</p>}
          {s?.outcome === Outcome.OutcomeTimedOut && (
            <p className="text-warning">The query was stopped after the {timeoutSeconds} s timeout. The timeout is in Settings.</p>
          )}
          {s?.guard.endedByBatch && (
            <p role="alert" className="text-warning">
              A COMMIT or ROLLBACK in the batch ended read-only protection. Changes made after it may have been committed.
            </p>
          )}
          {s?.guard.active && !s.guard.endedByBatch && (
            <p>Read-only mode: the batch ran in a transaction that was rolled back, so changes it made were undone.</p>
          )}
          {s && (
            <p className={cn(s.errorCount > 0 && "text-foreground")}>
              {s.outcome === Outcome.OutcomeFailed ? "Not run." : `Finished in ${formatDuration(s.elapsedMs)}.`}{" "}
              {s.outcome !== Outcome.OutcomeFailed && `${rowCount(s.totalRows)} returned.`}
              {run.finishedAt && ` Completed at ${new Date(run.finishedAt).toLocaleTimeString()}.`}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
