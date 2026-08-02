/**
 * The client's domain vocabulary.
 *
 * Every payload the viewer renders is parsed through a schema here, and every
 * component's props are derived from one with `z.infer`. There is deliberately
 * no hand-written interface mirroring a schema anywhere in the UI package —
 * two declarations of the same shape drift, and the one that drifts is always
 * the one without a validator behind it.
 */

export * from "./quantity";
export * from "./identity";
export * from "./temporal";
export * from "./provenance";
export * from "./risk";
export * from "./cascade";
export * from "./run";
export * from "./network";
export * from "./scene";
