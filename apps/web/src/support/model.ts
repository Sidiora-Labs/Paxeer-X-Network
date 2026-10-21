export function support(): Readonly<{
  operations: readonly ["create", "list", "reply", "read", "status", "feedback"];
  reportContext: readonly ["trace_id"];
}> {
  const operations = ["create", "list", "reply", "read", "status", "feedback"] as const;
  const reportContext = ["trace_id"] as const;
  return Object.freeze({ operations, reportContext });
}

export function human_web_support() {
  return support();
}
