import {
  columnResizingFeature,
  columnSizingFeature,
  createColumnHelper,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import type { Value } from "@/services/backend";
import type { ResultSetData } from "@/stores/execution-store";
import { copyText } from "@/utils/clipboard";
import {
  describeColumn,
  formatValue,
  inRange,
  initialWidth,
  isNull,
  isNumericColumn,
  normalize,
  rangeToTsv,
  toTsv,
  type CellRange,
} from "./grid-format";

export const ROW_HEIGHT = 24;
const GUTTER_WIDTH = 52;

const features = tableFeatures({ columnSizingFeature, columnResizingFeature });
const helper = createColumnHelper<typeof features, Value[]>();

interface ResultGridProps {
  resultSet: ResultSetData;
  /** Accessible name, for example "Result 1". */
  label: string;
  /** Fixed height in pixels; omit to fill the parent. */
  height?: number;
}

/** A virtualized, read-only grid for one result set, with spreadsheet-style selection and copy. */
export function ResultGrid({ resultSet, label, height }: ResultGridProps) {
  const { columns: cols, rows } = resultSet;
  const scrollRef = useRef<HTMLDivElement>(null);
  const [selection, setSelection] = useState<CellRange | null>(null);
  const dragging = useRef(false);

  // Widths are estimated once the first rows arrive; later batches do not resize columns.
  const hasRows = rows.length > 0;
  const columns = useMemo(
    () =>
      cols.map((c, i) =>
        helper.accessor((row) => row[i], {
          id: `c${i}`,
          header: c.name,
          size: initialWidth(c, rows, i),
          minSize: 40,
          maxSize: 1200,
        }),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cols, hasRows],
  );

  const table = useTable({ features, columns, data: rows, columnResizeMode: "onChange" });
  const headers = table.getHeaderGroups()[0]?.headers ?? [];
  const totalWidth = GUTTER_WIDTH + table.getTotalSize();

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
    // Render a first screenful before the scroll container has been measured.
    initialRect: { width: 1000, height: height ?? 400 },
  });

  const lastCol = cols.length - 1;
  const lastRow = rows.length - 1;
  const all: CellRange | null = rows.length && cols.length ? { anchor: { row: 0, col: 0 }, focus: { row: lastRow, col: lastCol } } : null;

  const copy = (range: CellRange | null, headers = false) => {
    if (range) void copyText(rangeToTsv(cols, rows, range, { headers }));
  };
  const selectedRows = (): CellRange | null => {
    if (!selection) return null;
    const r = normalize(selection);
    return { anchor: { row: r.top, col: 0 }, focus: { row: r.bottom, col: lastCol } };
  };

  const pick = (row: number, col: number, e: MouseEvent) => {
    if (e.button !== 0) {
      // Right-click keeps a selection that contains the cell, so "Copy" copies what is selected.
      if (!inRange(selection, row, col)) setSelection({ anchor: { row, col }, focus: { row, col } });
      return;
    }
    dragging.current = true;
    setSelection((s) => (e.shiftKey && s ? { anchor: s.anchor, focus: { row, col } } : { anchor: { row, col }, focus: { row, col } }));
  };
  const pickRow = (row: number, e: MouseEvent) => {
    if (e.button !== 0 && selection && normalize(selection).top <= row && row <= normalize(selection).bottom) return;
    setSelection((s) =>
      e.shiftKey && s ? { anchor: { row: s.anchor.row, col: 0 }, focus: { row, col: lastCol } } : { anchor: { row, col: 0 }, focus: { row, col: lastCol } },
    );
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const ctrl = e.ctrlKey || e.metaKey;
    if (ctrl && e.key.toLowerCase() === "a") {
      e.preventDefault();
      setSelection(all);
      return;
    }
    if (ctrl && e.key.toLowerCase() === "c") {
      e.preventDefault();
      copy(selection, e.shiftKey);
      return;
    }
    const moves: Record<string, [number, number]> = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] };
    const move = moves[e.key];
    if (!move || !all) return;
    e.preventDefault();
    const from = selection?.focus ?? { row: 0, col: 0 };
    const focus = {
      row: Math.min(Math.max(from.row + move[0], 0), lastRow),
      col: Math.min(Math.max(from.col + move[1], 0), lastCol),
    };
    setSelection(e.shiftKey && selection ? { anchor: selection.anchor, focus } : { anchor: focus, focus });
    virtualizer.scrollToIndex(focus.row);
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          ref={scrollRef}
          role="grid"
          aria-label={label}
          aria-rowcount={rows.length + 1}
          aria-colcount={cols.length}
          tabIndex={0}
          onKeyDown={onKeyDown}
          onMouseUp={() => (dragging.current = false)}
          onMouseLeave={() => (dragging.current = false)}
          className="relative overflow-auto bg-editor font-mono text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring/50"
          style={height ? { height } : { height: "100%" }}
        >
          <div style={{ width: totalWidth, minWidth: "100%" }}>
            <div role="row" className="sticky top-0 z-10 flex border-b bg-panel font-sans" style={{ height: ROW_HEIGHT }}>
              <div
                className="sticky left-0 z-10 shrink-0 cursor-pointer border-r bg-panel"
                style={{ width: GUTTER_WIDTH }}
                title="Select all (Ctrl+A)"
                onMouseDown={() => setSelection(all)}
              />
              {headers.map((header, i) => {
                const col = cols[i]!;
                return (
                  <div
                    key={header.id}
                    role="columnheader"
                    title={describeColumn(col)}
                    className={cn(
                      "relative flex shrink-0 items-center border-r px-2 font-medium",
                      isNumericColumn(col) && "justify-end",
                    )}
                    style={{ width: header.getSize() }}
                  >
                    <span className="truncate">{col.name || <span className="italic text-muted-foreground">(no column name)</span>}</span>
                    <div
                      role="separator"
                      aria-orientation="vertical"
                      aria-label={`Resize ${col.name}`}
                      onMouseDown={header.getResizeHandler()}
                      onDoubleClick={() => header.column.resetSize()}
                      className={cn(
                        "absolute top-0 right-0 h-full w-1.5 cursor-col-resize hover:bg-primary/50",
                        header.column.getIsResizing() && "bg-primary",
                      )}
                    />
                  </div>
                );
              })}
            </div>

            <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
              {virtualizer.getVirtualItems().map((item) => {
                const row = rows[item.index]!;
                return (
                  <div
                    key={item.key}
                    role="row"
                    aria-rowindex={item.index + 2}
                    className="absolute left-0 flex w-full border-b border-border/50"
                    style={{ height: ROW_HEIGHT, transform: `translateY(${item.start}px)` }}
                  >
                    <div
                      role="rowheader"
                      className="sticky left-0 flex shrink-0 cursor-pointer items-center justify-end border-r bg-panel px-2 text-muted-foreground"
                      style={{ width: GUTTER_WIDTH }}
                      onMouseDown={(e) => pickRow(item.index, e)}
                    >
                      {item.index + 1}
                    </div>
                    {headers.map((header, c) => {
                      const value = row[c];
                      const selected = inRange(selection, item.index, c);
                      return (
                        <div
                          key={header.id}
                          role="gridcell"
                          aria-selected={selected}
                          onMouseDown={(e) => pick(item.index, c, e)}
                          onMouseEnter={() => {
                            if (dragging.current) setSelection((s) => (s ? { anchor: s.anchor, focus: { row: item.index, col: c } } : s));
                          }}
                          className={cn(
                            "flex shrink-0 cursor-cell items-center overflow-hidden border-r border-border/50 px-2 whitespace-pre select-none",
                            isNumericColumn(cols[c]!) && "justify-end",
                            selected && "bg-primary/20",
                          )}
                          style={{ width: header.getSize() }}
                        >
                          {isNull(value) ? (
                            <span className="text-muted-foreground/70 italic">NULL</span>
                          ) : (
                            <span className="truncate">{formatValue(value).split(/\r?\n/)[0]}</span>
                          )}
                        </div>
                      );
                    })}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-60">
        <ContextMenuItem disabled={!selection} onSelect={() => copy(selection)}>
          Copy
          <ContextMenuShortcut>Ctrl+C</ContextMenuShortcut>
        </ContextMenuItem>
        <ContextMenuItem disabled={!selection} onSelect={() => copy(selection, true)}>
          Copy with headers
          <ContextMenuShortcut>Ctrl+Shift+C</ContextMenuShortcut>
        </ContextMenuItem>
        <ContextMenuItem disabled={!selection} onSelect={() => copy(selectedRows())}>
          Copy row{selection && normalize(selection).top !== normalize(selection).bottom ? "s" : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem disabled={!all} onSelect={() => all && void copyText(toTsv(cols, rows, cols.map((_, i) => i), { headers: true }))}>
          Copy all with headers
        </ContextMenuItem>
        <ContextMenuItem disabled={!all} onSelect={() => setSelection(all)}>
          Select all
          <ContextMenuShortcut>Ctrl+A</ContextMenuShortcut>
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
