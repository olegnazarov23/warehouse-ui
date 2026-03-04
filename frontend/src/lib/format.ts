/** Format bytes as human readable */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

/** Format USD cost */
export function formatCost(usd: number): string {
  if (usd < 0.01) return "<$0.01";
  return "$" + usd.toFixed(2);
}

/** Format duration in ms */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 60000)}m ${Math.round((ms % 60000) / 1000)}s`;
}

/** Format a large number with commas */
export function formatNumber(n: number): string {
  return n.toLocaleString();
}

/** Truncate text */
export function truncate(text: string, max: number): string {
  return text.length > max ? text.slice(0, max) + "..." : text;
}
