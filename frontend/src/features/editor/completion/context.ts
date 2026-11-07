/**
 * Completion context detection: HEURISTIC, by design.
 *
 * Qryvanta has no T-SQL parser until Phase 4. This module tokenizes the text before the cursor
 * just enough to answer three questions: is the cursor inside a string or comment, is it right
 * after a qualifier ("o." or "dbo."), and is it where a table or view name goes (after FROM,
 * JOIN, UPDATE, INTO, or a comma in a FROM list)? It also finds the tables a statement
 * references, with their aliases, from the same tokens. It never decides anything about
 * correctness; a wrong guess only means a less useful suggestion list.
 * Replace it with the parser's query model when Phase 4 lands.
 */

export type Token =
  | { kind: "word"; text: string; quoted: boolean }
  | { kind: "dot" }
  | { kind: "comma" }
  | { kind: "open" }
  | { kind: "close" }
  | { kind: "semicolon" }
  | { kind: "other" };

export interface Tokenized {
  tokens: Token[];
  /** True when the text ends inside a string literal or comment. */
  inLiteral: boolean;
}

const identStart = /[\p{L}_@#]/u;
const identPart = /[\p{L}\p{N}_@#$]/u;

/** Splits SQL into the tokens completion cares about. Strings, numbers and comments are skipped. */
export function tokenize(sql: string): Tokenized {
  const tokens: Token[] = [];
  let i = 0;
  const n = sql.length;
  while (i < n) {
    const ch = sql[i]!;
    if (/\s/.test(ch)) {
      i++;
    } else if (ch === "-" && sql[i + 1] === "-") {
      const end = sql.indexOf("\n", i);
      if (end < 0) return { tokens, inLiteral: true };
      i = end + 1;
    } else if (ch === "/" && sql[i + 1] === "*") {
      const end = sql.indexOf("*/", i + 2);
      if (end < 0) return { tokens, inLiteral: true };
      i = end + 2;
    } else if (ch === "'" || ((ch === "N" || ch === "n") && sql[i + 1] === "'")) {
      // String literal; '' is an escaped quote.
      let j = ch === "'" ? i + 1 : i + 2;
      for (;;) {
        const q = sql.indexOf("'", j);
        if (q < 0) return { tokens, inLiteral: true };
        if (sql[q + 1] === "'") {
          j = q + 2;
          continue;
        }
        i = q + 1;
        break;
      }
      tokens.push({ kind: "other" });
    } else if (ch === "[") {
      // Bracketed identifier; ]] is an escaped bracket. An unterminated one is a partial word.
      let j = i + 1;
      let text = "";
      let closed = false;
      while (j < n) {
        if (sql[j] === "]") {
          if (sql[j + 1] === "]") {
            text += "]";
            j += 2;
            continue;
          }
          closed = true;
          j++;
          break;
        }
        text += sql[j];
        j++;
      }
      tokens.push({ kind: "word", text, quoted: true });
      i = closed ? j : n;
    } else if (ch === '"') {
      const end = sql.indexOf('"', i + 1);
      tokens.push({ kind: "word", text: sql.slice(i + 1, end < 0 ? n : end), quoted: true });
      i = end < 0 ? n : end + 1;
    } else if (identStart.test(ch)) {
      let j = i + 1;
      while (j < n && identPart.test(sql[j]!)) j++;
      tokens.push({ kind: "word", text: sql.slice(i, j), quoted: false });
      i = j;
    } else if (/[0-9]/.test(ch)) {
      let j = i + 1;
      while (j < n && /[0-9.eE]/.test(sql[j]!)) j++;
      tokens.push({ kind: "other" });
      i = j;
    } else {
      tokens.push(
        ch === "."
          ? { kind: "dot" }
          : ch === ","
            ? { kind: "comma" }
            : ch === "("
              ? { kind: "open" }
              : ch === ")"
                ? { kind: "close" }
                : ch === ";"
                  ? { kind: "semicolon" }
                  : { kind: "other" },
      );
      i++;
    }
  }
  return { tokens, inLiteral: false };
}

const isWord = (t: Token | undefined, ...words: string[]): boolean =>
  t?.kind === "word" && !t.quoted && words.includes(t.text.toUpperCase());

/** Keywords after which a table or view name is expected. */
const objectKeywords = ["FROM", "JOIN", "UPDATE", "INTO", "APPLY", "TABLE"];

/** Keywords that start a clause; used to know whether a comma is inside a FROM list. */
const clauseKeywords = new Set([
  "SELECT",
  "FROM",
  "WHERE",
  "GROUP",
  "ORDER",
  "HAVING",
  "ON",
  "SET",
  "VALUES",
  "UNION",
  "EXCEPT",
  "INTERSECT",
  "OUTPUT",
  "WITH",
]);

export type CompletionContext =
  /** Inside a string or comment: no suggestions. */
  | { kind: "none" }
  /** After "a." or "a.b.": the parts before the cursor's word. */
  | { kind: "member"; qualifier: string[] }
  /** Where a table or view name goes. */
  | { kind: "object" }
  /** Anywhere else: keywords, functions, columns of referenced tables. */
  | { kind: "general" };

/**
 * Classifies the cursor position. textBefore is the statement text up to the cursor; a word
 * being typed at the end is ignored (it is what the suggestion replaces).
 */
export function detectContext(textBefore: string): CompletionContext {
  const { tokens, inLiteral } = tokenize(textBefore);
  if (inLiteral) return { kind: "none" };

  let end = tokens.length;
  // Drop the partial word being typed (only when the text ends inside it).
  if (end > 0 && tokens[end - 1]!.kind === "word" && /[\p{L}\p{N}_@#$\]"]$/u.test(textBefore)) end--;
  // An unterminated "[Ord" is also a partial word.
  if (end > 0 && tokens[end - 1]!.kind === "word" && /\[[^\]]*$/.test(textBefore)) end = Math.min(end, tokens.length - 1);

  const last = tokens[end - 1];
  if (last?.kind === "dot") {
    const qualifier: string[] = [];
    let k = end - 1;
    while (k > 0 && tokens[k]!.kind === "dot" && tokens[k - 1]!.kind === "word") {
      qualifier.unshift((tokens[k - 1] as { text: string }).text);
      k -= 2;
    }
    if (qualifier.length > 0) return { kind: "member", qualifier };
  }

  if (isWord(last, ...objectKeywords)) return { kind: "object" };

  // "FROM a, |" or "FROM a AS x, |": a comma whose nearest clause keyword is FROM.
  if (last?.kind === "comma") {
    let depth = 0;
    for (let k = end - 2; k >= 0; k--) {
      const t = tokens[k]!;
      if (t.kind === "close") depth++;
      else if (t.kind === "open") {
        if (depth === 0) break;
        depth--;
      } else if (depth === 0 && t.kind === "word" && !t.quoted) {
        const w = t.text.toUpperCase();
        if (w === "FROM") return { kind: "object" };
        if (clauseKeywords.has(w) || w === "JOIN") break;
      }
    }
  }
  return { kind: "general" };
}

/** A table or view the statement references. */
export interface TableRef {
  database: string | null;
  schema: string | null;
  name: string;
  alias: string | null;
}

/** Words that can follow a table name but are not aliases. */
const notAliases = new Set([
  "WHERE",
  "JOIN",
  "INNER",
  "LEFT",
  "RIGHT",
  "FULL",
  "CROSS",
  "OUTER",
  "ON",
  "GROUP",
  "ORDER",
  "HAVING",
  "UNION",
  "EXCEPT",
  "INTERSECT",
  "SET",
  "VALUES",
  "OUTPUT",
  "WITH",
  "OPTION",
  "FOR",
  "SELECT",
  "AS",
  "APPLY",
  "PIVOT",
  "UNPIVOT",
  "TABLESAMPLE",
  "GO",
  "DEFAULT",
  "WHEN",
  "THEN",
  "END",
]);

/** Finds table references (FROM/JOIN/UPDATE/INTO and comma lists) and their aliases. */
export function findTableRefs(sql: string): TableRef[] {
  const { tokens } = tokenize(sql);
  const refs: TableRef[] = [];
  const readRef = (start: number, keyword: string): number => {
    // name(.name){0,2}
    const parts: string[] = [];
    let k = start;
    while (tokens[k]?.kind === "word") {
      parts.push((tokens[k] as { text: string }).text);
      if (tokens[k + 1]?.kind === "dot" && tokens[k + 2]?.kind === "word") k += 2;
      else break;
    }
    if (parts.length === 0 || parts.length > 3) return start;
    k++;
    // Table hints, before or after the alias: WITH (NOLOCK)
    const skipHint = () => {
      if (!isWord(tokens[k], "WITH") || tokens[k + 1]?.kind !== "open") return;
      let depth = 0;
      for (k = k + 1; k < tokens.length; k++) {
        if (tokens[k]!.kind === "open") depth++;
        else if (tokens[k]!.kind === "close" && --depth === 0) {
          k++;
          break;
        }
      }
    };
    skipHint();
    let alias: string | null = null;
    if (isWord(tokens[k], "AS") && tokens[k + 1]?.kind === "word") {
      alias = (tokens[k + 1] as { text: string }).text;
      k += 2;
    } else if (tokens[k]?.kind === "word") {
      const t = tokens[k] as { text: string; quoted: boolean };
      if (t.quoted || !notAliases.has(t.text.toUpperCase())) {
        alias = t.text;
        k++;
      }
    }
    skipHint();
    const name = parts[parts.length - 1]!;
    // A table-valued function (FROM dbo.fn(...)) or a variable is not a table we can complete.
    // After INTO, "(" starts the column list instead.
    const call = keyword !== "INTO" && tokens[start + parts.length * 2 - 1]?.kind === "open";
    if (call || name.startsWith("@")) return k;
    refs.push({
      database: parts.length === 3 ? parts[0]! : null,
      schema: parts.length >= 2 ? parts[parts.length - 2]! : null,
      name,
      alias,
    });
    return k;
  };

  for (let i = 0; i < tokens.length; i++) {
    if (!isWord(tokens[i], "FROM", "JOIN", "UPDATE", "INTO", "APPLY")) continue;
    // DELETE FROM x / INSERT INTO x / UPDATE x: same shape.
    const keyword = (tokens[i] as { text: string }).text.toUpperCase();
    let k = readRef(i + 1, keyword);
    // Comma list in FROM: FROM a, b AS x, c
    while (keyword === "FROM" && tokens[k]?.kind === "comma") {
      const next = readRef(k + 1, keyword);
      if (next === k + 1) break;
      k = next;
    }
  }
  return refs;
}

/** The statement around the cursor: text after the last ";" or GO line before it, to the next one after. */
export function statementAround(text: string, offset: number): { text: string; start: number } {
  const before = text.slice(0, offset);
  const sep = /;|^\s*GO\s*$/gim;
  let start = 0;
  for (let m = sep.exec(before); m; m = sep.exec(before)) start = m.index + m[0].length;
  sep.lastIndex = offset;
  const next = sep.exec(text);
  return { text: text.slice(start, next ? next.index : text.length), start };
}

/** T-SQL reserved keywords: identifiers spelled like these must be quoted. */
const reserved = new Set(
  `ADD ALL ALTER AND ANY AS ASC AUTHORIZATION BACKUP BEGIN BETWEEN BREAK BROWSE BULK BY CASCADE CASE CHECK
CHECKPOINT CLOSE CLUSTERED COALESCE COLLATE COLUMN COMMIT COMPUTE CONSTRAINT CONTAINS CONTAINSTABLE CONTINUE
CONVERT CREATE CROSS CURRENT CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER CURSOR DATABASE DBCC
DEALLOCATE DECLARE DEFAULT DELETE DENY DESC DISK DISTINCT DISTRIBUTED DOUBLE DROP DUMP ELSE END ERRLVL ESCAPE
EXCEPT EXEC EXECUTE EXISTS EXIT EXTERNAL FETCH FILE FILLFACTOR FOR FOREIGN FREETEXT FREETEXTTABLE FROM FULL
FUNCTION GOTO GRANT GROUP HAVING HOLDLOCK IDENTITY IDENTITY_INSERT IDENTITYCOL IF IN INDEX INNER INSERT INTERSECT
INTO IS JOIN KEY KILL LEFT LIKE LINENO LOAD MERGE NATIONAL NOCHECK NONCLUSTERED NOT NULL NULLIF OF OFF OFFSETS
ON OPEN OPENDATASOURCE OPENQUERY OPENROWSET OPENXML OPTION OR ORDER OUTER OVER PERCENT PIVOT PLAN PRECISION
PRIMARY PRINT PROC PROCEDURE PUBLIC RAISERROR READ READTEXT RECONFIGURE REFERENCES REPLICATION RESTORE RESTRICT
RETURN REVERT REVOKE RIGHT ROLLBACK ROWCOUNT ROWGUIDCOL RULE SAVE SCHEMA SECURITYAUDIT SELECT
SEMANTICKEYPHRASETABLE SEMANTICSIMILARITYDETAILSTABLE SEMANTICSIMILARITYTABLE SESSION_USER SET SETUSER SHUTDOWN
SOME STATISTICS SYSTEM_USER TABLE TABLESAMPLE TEXTSIZE THEN TO TOP TRAN TRANSACTION TRIGGER TRUNCATE TRY_CONVERT
TSEQUAL UNION UNIQUE UNPIVOT UPDATE UPDATETEXT USE USER VALUES VARYING VIEW WAITFOR WHEN WHERE WHILE WITH
WITHIN WRITETEXT`.split(/\s+/),
);

/**
 * Brackets an identifier when it is not a regular identifier: it starts with a digit or
 * symbol, contains spaces or punctuation, or is a reserved keyword. Brackets are escaped as in
 * QUOTENAME (and Go's mssql.QuoteIdent).
 */
export function quoteIfNeeded(name: string): string {
  const regular = /^[\p{L}_][\p{L}\p{N}_@$#]*$/u.test(name) && !reserved.has(name.toUpperCase());
  return regular ? name : `[${name.replaceAll("]", "]]")}]`;
}

/** Case-insensitive identifier comparison, as with SQL Server's default collations. */
export const sameName = (a: string | null | undefined, b: string | null | undefined): boolean =>
  a != null && b != null && a.localeCompare(b, undefined, { sensitivity: "accent" }) === 0;
