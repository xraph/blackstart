/**
 * Contingency sweeps, from the platform.
 *
 * This screen shows what the platform actually serves today: N-1 screening
 * sweeps, ranked by worst branch loading. The reviewed design showed *scenario
 * runs* — expected unserved energy, a reproducibility verdict, both time axes
 * — and none of that exists yet: risk and cascade are phases 3 and 4, and the
 * API publishes no probability, no unserved energy and no cascade at all.
 *
 * Rendering the design's columns against this data would have meant inventing
 * the numbers. The schema refused it outright — ScenarioRun requires a
 * complete run to report an EUE, and a screening sweep has none — which is the
 * invariant doing its job. So the screen shows sweeps as sweeps, and the
 * scenario-run grid waits for scenario runs.
 *
 * Selection stays in the URL, so a comparison can be linked rather than
 * described.
 */

import { useNavigate } from "@tanstack/react-router";
import {
  useContingencyRunsList,
  useProvenanceRunsTrace,
  type SweepRun,
} from "@blackstart/api-client";
import { ProvenanceChainView, RAIL_BREAKPOINT, useMediaQuery } from "@blackstart/ui";

import { runsRoute } from "../router";
import { useApiClient } from "../api/provider";
import { chainOfTrace } from "../api/provenance";

export function RunsScreen(): React.JSX.Element {
  const search = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const client = useApiClient();
  const wide = useMediaQuery(RAIL_BREAKPOINT);

  const { data, isPending, isError, error } = useContingencyRunsList(client, undefined, 50);

  const runs = data?.runs ?? [];
  const selected = new Set(search.compare);

  const toggle = (id: string): void => {
    void navigate({
      search: (prev) => ({
        ...prev,
        compare: prev.compare.includes(id)
          ? prev.compare.filter((x) => x !== id)
          : [...prev.compare, id].slice(-2),
      }),
    });
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 overflow-x-auto border-b border-line bg-panel px-3">
        <span className="font-mono text-[10px] uppercase tracking-[0.13em] text-fg-3">
          Contingency sweeps
        </span>
        <span className="flex-1" />
        <span className="whitespace-nowrap font-mono text-[10px] tabular text-fg-3">
          {isPending ? "loading…" : `${runs.length} runs`}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {isError ? (
          <Notice tone="alarm" title="The platform is not reachable">
            {error.message}
            <br />
            Start the host with <code className="font-mono">./bin/blackstart</code>, or point the
            viewer elsewhere with <code className="font-mono">VITE_API_BASE_URL</code>.
          </Notice>
        ) : isPending ? (
          <Notice title="Loading sweeps">Asking the platform for recent N-1 runs.</Notice>
        ) : runs.length === 0 ? (
          <Notice title="No sweeps recorded">
            Run one with <code className="font-mono">bsctl contingency sweep</code>. The platform
            is reachable and has nothing to show.
          </Notice>
        ) : wide ? (
          <SweepTable runs={runs} selected={selected} onToggle={toggle} />
        ) : (
          <ul className="m-0 list-none p-0">
            {runs.map((r) => (
              <SweepCard
                key={r.id}
                run={r}
                selected={selected.has(r.id)}
                onToggle={() => toggle(r.id)}
              />
            ))}
          </ul>
        )}
      </div>

      <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-3 border-t border-line bg-panel px-3.5 py-2">
        <span className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">Selected</span>
        {selected.size === 0 ? (
          <span className="text-[11px] text-fg-3">Pick two sweeps to compare them.</span>
        ) : (
          <span className="font-mono text-[10px] tabular text-fg">
            {[...selected].join("  ·  ")}
          </span>
        )}
        <span className="flex-1" />
        <span className="max-w-[52ch] text-[11px] leading-snug text-fg-3">
          Expected unserved energy is not served yet — the platform models power flow and N-1
          screening, not risk or cascade.
        </span>
      </div>

      {/* One selection shows its provenance. Two is a comparison, and the
          chains of two runs side by side is a different panel than this. */}
      {selected.size === 1 && <SweepProvenance runId={[...selected][0] ?? ""} />}
    </div>
  );
}

/**
 * The provenance of one sweep, from the platform's own trace.
 *
 * Its own component because it fetches: a hook cannot be called conditionally,
 * and the trace is only wanted once a run is actually selected.
 */
function SweepProvenance({ runId }: { readonly runId: string }): React.JSX.Element {
  const client = useApiClient();
  const { data, isPending, isError, error } = useProvenanceRunsTrace(client, runId);

  return (
    <div className="max-h-64 shrink-0 overflow-y-auto border-t border-line bg-panel px-3.5 py-3">
      {isPending ? (
        <p className="m-0 font-mono text-[10px] text-fg-3">Tracing {runId}…</p>
      ) : isError ? (
        <p className="m-0 text-[11px] leading-snug text-alarm">
          No provenance for this run: {error.message}
        </p>
      ) : data === undefined ? null : (
        <ProvenanceChainView chain={chainOfTrace(data)} />
      )}
    </div>
  );
}

const TH =
  "sticky top-0 h-[30px] whitespace-nowrap border-b border-line-2 bg-panel px-2.5 text-left text-[9px] font-normal uppercase tracking-[0.1em] text-fg-3";
const TD = "touch-row h-8 whitespace-nowrap border-b border-line-t px-2.5 text-fg-2";

function SweepTable({
  runs,
  selected,
  onToggle,
}: {
  readonly runs: readonly SweepRun[];
  readonly selected: ReadonlySet<string>;
  readonly onToggle: (id: string) => void;
}): React.JSX.Element {
  return (
    <table className="w-full border-collapse font-mono text-[10.5px] tabular">
      <thead>
        <tr>
          <th className={`${TH} w-[26px]`}>
            <span className="sr-only">Select</span>
          </th>
          <th className={TH}>Run</th>
          <th className={TH}>Model hash</th>
          <th className={TH}>Solver</th>
          <th className={`${TH} text-right`}>Candidates</th>
          <th className={`${TH} text-right`}>Violations</th>
          <th className={TH}>Started</th>
          <th className={`${TH} text-right`}>Wall</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => {
          const on = selected.has(r.id);
          const violations = r.candidates.reduce((n, c) => n + c.violations.length, 0);

          return (
            <tr key={r.id} className={on ? "bg-panel-3" : undefined}>
              <td className={`${TD}${on ? " shadow-[inset_2px_0_0_var(--bs-fg)]" : ""}`}>
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() => onToggle(r.id)}
                  aria-label={`Select sweep ${r.id}`}
                  className="size-[11px] accent-[var(--bs-fg)]"
                />
              </td>
              <td className={`${TD} text-fg`}>{r.id}</td>
              <td className={TD}>{r.modelHash.slice(0, 16)}</td>
              <td className={TD}>{r.solverVersion}</td>
              <td className={`${TD} text-right`}>{r.candidates.length}</td>
              <td className={`${TD} text-right`}>{violations}</td>
              <td className={TD}>{formatInstantLocal(r.startedAt)}</td>
              <td className={`${TD} text-right`}>{wallOf(r)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function SweepCard({
  run,
  selected,
  onToggle,
}: {
  readonly run: SweepRun;
  readonly selected: boolean;
  readonly onToggle: () => void;
}): React.JSX.Element {
  const violations = run.candidates.reduce((n, c) => n + c.violations.length, 0);

  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-pressed={selected}
        className={`flex w-full flex-col gap-1.5 border-b border-line-t px-3.5 py-3 text-left ${
          selected ? "bg-panel-3 shadow-[inset_2px_0_0_var(--bs-fg)]" : ""
        }`}
      >
        <span className="font-mono text-[11px] text-fg">{run.id}</span>
        <span className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-[9.5px] tabular text-fg-3">
          <span>{run.candidates.length} candidates</span>
          <span>{violations} violations</span>
          <span>solver {run.solverVersion}</span>
          <span>{formatInstantLocal(run.startedAt)}</span>
        </span>
      </button>
    </li>
  );
}

function Notice({
  title,
  tone,
  children,
}: {
  readonly title: string;
  readonly tone?: "alarm";
  readonly children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div className="p-6">
      <h2
        className={`m-0 font-mono text-[10px] uppercase tracking-[0.13em] ${
          tone === "alarm" ? "text-alarm" : "text-fg-3"
        }`}
      >
        {title}
      </h2>
      <p className="mt-2 max-w-[64ch] text-[12px] leading-relaxed text-fg-2">{children}</p>
    </div>
  );
}

/** `2026-08-02 22:00Z`, matching the instant format used across the console. */
function formatInstantLocal(t: string): string {
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/.exec(t);

  return m === null ? t : `${m[1]} ${m[2]}Z`;
}

function wallOf(run: SweepRun): string {
  const ms = Date.parse(run.finishedAt) - Date.parse(run.startedAt);
  if (Number.isNaN(ms) || ms < 0) return "—";

  const total = Math.round(ms / 1000);

  return `${Math.floor(total / 60)}m ${String(total % 60).padStart(2, "0")}s`;
}
