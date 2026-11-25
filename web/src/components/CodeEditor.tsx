import { indentWithTab } from "@codemirror/commands";
import { Compartment, EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorView, keymap } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { useEffect, useRef } from "react";
import { languageFor } from "./codeLanguages";

// A small code editor (CodeMirror 6). This module is loaded only when a file
// is opened (React.lazy), and each language only when a file needs it.

// Fit the editor into the panel's dialog: fill the height, IDE-like font.
const layout = EditorView.theme({
  "&": { height: "100%", fontSize: "13px" },
  ".cm-scroller": { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace", lineHeight: "1.55" },
  ".cm-gutters": { borderRight: "1px solid #2c313a" },
});

export default function CodeEditor({
  value,
  filename,
  readOnly,
  onChange,
  onSave,
  onCursor,
  autoFocus = true,
}: {
  value: string;
  filename: string;
  readOnly: boolean;
  onChange: (text: string) => void;
  onSave: () => void;
  onCursor?: (line: number, col: number) => void;
  /** Put the cursor in the editor when it opens (off for read-only views on a page). */
  autoFocus?: boolean;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const language = useRef(new Compartment());
  const editable = useRef(new Compartment());
  // Callbacks change on every render; the editor reads the latest via refs.
  const cb = useRef({ onChange, onSave, onCursor });
  cb.current = { onChange, onSave, onCursor };

  // Create the editor once per file.
  useEffect(() => {
    const v = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          keymap.of([
            indentWithTab,
            { key: "Mod-s", preventDefault: true, run: () => (cb.current.onSave(), true) },
          ]),
          oneDark,
          layout,
          language.current.of([]),
          editable.current.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) cb.current.onChange(u.state.doc.toString());
            if (u.docChanged || u.selectionSet) {
              const head = u.state.selection.main.head;
              const line = u.state.doc.lineAt(head);
              cb.current.onCursor?.(line.number, head - line.from + 1);
            }
          }),
        ],
      }),
    });
    view.current = v;
    if (autoFocus) v.focus();
    let cancelled = false;
    languageFor(filename)
      ?.load()
      .then((ext) => !cancelled && v.dispatch({ effects: language.current.reconfigure(ext) }))
      .catch(() => {}); // plain text is fine
    return () => {
      cancelled = true;
      v.destroy();
      view.current = null;
    };
    // value is only the starting text; later edits come from the editor itself.
  }, [filename]);

  // The file was (re)loaded from the server: replace the text.
  useEffect(() => {
    const v = view.current;
    if (v && v.state.doc.toString() !== value) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
    }
    // Only when the loaded text changes, not on every keystroke.
  }, [value]);

  useEffect(() => {
    view.current?.dispatch({
      effects: editable.current.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
    });
  }, [readOnly]);

  return <div ref={host} className="h-full min-h-0 overflow-hidden" />;
}
