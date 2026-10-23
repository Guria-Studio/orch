// Syntax highlighting for fenced code in rendered markdown. Loaded on demand
// (components/Markdown.tsx imports it only when a block names a language), so
// pages without code never fetch it. highlight.js's core plus the languages
// orch's task descriptions use; a fence naming another language stays plain.
import hljs from "highlight.js/lib/core"
import bash from "highlight.js/lib/languages/bash"
import diff from "highlight.js/lib/languages/diff"
import go from "highlight.js/lib/languages/go"
import javascript from "highlight.js/lib/languages/javascript"
import json from "highlight.js/lib/languages/json"
import python from "highlight.js/lib/languages/python"
import shell from "highlight.js/lib/languages/shell"
import sql from "highlight.js/lib/languages/sql"
import typescript from "highlight.js/lib/languages/typescript"
import yaml from "highlight.js/lib/languages/yaml"

for (const [name, lang] of Object.entries({ bash, diff, go, javascript, json, python, shell, sql, typescript, yaml })) {
  hljs.registerLanguage(name, lang)
}

/** Colors one `<code class="language-x">`; an unknown language is left as is. */
export function highlight(code: HTMLElement, lang: string) {
  if (hljs.getLanguage(lang)) hljs.highlightElement(code)
}
