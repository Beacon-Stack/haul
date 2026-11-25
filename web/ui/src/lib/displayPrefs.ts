// Display preferences kept in this browser, like the theme: they change
// how Haul looks to you, not how it runs.

import { useSyncExternalStore } from "react";

const KEY_PIECE_ANIMATION = "haul-piece-animation";
const listeners = new Set<() => void>();

function readPieceAnimation(): boolean {
  try {
    return localStorage.getItem(KEY_PIECE_ANIMATION) !== "off";
  } catch {
    return true;
  }
}

export function setPieceAnimation(on: boolean): void {
  try {
    localStorage.setItem(KEY_PIECE_ANIMATION, on ? "on" : "off");
  } catch {
    // Storage blocked: the choice lasts until the page reloads.
  }
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// usePieceAnimation is whether the piece bar animates. On by default.
export function usePieceAnimation(): boolean {
  return useSyncExternalStore(subscribe, readPieceAnimation);
}
