import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Classification, EvidenceKind, Severity, type Analysis, type Finding } from "@/services/backend";
import { useAnalysisStore, type AnalysisTarget } from "@/stores/analysis-store";
import { AnalysisPanel } from "./AnalysisPanel";

const sql = "SELECT *\nFROM Sales.Customers WHERE Region = NULL";
const at = sql.indexOf("Region = NULL");
const column = at - sql.indexOf("\n");
const span = { offset: at, length: 13, line: 2, column };

const nullFinding: Finding = {
  ruleId: "QRY-NULL-001",
  title: "\"Region = NULL\" is never true",
  explanation: "Comparing with NULL using = gives UNKNOWN for every row.",
  severity: Severity.SeverityHigh,
  classification: Classification.Confirmed,
  span,
  evidence: [{ kind: EvidenceKind.EvidenceStatic, summary: "Compares with the NULL literal.", static: { span, text: "Region = NULL" } }],
  correction: { description: "Use IS NULL.", span, replacement: "Region IS NULL" },
};

const planFinding: Finding = {
  ruleId: "QRY-TEST-001",
  title: "Estimate far off",
  explanation: "The estimate is far from the actual row count.",
  severity: Severity.SeverityLow,
  classification: Classification.Potential,
  span: { offset: 0, length: 6, line: 1, column: 1 },
  evidence: [
    { kind: EvidenceKind.EvidencePlan, summary: "Rows differ.", plan: { nodeId: 3, operator: "Hash Match", property: "rows", value: "120", actual: false } },
    { kind: EvidenceKind.EvidenceProbe, summary: "Probe counted rows.", probe: { sql: "SELECT COUNT(*) FROM t", result: "4000" } },
  ],
};

const analysis = (extra: Partial<Analysis> = {}): Analysis => ({
  available: true,
  reason: "",
  findings: [],
  failures: [],
  rules: 3,
  statements: 1,
  syntaxErrors: 0,
  metadata: { checked: true, reason: "", unresolved: [] },
  ...extra,
});

const target: AnalysisTarget = { tabId: "t1", sql, tabText: sql, startLine: 1, startColumn: 1, connectionId: "c1", database: "Sales" };

function setup(state: Partial<ReturnType<typeof useAnalysisStore.getState>>, activeTab: { id: string; content: string } | null = { id: "t1", content: sql }) {
  useAnalysisStore.setState({ target: null, running: false, result: null, error: null, ...state });
  const onAnalyze = vi.fn();
  const onRevealRange = vi.fn();
  render(<AnalysisPanel activeTab={activeTab} onAnalyze={onAnalyze} onRevealRange={onRevealRange} />);
  return { onAnalyze, onRevealRange };
}

beforeEach(() => useAnalysisStore.setState({ target: null, running: false, result: null, error: null }));

describe("AnalysisPanel", () => {
  it("starts empty and analyzes on request", async () => {
    const { onAnalyze } = setup({});
    expect(screen.getByText(/Nothing analyzed yet\. Analyze runs the diagnostic rules/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Analyze/ }));
    expect(onAnalyze).toHaveBeenCalledOnce();
  });

  it("cannot analyze without a tab", () => {
    setup({}, null);
    expect(screen.getByRole("button", { name: /Analyze/ })).toBeDisabled();
  });

  it("says when the parser is unavailable, distinctly from no findings", () => {
    setup({ target, result: analysis({ available: false, reason: "parser unavailable: sidecar not found", rules: 0 }) });
    expect(screen.getByRole("status")).toHaveTextContent("Parser unavailable, so the query could not be analyzed: parser unavailable: sidecar not found");
    expect(screen.queryByText(/No findings/)).not.toBeInTheDocument();
  });

  it("says when there are no findings", () => {
    setup({ target, result: analysis() });
    expect(screen.getByRole("status")).toHaveTextContent("No findings. 3 rules found nothing to report.");
  });

  it("says when there was nothing to analyze, and about syntax errors", () => {
    setup({ target, result: analysis({ statements: 0, syntaxErrors: 2 }) });
    expect(screen.getByRole("status")).toHaveTextContent("Nothing to analyze");
    expect(screen.getByText(/2 syntax errors: batches with syntax errors were not analyzed/)).toBeInTheDocument();
  });

  it("groups findings by severity with classification, evidence and the correction as a diff", () => {
    setup({ target, result: analysis({ findings: [planFinding, nullFinding] }) });
    const high = screen.getByRole("region", { name: "High findings" });
    const low = screen.getByRole("region", { name: "Low findings" });
    // High comes first, whatever the order of the findings.
    expect(high.compareDocumentPosition(low) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(high).getByText("Confirmed issue")).toBeInTheDocument();
    expect(within(high).getByText("QRY-NULL-001")).toBeInTheDocument();
    expect(within(high).getByText(new RegExp(`Line 2, column ${column}`))).toBeInTheDocument();
    expect(within(high).getByText("Compares with the NULL literal.")).toBeInTheDocument();
    const diff = within(high).getByLabelText("Suggested correction");
    expect(diff).toHaveTextContent("not applied automatically");
    expect(within(diff).getByText("- Region = NULL")).toBeInTheDocument();
    expect(within(diff).getByText("+ Region IS NULL")).toBeInTheDocument();
    expect(within(low).getByText("Potential issue")).toBeInTheDocument();
    expect(within(low).getByText(/Hash Match \(node 3\): rows = 120 · estimated/)).toBeInTheDocument();
    expect(within(low).getByText("SELECT COUNT(*) FROM t")).toBeInTheDocument();
    expect(within(low).getByText("Result: 4000")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Findings/ })).toHaveTextContent("(2)");
  });

  it("selects a finding's span in the editor, offset by the selection it came from", async () => {
    const fromSelection = { ...target, startLine: 10, startColumn: 5 };
    const { onRevealRange } = setup({ target: fromSelection, result: analysis({ findings: [nullFinding, { ...planFinding, span: { offset: 0, length: 6, line: 1, column: 1 } }] }) });
    await userEvent.click(screen.getByRole("button", { name: nullFinding.title }));
    expect(onRevealRange).toHaveBeenLastCalledWith(11, column, 13);
    await userEvent.click(screen.getByRole("button", { name: planFinding.title }));
    expect(onRevealRange).toHaveBeenLastCalledWith(10, 5, 6);
  });

  it("warns when the text changed and does not select in another tab", async () => {
    const { onRevealRange } = setup({ target, result: analysis({ findings: [nullFinding] }) }, { id: "t1", content: sql + " AND 1 = 1" });
    expect(screen.getByText(/The text changed after this analysis/)).toBeInTheDocument();
    cleanup();
    setup({ target, result: analysis({ findings: [nullFinding] }) }, { id: "t2", content: "SELECT 2" });
    expect(screen.getByText("These findings are for another tab.")).toBeInTheDocument();
    const button = screen.getByRole("button", { name: nullFinding.title });
    expect(button).toBeDisabled();
    await userEvent.click(button);
    expect(onRevealRange).not.toHaveBeenCalled();
  });

  it("says why metadata was not checked, and which tables could not be", () => {
    setup({ target, result: analysis({ metadata: { checked: false, reason: "Not connected: connect to check keys, types and nullability.", unresolved: [] } }) });
    expect(screen.getByText("Not connected: connect to check keys, types and nullability.")).toBeInTheDocument();
    cleanup();
    setup({
      target,
      result: analysis({ metadata: { checked: true, reason: "", unresolved: [{ name: "dbo.Missing", reason: "not found, or no permission to see it" }] } }),
    });
    expect(screen.getByText(/Keys, types and nullability were not checked for/)).toBeInTheDocument();
    expect(screen.getByText("dbo.Missing")).toBeInTheDocument();
    expect(screen.getByText(/not found, or no permission to see it/)).toBeInTheDocument();
  });

  it("shows metadata evidence", () => {
    const meta: Finding = {
      ...nullFinding,
      ruleId: "QRY-JOIN-005",
      title: "INNER JOIN drops dbo.Employees rows where DepartmentID is NULL",
      classification: Classification.Potential,
      evidence: [{ kind: EvidenceKind.EvidenceMetadata, summary: "dbo.Employees.DepartmentID allows NULL.",
        metadata: { object: "dbo.Employees", column: "DepartmentID", property: "nullable", value: "yes" } }],
      correction: null,
    };
    setup({ target, result: analysis({ findings: [meta] }) });
    expect(screen.getByText("dbo.Employees.DepartmentID allows NULL.")).toBeInTheDocument();
    expect(screen.getByText("dbo.Employees.DepartmentID: nullable = yes")).toBeInTheDocument();
  });

  it("lists failed rules with the findings of the others", () => {
    setup({ target, result: analysis({ findings: [nullFinding], failures: [{ ruleId: "QRY-TEST-009", message: "panicked: boom" }] }) });
    expect(screen.getByRole("alert")).toHaveTextContent("Some rules failed, so findings may be missing:QRY-TEST-009: panicked: boom");
    expect(screen.getByRole("button", { name: nullFinding.title })).toBeInTheDocument();
  });

  it("shows progress and backend errors", () => {
    setup({ target, running: true });
    expect(screen.getByText("Analyzing…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Analyze/ })).toBeDisabled();
    cleanup();
    setup({ target, error: "Analyze failed: no backend" });
    expect(screen.getByText("Could not analyze the query: Analyze failed: no backend")).toBeInTheDocument();
  });
});
