import {
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  useReactFlow,
  useStore,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AlertTriangle } from "lucide-react";
import { createContext, memo, useContext, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { NodeGap, PlanOperator, PlanStatement } from "@/services/backend";
import { costPercent, layoutStatement, NODE_HEIGHT, NODE_WIDTH, objectLabel, strokeWidth, type PlanDirection } from "./plan-model";

interface OperatorData extends Record<string, unknown> {
  op: PlanOperator;
  cost: number | null;
  gap: NodeGap | null;
  direction: PlanDirection;
}

// Selection is shared through context, so selecting an operator re-renders nodes without
// recomputing the layout or handing React Flow new node arrays.
const SelectedContext = createContext<string | null>(null);

/** Below this zoom, operators show only their name: the rest is unreadable, and drawing it for
 * hundreds of operators makes zooming out stutter. */
const DETAIL_ZOOM = 0.45;

const OperatorNode = memo(function OperatorNode({ id, data }: NodeProps<Node<OperatorData>>) {
  const selected = useContext(SelectedContext) === id;
  const detailed = useStore((s) => s.transform[2] >= DETAIL_ZOOM);
  const { op, cost, gap, direction } = data;
  const warnings = (op.warnings ?? []).length;
  // Data flows towards the root: into a node on the root's side, out on the other.
  const inSide = direction === "rtl" ? Position.Right : Position.Left;
  const outSide = direction === "rtl" ? Position.Left : Position.Right;
  if (!detailed) {
    return (
      <div
        aria-label={`${op.physicalOp}, node ${op.nodeId}`}
        className={cn(
          "flex h-full w-full items-center justify-center rounded-md border bg-card px-2 text-center text-lg font-semibold",
          selected && "ring-2 ring-primary",
          (gap?.big || warnings > 0) && "border-warning",
        )}
      >
        <Handle type="target" position={inSide} className="!size-1.5 !border-0" />
        {op.physicalOp}
        <Handle type="source" position={outSide} className="!size-1.5 !border-0" />
      </div>
    );
  }
  return (
    <div
      aria-label={`${op.physicalOp}, node ${op.nodeId}`}
      className={cn(
        "flex h-full w-full flex-col justify-between rounded-md border bg-card px-2 py-1.5 text-[11px] shadow-sm",
        selected && "ring-2 ring-primary",
        gap?.big && "border-warning",
      )}
    >
      <Handle type="target" position={inSide} className="!size-1.5 !border-0 !bg-muted-foreground" />
      <div className="flex items-start gap-1">
        <span className="font-semibold leading-tight">{op.physicalOp}</span>
        {warnings > 0 && (
          <span className="ml-auto flex items-center gap-0.5 text-warning" title={`${warnings} warning(s)`} aria-label={`${warnings} warnings`}>
            <AlertTriangle className="size-3" />
            {warnings}
          </span>
        )}
      </div>
      {objectLabel(op) && <span className="truncate font-mono text-muted-foreground">{objectLabel(op)}</span>}
      <div className="flex items-center justify-between text-muted-foreground">
        <span>Cost {cost == null ? "unavailable" : `${cost.toFixed(cost < 1 ? 1 : 0)}%`}</span>
        {gap?.big && (
          <span className="rounded-sm bg-warning/15 px-1 text-warning" title="Large gap between estimated and actual rows">
            ×{gap.factor >= 100 ? Math.round(gap.factor) : gap.factor.toFixed(1)}
          </span>
        )}
      </div>
      <Handle type="source" position={outSide} className="!size-1.5 !border-0 !bg-muted-foreground" />
    </div>
  );
});

const nodeTypes = { operator: OperatorNode };

/** React Flow follows the app's theme (the "dark" class on the root element). */
function useColorMode(): "dark" | "light" {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  useEffect(() => {
    const mo = new MutationObserver(() => setDark(document.documentElement.classList.contains("dark")));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => mo.disconnect();
  }, []);
  return dark ? "dark" : "light";
}

/** Fits the plan when the statement or direction changes and when the panel is resized. */
function FitView({ layoutKey, container }: { layoutKey: string; container: React.RefObject<HTMLDivElement | null> }) {
  const { fitView } = useReactFlow();
  useEffect(() => {
    const id = requestAnimationFrame(() => void fitView({ padding: 0.15 }));
    return () => cancelAnimationFrame(id);
  }, [layoutKey, fitView]);
  useEffect(() => {
    const el = container.current;
    if (!el) return;
    let frame = 0;
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => void fitView({ padding: 0.15 }));
    });
    ro.observe(el);
    return () => {
      cancelAnimationFrame(frame);
      ro.disconnect();
    };
  }, [container, fitView]);
  return null;
}

/** Plans this large render only the visible operators; below it, mounting and unmounting nodes
 * while panning costs more than drawing them all. */
const VIRTUALIZE_FROM = 400;

/** Plans this large get a minimap. */
const MINIMAP_FROM = 15;

export interface PlanDiagramProps {
  statement: PlanStatement;
  /** The plan has measured rows: arrows follow them; otherwise estimated rows. */
  actual: boolean;
  direction: PlanDirection;
  gaps: Map<number, NodeGap>;
  selected: string | null;
  onSelect: (op: PlanOperator) => void;
}

/** One statement's operators as a tree, with arrows as thick as the rows they carry. */
export function PlanDiagram({ statement, actual, direction, gaps, selected, onSelect }: PlanDiagramProps) {
  const colorMode = useColorMode();
  const container = useRef<HTMLDivElement>(null);
  const { nodes, edges } = useMemo(() => {
    const layout = layoutStatement(statement, direction, actual);
    const root = statement.root ?? null;
    const nodes: Node<OperatorData>[] = layout.nodes.map((n) => ({
      id: n.id,
      type: "operator",
      position: { x: n.x, y: n.y },
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      data: { op: n.op, cost: costPercent(n.op, root), gap: gaps.get(n.op.nodeId) ?? null, direction },
      draggable: false,
      connectable: false,
    }));
    const edges: Edge[] = layout.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      label: e.rows == null ? undefined : formatRows(e.rows),
      style: { strokeWidth: strokeWidth(e.rows), strokeDasharray: e.rows == null ? "4 3" : undefined },
      labelStyle: { fontSize: 10 },
      focusable: false,
    }));
    return { nodes, edges };
  }, [statement, direction, actual, gaps]);

  return (
    <SelectedContext.Provider value={selected}>
      <div ref={container} className="relative h-full w-full" data-testid="plan-diagram">
        <ReactFlow
          colorMode={colorMode}
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodeClick={(_, node) => onSelect((node.data as OperatorData).op)}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          fitView
          minZoom={0.05}
          maxZoom={2}
          onlyRenderVisibleElements={nodes.length > VIRTUALIZE_FROM}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={24} size={1} />
          <Controls showInteractive={false} />
          {nodes.length >= MINIMAP_FROM && <MiniMap pannable zoomable style={{ width: 140, height: 90 }} />}
          <FitView layoutKey={`${statement.id}:${direction}:${nodes.length}`} container={container} />
        </ReactFlow>
        <p className="pointer-events-none absolute top-2 left-2 rounded bg-background/80 px-1.5 py-0.5 text-[11px] text-muted-foreground">
          Arrow thickness: {actual ? "actual rows" : "estimated rows (all executions)"}
        </p>
      </div>
    </SelectedContext.Provider>
  );
}

function formatRows(rows: number): string {
  if (rows >= 1e6) return `${(rows / 1e6).toFixed(1)}M`;
  if (rows >= 1e4) return `${(rows / 1e3).toFixed(0)}K`;
  return Number.isInteger(rows) ? String(rows) : rows.toFixed(1);
}
