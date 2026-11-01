// Parse every ```mermaid block in the given Markdown files with Mermaid's own
// parser. Exits non-zero if any diagram is invalid.
// Usage: node check.mjs README.md docs/*.md
import fs from 'node:fs';
import { JSDOM } from 'jsdom';

// Mermaid expects a browser-like environment; jsdom is enough for parsing.
const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, 'navigator', { value: dom.window.navigator, configurable: true });

const mermaid = (await import('mermaid')).default;
mermaid.initialize({ startOnLoad: false });

const files = process.argv.slice(2);
if (files.length === 0) {
  console.error('usage: node check.mjs <markdown files...>');
  process.exit(2);
}

let total = 0;
let failed = 0;
for (const file of files) {
  const text = fs.readFileSync(file, 'utf8');
  const blocks = [...text.matchAll(/```mermaid\n([\s\S]*?)```/g)];
  for (const [index, match] of blocks.entries()) {
    total += 1;
    const first = match[1].trim().split('\n')[0];
    try {
      await mermaid.parse(match[1]);
      console.log(`ok    ${file} #${index + 1} (${first})`);
    } catch (error) {
      failed += 1;
      const detail = String(error.message || error).split('\n').slice(0, 4).join('\n        ');
      console.log(`FAIL  ${file} #${index + 1} (${first})\n        ${detail}`);
    }
  }
}
console.log(`${total} diagram(s) checked, ${failed} failed`);
process.exit(failed ? 1 : 0);
