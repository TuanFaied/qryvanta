import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Outcome, type Message } from "@/services/backend";
import type { Run } from "@/stores/execution-store";
import { MessagesView, messageHeader } from "./MessagesView";

const msg = (m: Partial<Message>): Message => ({
  kind: "error",
  number: 207,
  severity: 16,
  state: 1,
  line: 2,
  procedure: "",
  text: "Invalid column name 'nmae'.",
  afterResultSet: 0,
  batch: 0,
  scriptLine: 0,
  ...m,
});

function run(extra: Partial<Run> = {}): Run {
  return {
    runId: "r1",
    tabId: "t1",
    connectionId: "c1",
    database: "Sales",
    status: "finished",
    startedAt: 0,
    finishedAt: 1000,
    lineOffset: 9,
    batches: [],
    resultSets: [],
    messages: [],
    plan: "",
    plans: [],
    summary: {
      outcome: Outcome.OutcomeSucceeded,
      resultSets: [],
      rowsAffected: [3],
      messages: [],
      errorCount: 1,
      truncatedBy: "",
      totalRows: 0,
      totalBytes: 0,
      elapsedMs: 42,
      guard: { active: true, rolledBack: true, endedByBatch: false },
      batches: [],
      refusals: [],
      error: "",
    },
    error: null,
    ...extra,
  };
}

describe("MessagesView", () => {
  it("formats errors like SQL Server tools", () => {
    expect(messageHeader(msg({}))).toBe("Msg 207, Level 16, State 1, Line 2");
    expect(messageHeader(msg({ procedure: "dbo.p", line: 0 }))).toBe("Msg 207, Level 16, State 1, Procedure dbo.p");
  });

  it("moves the editor to the error line, offset by where the selection started", async () => {
    const onRevealLine = vi.fn();
    render(<MessagesView run={run({ messages: [msg({})] })} timeoutSeconds={30} onRevealLine={onRevealLine} />);
    await userEvent.click(screen.getByRole("button", { name: "Msg 207, Level 16, State 1, Line 2" }));
    expect(onRevealLine).toHaveBeenCalledWith(11);
  });

  it("does not link errors inside procedures, whose lines are not editor lines", () => {
    render(<MessagesView run={run({ messages: [msg({ procedure: "dbo.p", line: 5 })] })} timeoutSeconds={30} onRevealLine={vi.fn()} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("Msg 207, Level 16, State 1, Procedure dbo.p, Line 5")).toBeInTheDocument();
  });

  it("shows info messages, row counts, the rollback note and how the run ended", () => {
    render(
      <MessagesView
        run={run({ messages: [msg({ kind: "info", text: "Hello from PRINT", number: 0 })] })}
        timeoutSeconds={30}
        onRevealLine={vi.fn()}
      />,
    );
    expect(screen.getByText("Hello from PRINT")).toBeInTheDocument();
    expect(screen.getByText("(3 rows affected)")).toBeInTheDocument();
    expect(screen.getByText(/transaction that was rolled back/)).toBeInTheDocument();
    expect(screen.getByText(/Finished in 42 ms/)).toBeInTheDocument();
  });

  it("warns when the batch ended read-only protection, and explains timeouts", () => {
    const base = run();
    render(
      <MessagesView
        run={{ ...base, summary: { ...base.summary!, outcome: Outcome.OutcomeTimedOut, guard: { active: true, rolledBack: false, endedByBatch: true } } }}
        timeoutSeconds={30}
        onRevealLine={vi.fn()}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("ended read-only protection");
    expect(screen.getByText(/after the 30 s timeout/)).toBeInTheDocument();
    expect(screen.queryByText(/transaction that was rolled back/)).not.toBeInTheDocument();
  });

  it("groups messages by GO batch and links errors to their script line", async () => {
    const onRevealLine = vi.fn();
    render(
      <MessagesView
        run={run({
          lineOffset: 0,
          batches: [
            { batch: 0, execution: 0, line: 1, errorCount: 0, elapsedMs: 1 },
            { batch: 1, execution: 0, line: 4, errorCount: 1, elapsedMs: 1 },
            { batch: 1, execution: 1, line: 4, errorCount: 1, elapsedMs: 1 },
          ],
          messages: [msg({ kind: "info", text: "first", number: 0, line: 0 }), msg({ batch: 1, line: 2, scriptLine: 5 })],
        })}
        timeoutSeconds={30}
        onRevealLine={onRevealLine}
      />,
    );
    expect(screen.getByText("Batch 1 · line 1")).toBeInTheDocument();
    expect(screen.getByText("Batch 2 · line 4 · ran 2 times")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Msg 207, Level 16, State 1, Line 5" }));
    expect(onRevealLine).toHaveBeenCalledWith(5);
  });

  it("shows no batch headers for a single batch", () => {
    render(
      <MessagesView
        run={run({ batches: [{ batch: 0, execution: 0, line: 1, errorCount: 0, elapsedMs: 1 }], messages: [msg({})] })}
        timeoutSeconds={30}
        onRevealLine={vi.fn()}
      />,
    );
    expect(screen.queryByText(/^Batch 1/)).not.toBeInTheDocument();
  });

  function refused(extra: Partial<Run> = {}): Run {
    const base = run({ lineOffset: 9, columnOffset: 4, ...extra });
    return {
      ...base,
      summary: {
        ...base.summary!,
        outcome: Outcome.OutcomeRefused,
        errorCount: 0,
        rowsAffected: [],
        guard: { active: false, rolledBack: false, endedByBatch: false },
        error: "Read-only mode did not run the script.",
        refusals: [
          { batch: 0, line: 1, column: 8, offset: 7, length: 10, statement: "UPDATE", reason: "Writes to dbo.Orders." },
          { batch: 0, line: 3, column: 1, offset: 40, length: 14, statement: "EXEC", reason: "Runs the procedure dbo.p." },
        ],
      },
    };
  }

  it("lists what read-only mode refused, and selects it in the editor", async () => {
    const onRevealRange = vi.fn();
    const onOpenSettings = vi.fn();
    render(
      <MessagesView run={refused()} timeoutSeconds={30} onRevealLine={vi.fn()} onRevealRange={onRevealRange} onOpenSettings={onOpenSettings} />,
    );
    expect(screen.getByRole("alert", { name: "Refused by read-only mode" })).toHaveTextContent("Nothing was sent to the server");
    expect(screen.getByText("Writes to dbo.Orders.")).toBeInTheDocument();
    // The selection started on editor line 10, column 5: its first line is shifted by both.
    await userEvent.click(screen.getByRole("button", { name: "Line 10, column 12 · UPDATE" }));
    expect(onRevealRange).toHaveBeenCalledWith(10, 12, 10);
    // Later lines are shifted only by the line.
    await userEvent.click(screen.getByRole("button", { name: "Line 12, column 1 · EXEC" }));
    expect(onRevealRange).toHaveBeenCalledWith(12, 1, 14);

    await userEvent.click(screen.getByRole("button", { name: "Turn off read-only mode…" }));
    expect(onOpenSettings).toHaveBeenCalled();
    expect(screen.getByText(/Not run\./)).toBeInTheDocument();
    expect(screen.queryByText(/returned\./)).not.toBeInTheDocument();
    expect(screen.queryByText(/transaction that was rolled back/)).not.toBeInTheDocument();
  });

  it("explains a refusal without statements, such as an unavailable parser", () => {
    const r = refused();
    r.summary = { ...r.summary!, refusals: [], error: "Read-only mode checks every statement with the T-SQL parser…" };
    render(<MessagesView run={r} timeoutSeconds={30} onRevealLine={vi.fn()} />);
    expect(screen.getByText(/checks every statement with the T-SQL parser/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Turn off/ })).not.toBeInTheDocument();
  });
});
