// torrentFile.ts — pure helpers for the .torrent upload path in AddModal.
// Kept separate from the React component so the validation + encoding
// logic is unit-testable as plain functions when a test framework is added.

// Mirror the server-side cap in internal/api/v1/torrents.go. The server
// re-validates, but rejecting on the client avoids a wasted round-trip.
export const MAX_TORRENT_FILE_BYTES = 10 * 1024 * 1024;

export const TORRENT_DATA_URI_PREFIX = "data:application/x-bittorrent;base64,";

export type ValidateResult =
  | { ok: true }
  | { ok: false; error: string };

// validateTorrentFile checks the file's name and size before we read it.
// Cheap, sync, runs immediately on pick / drop. Magic-byte validation
// happens in readTorrentFileAsDataURI once the bytes are loaded.
export function validateTorrentFile(file: File): ValidateResult {
  const name = file.name.toLowerCase();
  if (!name.endsWith(".torrent")) {
    return { ok: false, error: "File must have a .torrent extension" };
  }
  if (file.size === 0) {
    return { ok: false, error: "File is empty" };
  }
  if (file.size > MAX_TORRENT_FILE_BYTES) {
    return { ok: false, error: `File exceeds ${formatBytes(MAX_TORRENT_FILE_BYTES)} limit` };
  }
  return { ok: true };
}

// readTorrentFileAsDataURI loads the file as bytes, verifies the bencoded
// magic byte ('d' / 0x64), and returns the full data URI ready to submit.
// Throws if the file isn't a bencoded torrent — caller should toast or
// show inline.
export async function readTorrentFileAsDataURI(file: File): Promise<string> {
  const buf = await file.arrayBuffer();
  const bytes = new Uint8Array(buf);
  if (bytes.length === 0) {
    throw new Error("File is empty");
  }
  // Bencoded dict starts with 'd' (0x64). Any other first byte means this
  // isn't a torrent file even if the extension says otherwise.
  if (bytes[0] !== 0x64) {
    throw new Error("File doesn't look like a torrent (bad magic byte)");
  }
  return TORRENT_DATA_URI_PREFIX + bytesToBase64(bytes);
}

// bytesToBase64 base64-encodes a Uint8Array in 32KB chunks. Calling
// btoa(String.fromCharCode(...arr)) directly hits a stack-size RangeError
// in V8 around 100KB — the chunked form is the standard workaround and
// handles multi-MB torrents without issue.
function bytesToBase64(bytes: Uint8Array): string {
  const chunkSize = 0x8000;
  let binary = "";
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode.apply(null, Array.from(chunk));
  }
  return btoa(binary);
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
