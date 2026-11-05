/** Splits a query at unquoted whitespace, keeping each token as written. */
function tokens(query: string): string[] {
  return query.match(/(?:[^\s"]|"[^"]*"?)+/g) ?? [];
}

/** The query term for a group. The server has no escape for a quote, so quotes in a name are dropped. */
export function groupTerm(name: string): string {
  return `group:"${name.replaceAll('"', '')}"`;
}

export function hasGroupTerm(query: string, name: string): boolean {
  const term = groupTerm(name).toLowerCase();
  return tokens(query).some((t) => t.toLowerCase() === term);
}

/** Adds `group:"Name"` to the query, or removes it when it is already there. */
export function toggleGroupTerm(query: string, name: string): string {
  const term = groupTerm(name);
  if (!hasGroupTerm(query, name)) return query.trim() === '' ? term : `${query.trimEnd()} ${term}`;
  const lower = term.toLowerCase();
  return tokens(query)
    .filter((t) => t.toLowerCase() !== lower)
    .join(' ');
}
