import type { NarrativeHit } from "../types.js";

/**
 * Pluggable Markdown narrative retrieval (QMD or fallback).
 */
export type NarrativeQuery = {
  query: string;
  k: number;
  kinds?: string[];
  packages?: string[];
  queryVariants?: string[];
};

export interface NarrativeRetrieval {
  search(q: NarrativeQuery): Promise<NarrativeHit[]>;
}

export type { NarrativeHit };
