import type { Extension } from "@codemirror/state";

// File name → editor language. Kept apart from CodeEditor so the file list can
// show the language without loading the editor; every language is its own
// small chunk, loaded only when a file needs it.

export type Lang = { name: string; load: () => Promise<Extension> };

// Older CodeMirror 5 modes (shell, nginx, ini), wrapped for CodeMirror 6.
const legacy = async (mode: Promise<{ [k: string]: unknown }>, key: string): Promise<Extension> => {
  const { StreamLanguage } = await import("@codemirror/language");
  return StreamLanguage.define((await mode)[key] as Parameters<typeof StreamLanguage.define>[0]);
};

const php: Lang = { name: "PHP", load: async () => (await import("@codemirror/lang-php")).php() };
const js: Lang = { name: "JavaScript", load: async () => (await import("@codemirror/lang-javascript")).javascript() };
const ts: Lang = { name: "TypeScript", load: async () => (await import("@codemirror/lang-javascript")).javascript({ typescript: true }) };
const jsx: Lang = { name: "JSX", load: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true, typescript: true }) };
const css: Lang = { name: "CSS", load: async () => (await import("@codemirror/lang-css")).css() };
const html: Lang = { name: "HTML", load: async () => (await import("@codemirror/lang-html")).html() };
const json: Lang = { name: "JSON", load: async () => (await import("@codemirror/lang-json")).json() };
const xml: Lang = { name: "XML", load: async () => (await import("@codemirror/lang-xml")).xml() };
const md: Lang = { name: "Markdown", load: async () => (await import("@codemirror/lang-markdown")).markdown() };
const sql: Lang = { name: "SQL", load: async () => (await import("@codemirror/lang-sql")).sql() };
const yaml: Lang = { name: "YAML", load: async () => (await import("@codemirror/lang-yaml")).yaml() };
const shell: Lang = { name: "Shell", load: () => legacy(import("@codemirror/legacy-modes/mode/shell"), "shell") };
const nginx: Lang = { name: "nginx", load: () => legacy(import("@codemirror/legacy-modes/mode/nginx"), "nginx") };
const ini: Lang = { name: "INI", load: () => legacy(import("@codemirror/legacy-modes/mode/properties"), "properties") };

const byExt: Record<string, Lang> = {
  php, phtml: php, inc: php,
  js, mjs: js, cjs: js, ts, jsx, tsx: jsx,
  css, scss: css, less: css,
  html, htm: html, twig: html, vue: html, svg: xml,
  json, xml, md: md, markdown: md, sql,
  yml: yaml, yaml, sh: shell, bash: shell,
  conf: nginx, ini, env: ini, htaccess: ini,
};

/** The language for a file name ("" when it's plain text). */
export function languageFor(filename: string): Lang | null {
  const base = filename.split("/").pop()?.toLowerCase() ?? "";
  if (base === ".htaccess" || base === ".env" || base.startsWith(".env.")) return ini;
  const ext = base.includes(".") ? base.split(".").pop()! : "";
  return byExt[ext] ?? null;
}

