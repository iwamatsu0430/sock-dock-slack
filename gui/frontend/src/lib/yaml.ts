/**
 * Inserts a route snippet (already indented as a list item under `routes:`)
 * at the end of the routes block, keeping the rest of the file and its
 * comments untouched.
 */
export function insertRoute(content: string, snippet: string): string {
  const lines = content.replace(/\s+$/, "").split("\n");
  const start = lines.findIndex((l) => /^routes:\s*(\[\s*\])?\s*(#.*)?$/.test(l));
  if (start === -1) {
    return `${lines.join("\n")}\n\nroutes:\n${snippet}\n`;
  }
  // `routes: []` becomes a block list.
  lines[start] = "routes:";
  // The block ends at the next top-level key.
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^[A-Za-z_][\w-]*\s*:/.test(lines[i])) {
      end = i;
      break;
    }
  }
  // Keep trailing blank lines/comments that belong to the next section.
  let insertAt = end;
  while (insertAt > start + 1 && /^\s*$/.test(lines[insertAt - 1])) insertAt--;
  lines.splice(insertAt, 0, ...snippet.split("\n"));
  return lines.join("\n") + "\n";
}
