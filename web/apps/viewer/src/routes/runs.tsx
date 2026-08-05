/**
 * The runs index — contingency sweeps and scenario runs.
 *
 * This screen once showed sweeps and nothing else, and said so at length: the
 * reviewed design asked for *scenario runs* — expected unserved energy, a
 * reproducibility verdict, both time axes — and none of it existed, because
 * risk and cascade were phases 3 and 4 and the API published no cascade at
 * all. Rendering the design's columns then would have meant inventing the
 * numbers, so the screen showed sweeps as sweeps and the scenario-run grid
 * waited for scenario runs.
 *
 * They exist now. A scenario binds a network model, a weather cycle,
 * hazard-derived line ratings, a contingency policy and a seed; executing it
 * screens contingencies and cascades the worst outage, producing up to two
 * child runs — and both listings are on this screen, each in its own section,
 * because they answer different questions and share no columns.
 *
 * Expected unserved energy and the reproducibility verdict are still not
 * served for either kind, and are still absent rather than approximated.
 *
 * What a scenario run does carry is weather coverage, and it is carried in two
 * distinguishable pieces: how many branches the run rated, and how many
 * branches there were. `weatherRated` *absent* means the rating step never
 * ran; `weatherRated: 0` against two thousand branches means the run rated
 * them and the weather touched none — a real finding. A `?? 0` anywhere in
 * this file collapses the first into the second at the last boundary in a
 * chain that goes back to a nullable Postgres column, so the rendering below
 * tests for `undefined` explicitly and never for falsiness.
 *
 * Selection stays in the URL, so a comparison can be linked rather than
 * described.
 */

import { Link, useNavigate } from "@tanstack/react-router";
import {
  useContingencyRunsList,
  useScenarioRunsRecent,
  type Run as SweepRun,
  type ScenarioRun,
} from "@blackstart/api-client";
import { RAIL_BREAKPOINT, StatusGlyph, useMediaQuery } from "@blackstart/ui";
import { RUN_STATUSES, abbreviateTypeId, type RunStatus } from "@blackstart/schema";

import { runsRoute } from "../router";
import { useApiClient } from "../api/provider";
import { cascadeSearch, type CascadeSearch } from "../search/params";

export function RunsScreen(): React.JSX.Element {
  const search = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const client = useApiClient();
  const wide = useMediaQuery(RAIL_BREAKPOINT);

  const sweeps = useContingencyRunsList(client, undefined, 50);
  const scenarios = useScenarioRunsRecent(client, 50);

  const sweepRuns: readonly SweepRun[] = sweeps.data?.runs ?? [];
  const scenarioRuns: readonly ScenarioRun[] = scenarios.data?.runs ?? [];

  const pending = sweeps.isPending || scenarios.isPending;
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
        <span className="whitespace-nowrap font-mono text-[10px] uppercase tracking-[0.13em] text-fg-3">
          Runs
        </span>
        <span className="flex-1" />
        <span className="whitespace-nowrap font-mono text-[10px] tabular text-fg-3">
          {pending ? "loading…" : `${sweepRuns.length + scenarioRuns.length} runs`}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        <Section title="Contingency sweeps">
          {sweeps.isError ? (
            <Notice tone="alarm" title="The platform is not reachable">
              {sweeps.error.message}
              <br />
              Start the host with <code className="font-mono">./bin/blackstart</code>, or point the
              viewer elsewhere with <code className="font-mono">VITE_API_BASE_URL</code>.
            </Notice>
          ) : sweeps.isPending ? (
            <Notice title="Loading sweeps">Asking the platform for recent N-1 runs.</Notice>
          ) : sweepRuns.length === 0 ? (
            <Notice title="No sweeps recorded">
              Run one with <code className="font-mono">bsctl contingency sweep</code>. The platform
              is reachable and has nothing to show.
            </Notice>
          ) : wide ? (
            <SweepTable runs={sweepRuns} selected={selected} onToggle={toggle} />
          ) : (
            <ul className="m-0 list-none p-0">
              {sweepRuns.map((r) => (
                <SweepCard
                  key={r.id}
                  run={r}
                  selected={selected.has(r.id)}
                  onToggle={() => toggle(r.id)}
                />
              ))}
            </ul>
          )}
        </Section>

        <Section title="Scenario runs">
          {scenarios.isError ? (
            <Notice tone="alarm" title="The platform is not reachable">
              {scenarios.error.message}
              <br />
              Start the host with <code className="font-mono">./bin/blackstart</code>, or point the
              viewer elsewhere with <code className="font-mono">VITE_API_BASE_URL</code>.
            </Notice>
          ) : scenarios.isPending ? (
            <Notice title="Loading scenario runs">
              Asking the platform for recent scenario executions.
            </Notice>
          ) : scenarioRuns.length === 0 ? (
            <Notice title="No scenario runs recorded">
              Execute one with <code className="font-mono">bsctl scenario run</code>. The platform
              is reachable and has nothing to show.
            </Notice>
          ) : wide ? (
            <ScenarioTable runs={scenarioRuns} />
          ) : (
            <ul className="m-0 list-none p-0">
              {scenarioRuns.map((r) => (
                <ScenarioCard key={r.id} run={r} />
              ))}
            </ul>
          )}
        </Section>
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
          Expected unserved energy and the reproducibility verdict are not served yet, for either
          kind of run — the platform models power flow, N-1 screening and cascade, not risk.
        </span>
      </div>
    </div>
  );
}

function Section({
  title,
  children,
}: {
  readonly title: string;
  readonly children: React.ReactNode;
}): React.JSX.Element {
  return (
    <section>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-line-t bg-panel-2 px-3 py-1.5">
        <h2 className="m-0 font-mono text-[9px] uppercase tracking-[0.13em] text-fg-2">{title}</h2>
      </div>
      {children}
    </section>
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

function ScenarioTable({ runs }: { readonly runs: readonly ScenarioRun[] }): React.JSX.Element {
  return (
    <table className="w-full border-collapse font-mono text-[10.5px] tabular">
      <thead>
        <tr>
          <th className={TH}>Run</th>
          <th className={TH}>Scenario</th>
          <th className={TH}>Status</th>
          <th className={TH}>Weather</th>
          <th className={TH}>Failed at</th>
          <th className={TH}>Cascade</th>
          <th className={TH}>Queued</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => (
          <tr key={r.id}>
            <td className={`${TD} text-fg`}>{r.id}</td>
            <td className={TD}>{r.scenarioId}</td>
            <td className={TD}>
              <RunStatusCell status={r.status} />
            </td>
            <td className={TD}>{weatherCoverage(r)}</td>
            <td className={TD}>{failedStep(r) ?? <span className="text-fg-4">—</span>}</td>
            <td className={TD}>
              <CascadeLink run={r} />
            </td>
            <td className={TD}>{formatInstantLocal(r.queuedAt)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ScenarioCard({ run }: { readonly run: ScenarioRun }): React.JSX.Element {
  const step = failedStep(run);

  return (
    <li className="flex flex-col gap-1.5 border-b border-line-t px-3.5 py-3">
      <span className="font-mono text-[11px] text-fg">{run.id}</span>
      <span className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[9.5px] tabular text-fg-3">
        <RunStatusCell status={run.status} />
        <span>{weatherCoverage(run)}</span>
        {step === undefined ? null : <span className="text-alarm">failed at {step}</span>}
        <span>{formatInstantLocal(run.queuedAt)}</span>
      </span>
      <CascadeLink run={run} />
    </li>
  );
}

/**
 * The status, as the shared glyph where the shared glyph applies.
 *
 * `ScenarioRun.status` is a plain string on the wire. Four spellings are known
 * — the scenario extension's own `Status` constants — and those get the
 * CVD-safe glyph the runs grid was designed around. A fifth spelling the
 * platform grows later is shown verbatim rather than mapped onto the nearest
 * glyph, because guessing which shape an unknown status deserves is exactly
 * the kind of quiet approximation this screen refuses elsewhere.
 */
function RunStatusCell({ status }: { readonly status: string }): React.JSX.Element {
  return isRunStatus(status) ? (
    <StatusGlyph status={status} />
  ) : (
    <span className="text-fg-3">{status}</span>
  );
}

function isRunStatus(status: string): status is RunStatus {
  return (RUN_STATUSES as readonly string[]).includes(status);
}

/**
 * How much of the network the weather actually rated.
 *
 * The one rendering in this file that must not be simplified. `weatherRated`
 * is optional all the way down — a nullable Postgres column, a `,omitempty`
 * off the wire, `optional:"true"` in the OpenAPI document — precisely so that
 * "the rating step never ran" stays distinguishable from "the rating step ran
 * and the weather touched nothing". Both are legitimate; only one is a
 * finding. `run.weatherRated ?? 0` would print the finding for the non-event,
 * so the test is against `undefined` and never against falsiness.
 *
 * `branchCount` is treated the same way for the same reason, and a run that
 * rated branches without recording how many there were reads "3 of —" rather
 * than inventing a denominator.
 */
function weatherCoverage(run: ScenarioRun): string {
  if (run.weatherRated === undefined) return "not rated";

  const total = run.branchCount === undefined ? "—" : String(run.branchCount);

  return `${run.weatherRated} of ${total}`;
}

/** The step a failed run stopped at, and nothing at all for a run that did not fail. */
function failedStep(run: ScenarioRun): string | undefined {
  return run.failure === undefined ? undefined : run.failure.step;
}

/**
 * The link that turns "here is a cascade" into "here is what this storm did".
 *
 * The cascade screen renders a run by id, so a scenario run reaches it
 * directly — carrying the manifest's own valid time, because the cascade
 * screen sits on a time axis and the storm's hour is the run's, not today's.
 *
 * Parsed rather than asserted: an id the cascade route would reject, or a
 * manifest instant that is not a UTC instant, produces no link instead of a
 * navigation that throws at the route boundary.
 */
function CascadeLink({ run }: { readonly run: ScenarioRun }): React.JSX.Element {
  const search = cascadeLinkSearch(run);

  if (search === undefined) return <span className="text-fg-4">—</span>;

  return (
    <Link
      to="/cascade"
      search={search}
      className="text-fg underline"
      aria-label={`Play back cascade ${search.run ?? ""} from scenario run ${run.id}`}
    >
      {abbreviateTypeId(search.run ?? "")}
    </Link>
  );
}

function cascadeLinkSearch(run: ScenarioRun): CascadeSearch | undefined {
  if (run.cascadeRunId === undefined) return undefined;

  const parsed = cascadeSearch.safeParse({
    valid: run.manifest.validAt,
    run: run.cascadeRunId,
  });

  return parsed.success ? parsed.data : undefined;
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
