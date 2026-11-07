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
    resultSets: [],
    messages: [],
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
});
