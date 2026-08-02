/**
 * Mapping measurement values onto the token names that carry their colour.
 *
 * Deliberately returns a Tailwind class rather than a hex. Components that
 * resolve their own hex would have to know which ground they are on, and the
 * whole point of the token layer is that they do not.
 */

import { riskBucket, type RiskBucket, MAX_ISLANDS, type IslandId } from "@blackstart/schema";

const RISK_TEXT = [
  "text-risk-0",
  "text-risk-1",
  "text-risk-2",
  "text-risk-3",
  "text-risk-4",
] as const;

const RISK_BG = ["bg-risk-0", "bg-risk-1", "bg-risk-2", "bg-risk-3", "bg-risk-4"] as const;

const ISLAND_BG = [
  "bg-island-1",
  "bg-island-2",
  "bg-island-3",
  "bg-island-4",
  "bg-island-5",
  "bg-island-6",
  "bg-island-7",
  "bg-island-8",
] as const;

const ISLAND_TEXT = [
  "text-island-1",
  "text-island-2",
  "text-island-3",
  "text-island-4",
  "text-island-5",
  "text-island-6",
  "text-island-7",
  "text-island-8",
] as const;

export function riskText(p: number): string {
  return RISK_TEXT[riskBucket(p)] ?? RISK_TEXT[0];
}

export function riskBg(p: number): string {
  return RISK_BG[riskBucket(p)] ?? RISK_BG[0];
}

export function bucketBg(bucket: RiskBucket): string {
  return RISK_BG[bucket] ?? RISK_BG[0];
}

export function islandBg(id: IslandId): string {
  return ISLAND_BG[id - 1] ?? ISLAND_BG[0];
}

export function islandText(id: IslandId): string {
  return ISLAND_TEXT[id - 1] ?? ISLAND_TEXT[0];
}

/**
 * The CSS variable behind an island, for the few places that need a real
 * colour: canvas strokes and SVG attributes, which cannot take a class.
 */
export function islandVar(id: IslandId): string {
  const clamped = Math.min(Math.max(id, 1), MAX_ISLANDS);
  return `var(--bs-island-${clamped})`;
}

export function riskVar(p: number): string {
  return `var(--bs-risk-${riskBucket(p)})`;
}

export const RISK_RAMP_VARS = [0, 1, 2, 3, 4].map((i) => `var(--bs-risk-${i})`);
