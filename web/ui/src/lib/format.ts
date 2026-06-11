import { formatBytes } from "@beacon-shared/utils";

export { formatBytes };

// formatSpeed renders a transfer rate. Zero renders as a real value
// ("0 B/s"), not a placeholder dash — rates are measurements, and the
// same datum must read identically on every page.
export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "0 B/s";
  return `${formatBytes(bytesPerSec)}/s`;
}
