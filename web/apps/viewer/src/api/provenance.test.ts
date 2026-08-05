/**
 * The join between the platform's trace and the console's chain.
 *
 * This is the seam where invented data would hide: a mapper is exactly the
 * place a missing field quietly becomes a plausible default. These assertions
 * pin that an absent link stays absent and an unverified input stays
 * unverified, so the chain view can draw the difference.
 */

import { describe, expect, it } from "vitest";
import { chainCompleteness } from "@blackstart/schema";
import type { traceResponse } from "@blackstart/api-client";
import { chainOfTrace } from "./provenance";

const dataset = (id: string, role: string, hash: string) => ({
  role,
  dataset: {
    id,
    contentHash: hash,
    parserVersion: "1.2.0",
    retrievedAt: "2026-08-02T18:14:00Z",
    sizeBytes: 4096,
    sourceId: "src_hrrr",
    url: `https://example.test/${id}`,
  },
});

const trace = (over: Partial<traceResponse> = {}): traceResponse =>
  ({
    inputs: [dataset("ds_case", "network-case", "a".repeat(64))],
    producer: {
      id: "mdl_01k3",
      kind: "solver",
      name: "bs-contingency",
      version: "0.3.1",
      codeCommit: "41416a7",
      parameters: {},
      recordedAt: "2026-08-02T18:00:00Z",
    },
    run: {
      id: "run_01k3",
      kind: "contingency",
      modelVersionId: "mdl_01k3",
      resultHash: "b".repeat(64),
      seed: "",
      startedAt: "2026-08-02T22:00:00Z",
      finishedAt: "2026-08-02T22:04:00Z",
    },
    reproducible: true,
    ...over,
  }) as traceResponse;

describe("chainOfTrace", () => {
  it("builds a complete chain from a full trace", () => {
    const chain = chainOfTrace(trace());
    expect(chainCompleteness(chain).complete).toBe(true);
    expect(chain.map((l) => l.kind)).toEqual(["network", "model", "run", "commit"]);
  });

  it("keeps every input, because a run consumes several", () => {
    const chain = chainOfTrace(
      trace({
        inputs: [
          dataset("ds_case", "network-case", "a".repeat(64)),
          dataset("ds_hrrr", "weather", "c".repeat(64)),
          dataset("ds_dem", "terrain", "d".repeat(64)),
        ],
      }),
    );

    expect(chain.filter((l) => l.kind === "dataset")).toHaveLength(2);
    expect(chain.filter((l) => l.kind === "network")).toHaveLength(1);
  });

  it("marks an unhashed input present but unverified rather than dropping it", () => {
    // A dataset nobody hashed is a real gap in the record. Omitting it would
    // make the chain look complete; defaulting a hash would be a lie.
    const chain = chainOfTrace(trace({ inputs: [dataset("ds_case", "network-case", "")] }));

    const link = chain.find((l) => l.ref === "ds_case");
    expect(link?.verified).toBe(false);
    expect(link?.digest).toBeUndefined();
    expect(chainCompleteness(chain).unverified).toContain("network");
  });

  it("carries the platform's reproducibility verdict onto the run and commit", () => {
    const chain = chainOfTrace(trace({ reproducible: false }));

    expect(chain.find((l) => l.kind === "run")?.verified).toBe(false);
    expect(chain.find((l) => l.kind === "commit")?.verified).toBe(false);
    expect(chainCompleteness(chain).complete).toBe(false);
  });

  it("omits a link the trace does not carry instead of inventing one", () => {
    const chain = chainOfTrace(
      trace({
        producer: { ...trace().producer, id: "", codeCommit: "" },
      }),
    );

    expect(chain.find((l) => l.kind === "model")).toBeUndefined();
    expect(chain.find((l) => l.kind === "commit")).toBeUndefined();

    // And the gap is reported rather than hidden.
    expect(chainCompleteness(chain).missing).toContain("commit");
  });

  it("degrades a second network-role input rather than discarding it", () => {
    const chain = chainOfTrace(
      trace({
        inputs: [
          dataset("ds_a", "network-case", "a".repeat(64)),
          dataset("ds_b", "network-case", "e".repeat(64)),
        ],
      }),
    );

    // two inputs, plus model, run and commit
    expect(chain).toHaveLength(5);
    expect(chain.filter((l) => l.kind === "network")).toHaveLength(1);
    expect(chain.find((l) => l.ref === "ds_b")?.kind).toBe("dataset");
  });
});
