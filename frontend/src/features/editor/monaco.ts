/**
 * Monaco, bundled locally and trimmed to what a T-SQL editor needs.
 *
 * `monaco-editor`'s main entry registers ~90 languages and the TypeScript, CSS, HTML and JSON
 * language services. This module loads only the editor core, the editing contributions
 * listed below, and the SQL grammar (Monaco's "sql" language is based on T-SQL). Nothing is
 * fetched from a CDN, and the one web worker Monaco needs is a bundled file served from the
 * app itself, so the production Content-Security-Policy (script-src/default-src 'self')
 * covers it without changes.
 *
 * Only SqlEditor imports this module, and SqlEditor is loaded with React.lazy, so Monaco is
 * a separate chunk that is downloaded when the first editor tab opens, not on first paint.
 */
import * as monaco from "monaco-editor/editor/editor.api";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";

// Editing features (the list mirrors monaco-editor's editor.main, minus the contributions
// that only make sense with a language service, such as rename and go-to-definition).
import "monaco-editor/editor/browser/coreCommands";
import "monaco-editor/editor/browser/widget/codeEditor/codeEditorWidget";
import "monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching";
import "monaco-editor/editor/contrib/caretOperations/browser/caretOperations";
import "monaco-editor/editor/contrib/caretOperations/browser/transpose";
import "monaco-editor/editor/contrib/clipboard/browser/clipboard";
import "monaco-editor/editor/contrib/comment/browser/comment";
import "monaco-editor/editor/contrib/contextmenu/browser/contextmenu";
import "monaco-editor/editor/contrib/cursorUndo/browser/cursorUndo";
import "monaco-editor/editor/contrib/dnd/browser/dnd";
import "monaco-editor/editor/contrib/find/browser/findController";
import "monaco-editor/features/find/register";
import "monaco-editor/editor/contrib/folding/browser/folding";
import "monaco-editor/editor/contrib/gotoError/browser/gotoError";
import "monaco-editor/editor/contrib/gotoError/browser/markerSelectionStatus";
import "monaco-editor/editor/contrib/hover/browser/hoverContribution";
import "monaco-editor/editor/contrib/indentation/browser/indentation";
import "monaco-editor/editor/contrib/inlineProgress/browser/inlineProgress";
import "monaco-editor/editor/contrib/insertFinalNewLine/browser/insertFinalNewLine";
import "monaco-editor/editor/contrib/lineSelection/browser/lineSelection";
import "monaco-editor/editor/contrib/linesOperations/browser/linesOperations";
import "monaco-editor/editor/contrib/multicursor/browser/multicursor";
import "monaco-editor/editor/contrib/smartSelect/browser/smartSelect";
import "monaco-editor/editor/contrib/snippet/browser/snippetController2";
import "monaco-editor/editor/contrib/stickyScroll/browser/stickyScrollContribution";
import "monaco-editor/editor/contrib/suggest/browser/suggestController";
import "monaco-editor/editor/contrib/toggleTabFocusMode/browser/toggleTabFocusMode";
import "monaco-editor/editor/contrib/tokenization/browser/tokenization";
import "monaco-editor/editor/contrib/wordHighlighter/browser/wordHighlighter";
import "monaco-editor/editor/contrib/wordOperations/browser/wordOperations";
import "monaco-editor/editor/contrib/wordPartOperations/browser/wordPartOperations";
import "monaco-editor/editor/standalone/browser/quickAccess/standaloneCommandsQuickAccess";
import "monaco-editor/editor/standalone/browser/quickAccess/standaloneHelpQuickAccess";
import "monaco-editor/editor/common/standaloneStrings";
import "monaco-esm/vs/base/browser/ui/codicons/codicon/codicon.css";
import "monaco-esm/vs/base/browser/ui/codicons/codicon/codicon-modifiers.css";

// T-SQL syntax highlighting.
import "monaco-editor/languages/definitions/sql/register";

self.MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
};

export const SQL_LANGUAGE = "sql";

// Colours match the app's design tokens (src/index.css) so the editor does not look like
// a foreign widget. Monaco needs hex values, so they are approximations of the oklch tokens.
monaco.editor.defineTheme("qryvanta-dark", {
  base: "vs-dark",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#101115",
    "editorGutter.background": "#101115",
    "editor.lineHighlightBackground": "#181a20",
    "editor.lineHighlightBorder": "#00000000",
    "editorLineNumber.foreground": "#5b5e69",
    "editorLineNumber.activeForeground": "#b9bcc6",
    "editorCursor.foreground": "#4fd1c5",
    "editorWidget.background": "#1a1b21",
    "editorWidget.border": "#2a2c33",
    "focusBorder": "#4fd1c580",
  },
});

monaco.editor.defineTheme("qryvanta-light", {
  base: "vs",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#ffffff",
    "editorGutter.background": "#ffffff",
    "editor.lineHighlightBackground": "#f4f5f8",
    "editor.lineHighlightBorder": "#00000000",
    "editorLineNumber.foreground": "#9a9ca5",
    "editorLineNumber.activeForeground": "#3a3c44",
    "editorCursor.foreground": "#007a78",
    "editorWidget.border": "#dfe1e6",
    "focusBorder": "#007a7880",
  },
});

export { monaco };
