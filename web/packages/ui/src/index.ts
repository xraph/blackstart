/**
 * The console's component inventory.
 *
 * Three layers, and the boundary between them is deliberate:
 *
 *  - `instrument` renders one measured thing. Every component here takes a
 *    schema type rather than a primitive, which is how the "no number without
 *    its unit" rule is enforced rather than remembered.
 *  - `shell` is the frame: it knows there is a stage and a rail, not what is
 *    in them.
 *  - `panels` compose instruments into the surfaces a screen is made of.
 *
 * Nothing here declares its own prop shape for domain data — every payload
 * type is inferred from `@blackstart/schema`. Two declarations of one shape
 * drift, and the one that drifts is always the one with no validator behind it.
 */

export { cn } from "./cn";
export { Field, Panel } from "./Panel";
export * from "./lib/palette";

export * from "./instrument/quantity";
export * from "./instrument/identity";
export * from "./instrument/risk";
export * from "./instrument/sparkline";
export * from "./instrument/status";
export * from "./instrument/provenance";

export * from "./shell/app-shell";
export * from "./shell/time-axis";
export * from "./shell/responsive";

export * from "./panels/contingency-list";
export * from "./panels/event-log";
export * from "./panels/layer-stack";
export * from "./panels/cascade";
export * from "./panels/runs-grid";
export * from "./panels/inspector";
