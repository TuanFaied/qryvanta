import { useEditorStore } from "@/stores/editor-store";
import { dbKey, objectKey, useSchemaStore } from "@/stores/schema-store";
import { monaco, SQL_LANGUAGE } from "../monaco";
import { statementAround } from "./context";
import { computeSuggestions, type MetadataSource, type SuggestionKind } from "./suggestions";

/** Editor models are named inmemory://qryvanta/<tabId>.sql (see SqlEditor). */
export function tabIdOf(uri: { path: string }): string | null {
  const m = /^\/(.+)\.sql$/.exec(uri.path);
  return m ? m[1]! : null;
}

/** Metadata for one connection, read from the schema store's cache. */
export function schemaSource(connectionId: string): MetadataSource {
  const s = useSchemaStore.getState;
  return {
    objects: (db) => {
      const l = s().objects[dbKey(connectionId, db)];
      return l?.status === "ready" ? l.data : undefined;
    },
    columns: (db, schema, name) => {
      const l = s().columns[objectKey(connectionId, db, schema, name)];
      return l?.status === "ready" ? l.data : undefined;
    },
    loadObjects: (db) => s().loadObjects(connectionId, db),
    loadColumns: (db, schema, name) => s().loadColumns(connectionId, db, schema, name),
  };
}

const kinds: Record<SuggestionKind, monaco.languages.CompletionItemKind> = {
  table: monaco.languages.CompletionItemKind.Struct,
  view: monaco.languages.CompletionItemKind.Interface,
  function: monaco.languages.CompletionItemKind.Function,
  column: monaco.languages.CompletionItemKind.Field,
  alias: monaco.languages.CompletionItemKind.Variable,
  keyword: monaco.languages.CompletionItemKind.Keyword,
  builtin: monaco.languages.CompletionItemKind.Function,
};

let registered = false;

/** Registers the schema-aware completion provider for SQL models, once. */
export function registerSqlCompletion(): void {
  if (registered) return;
  registered = true;
  monaco.languages.registerCompletionItemProvider(SQL_LANGUAGE, {
    triggerCharacters: ["."],
    provideCompletionItems: async (model, position, _context, token) => {
      const tabId = tabIdOf(model.uri);
      const tab = useEditorStore.getState().tabs.find((t) => t.id === tabId);
      const connectionId = tab?.connectionId ?? null;

      const text = model.getValue();
      const offset = model.getOffsetAt(position);
      const statement = statementAround(text, offset);
      const result = await computeSuggestions({
        textBefore: statement.text.slice(0, offset - statement.start),
        statement: statement.text,
        database: connectionId ? tab?.database ?? null : null,
        source: connectionId ? schemaSource(connectionId) : null,
      });
      if (token.isCancellationRequested) return { suggestions: [] };

      const word = model.getWordUntilPosition(position);
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
      return {
        incomplete: result.incomplete,
        suggestions: result.items.map((item) => ({
          label: { label: item.label, description: item.detail },
          kind: kinds[item.kind],
          insertText: item.insertText,
          insertTextRules: item.snippet ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
          detail: item.detail,
          documentation: item.documentation,
          sortText: item.sortText,
          range,
        })),
      };
    },
  });
}
