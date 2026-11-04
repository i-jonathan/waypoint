/** Splits a parse error into the line (if known) and the message without the file path or "line N:" prefix. */
export function describeParseError(e: { line?: number; message: string }): { line?: number; message: string } {
  const m = /^(?:.*?: )?line (\d+): (.*)$/s.exec(e.message);
  const line = e.line ?? (m ? Number(m[1]) : undefined);
  const message = m ? m[2]! : e.message;
  return line === undefined ? { message } : { line, message };
}
