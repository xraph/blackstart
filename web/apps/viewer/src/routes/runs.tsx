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
 * child runs. So `kind` is honoured rather than declared: sweeps, scenario
 * runs, or both. `cascade` and `validation` remain unlisted — the platform
 * serves a cascade *by id* and no index of them — and the screen says that
 * instead of quietly showing something else under a URL that promised
 * cascades.
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
import { cascadeSearch, type CascadeSearch, type RunsSearch } from "../search/params";

const KINDS = ["all", "sweep", "scenario", "cascade", "validation"] as const;
const STATUSES = ["any", "complete", "running", "failed", "queued"] as const;

export function RunsScreen(): React.JSX.Element {
  const search = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const client = useApiClient();
  const wide = useMediaQuery(RAIL_BREAKPOINT);

  const showSweeps = search.kind === "all" || search.kind === "sweep";
  const showScenarios = search.kind === "all" || search.kind === "scenario";
  /** `cascade` and `validation`: named in the URL contract, served by no index. */
  const unlisted = !showSweeps && !showScenarios;

  const sweeps = useContingencyRunsList(client, undefined, 50, { enabled: showSweeps });
  const scenarios = useScenarioRunsRecent(client, 50, { enabled: showScenarios });

  const sweepRuns: readonly SweepRun[] = showSweeps ? (sweeps.data?.runs ?? []) : [];
  /**
   * A scenario run publishes a status, so the status filter is real here. A
   * sweep publishes none at all — see `StatusNote` — so this is the only
   * listing the filter touches.
   */
  const scenarioRuns: readonly ScenarioRun[] = showScenarios
    ? (scenarios.data?.runs ?? []).filter(
        (r) => search.status === "any" || r.status === search.status,
      )
    : [];

  const pending = (showSweeps && sweeps.isPending) || (showScenarios && scenarios.isPending);
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

  const setKind = (kind: RunsSearch["kind"]): void => {
    void navigate({ search: (prev) => ({ ...prev, kind }) });
  };
  const setStatus = (status: RunsSearch["status"]): void => {
    void navigate({ search: (prev) => ({ ...prev, status }) });
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 overflow-x-auto border-b border-line bg-panel px-3">
        <span className="whitespace-nowrap font-mono text-[10px] uppercase tracking-[0.13em] text-fg-3">
          Runs
        </span>
        <Segmented label="Run kind" options={KINDS} value={search.kind} onChange={setKind} />
        <Segmented
          label="Run status"
          options={STATUSES}
          value={search.status}
          onChange={setStatus}
        />
        <span className="flex-1" />
        <span className="whitespace-nowrap font-mono text-[10px] tabular text-fg-3">
          {pending ? "loading…" : `${sweepRuns.length + scenarioRuns.length} runs`}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {unlisted ? (
          <Notice title={`No ${search.kind} run index`}>
            The platform serves a {search.kind} run by id — the cascade screen plays one back from{" "}
            <code className="font-mono">?run=</code> — but publishes no listing of them. Rather than
            show sweeps under a URL that says <code className="font-mono">kind={search.kind}</code>,
            this screen shows nothing.
          </Notice>
        ) : (
          <>
            {showSweeps ? (
              <Section title="Contingency sweeps" note={<StatusNote status={search.status} />}>
                {sweeps.isError ? (
                  <Notice tone="alarm" title="The platform is not reachable">
                    {sweeps.error.message}
                    <br />
                    Start the host with <code className="font-mono">./bin/blackstart</code>, or
                    point the viewer elsewhere with{" "}
                    <code className="font-mono">VITE_API_BASE_URL</code>.
                  </Notice>
                ) : sweeps.isPending ? (
                  <Notice title="Loading sweeps">Asking the platform for recent N-1 runs.</Notice>
                ) : sweepRuns.length === 0 ? (
                  <Notice title="No sweeps recorded">
                    Run one with <code className="font-mono">bsctl contingency sweep</code>. The
                    platform is reachable and has nothing to show.
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
            ) : null}

            {showScenarios ? (
              <Section title="Scenario runs">
                {scenarios.isError ? (
                  <Notice tone="alarm" title="The platform is not reachable">
                    {scenarios.error.message}
                    <br />
                    Start the host with <code className="font-mono">./bin/blackstart</code>, or
                    point the viewer elsewhere with{" "}
                    <code className="font-mono">VITE_API_BASE_URL</code>.
                  </Notice>
                ) : scenarios.isPending ? (
                  <Notice title="Loading scenario runs">
                    Asking the platform for recent scenario executions.
                  </Notice>
                ) : scenarioRuns.length === 0 ? (
                  <Notice title="No scenario runs recorded">
                    {search.status === "any"
                      ? "Execute one with bsctl scenario run. The platform is reachable and has nothing to show."
                      : `No scenario run is ${search.status}. Widen the status filter to see the rest.`}
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
            ) : null}
          </>
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
          Expected unserved energy and the reproducibility verdict are not served yet, for either
          kind of run — the platform models power flow, N-1 screening and cascade, not risk.
        </span>
      </div>
    </div>
  );
}

/**
 * The status filter, where it cannot honestly be applied.
 *
 * A sweep run carries no status field at all: it is a completed record or it
 * is not in the listing. Silently filtering it to nothing would claim the
 * platform answered a question it was never asked, and silently ignoring the
 * filter would let a reader believe these rows are the `failed` ones. So the
 * filter is applied to scenario runs, where it is real, and this says out loud
 * that the sweeps beside them are unfiltered.
 */
function StatusNote({ status }: { readonly status: RunsSearch["status"] }): React.JSX.Element {
  if (status === "any") return <span className="text-fg-4">every sweep on record</span>;

  return (
    <span className="text-risk-3">
      unfiltered — a sweep publishes no status, so “{status}” cannot be applied here
    </span>
  );
}

function Segmented<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  readonly label: string;
  readonly options: readonly T[];
  readonly value: T;
  readonly onChange: (next: T) => void;
}): React.JSX.Element {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className="flex shrink-0 overflow-hidden rounded-[2px] border border-line-2"
    >
      {options.map((option) => {
        const on = option === value;

        return (
          <button
            key={option}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(option)}
            className={`h-[19px] border-r border-line-2 px-[7px] font-mono text-[9.5px] uppercase tracking-[0.08em] last:border-r-0 ${
              on ? "bg-panel-3 text-fg" : "text-fg-3"
            }`}
          >
            {option}
          </button>
        );
      })}
    </div>
  );
}

function Section({
  title,
  note,
  children,
}: {
  readonly title: string;
  readonly note?: React.ReactNode;
  readonly children: React.ReactNode;
}): React.JSX.Element {
  return (
    <section>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-line-t bg-panel-2 px-3 py-1.5">
        <h2 className="m-0 font-mono text-[9px] uppercase tracking-[0.13em] text-fg-2">{title}</h2>
        {note === undefined ? null : (
          <span className="font-mono text-[9px] tracking-[0.02em]">{note}</span>
        )}
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
          <th className={TH}>Failure</th>
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
            <td className={TD}>
              <FailureCell run={r} />
            </td>
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
  const failure = failureOf(run);

  return (
    <li className="flex flex-col gap-1.5 border-b border-line-t px-3.5 py-3">
      <span className="font-mono text-[11px] text-fg">{run.id}</span>
      <span className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[9.5px] tabular text-fg-3">
        <RunStatusCell status={run.status} />
        <span>{weatherCoverage(run)}</span>
        <span>{formatInstantLocal(run.queuedAt)}</span>
      </span>
      {failure === undefined ? null : (
        // The card wraps where the row truncates: a card has the width a table
        // cell does not, and the reason is the half of the pair worth reading
        // in full.
        <span className="font-mono text-[9.5px] leading-snug text-alarm">
          failed at {failure.step}
          {failure.reason === undefined ? null : (
            <span className="text-fg-3"> — {failure.reason}</span>
          )}
        </span>
      )}
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

/**
 * Where a failed run stopped, and why — both halves, because they are not the
 * same fact.
 *
 * `extensions/scenario/scenario/run.go` keeps `Step` and `Reason` apart on
 * purpose: the four `Step` values are the coarse classification a dashboard
 * filters on, and `Reason` is the human-readable explanation, deliberately not
 * a machine-parsed code. A row showing only the step says the run stopped at
 * ratings; it does not say whether the forecast had no ampacity for the
 * branches this model reaches or whether the model never joined to the asset
 * record at all. Those are two different errors the backend goes to trouble to
 * distinguish, and they are fixed by two different people. Showing the step
 * alone throws that away at the last step of the chain.
 *
 * An empty reason is treated as no reason. The wire always carries the field —
 * Go's `Failure.Reason` has no `omitempty` — so absence arrives as `""`, and a
 * row reading "ratings — " is worse than a row reading "ratings".
 */
function failureOf(run: ScenarioRun): { step: string; reason: string | undefined } | undefined {
  if (run.failure === undefined) return undefined;

  // Widened deliberately: the generated type says `reason: string`, but this
  // is a wire payload and a field the platform stops sending would arrive as
  // `undefined` regardless of what the type asserts.
  const reason: string | undefined = run.failure.reason;

  return {
    step: run.failure.step,
    reason: reason === undefined || reason.trim() === "" ? undefined : reason,
  };
}

/**
 * The failure, in a row that cannot grow.
 *
 * A reason is a sentence — `ratingGap` in `extensions/scenario/resolve.go`
 * writes two of them and both run to several lines — so the cell truncates and
 * carries the full pair in `title`, which is what every other long string in
 * this console does (`risk.tsx` does exactly this with its evidence text). The
 * step never truncates: it is four known words and it is the half that has to
 * be readable at a glance.
 */
function FailureCell({ run }: { readonly run: ScenarioRun }): React.JSX.Element {
  const failure = failureOf(run);

  if (failure === undefined) return <span className="text-fg-4">—</span>;

  return (
    <span
      className="flex items-baseline gap-2"
      title={failure.reason === undefined ? failure.step : `${failure.step} — ${failure.reason}`}
    >
      <span className="shrink-0 text-alarm">{failure.step}</span>
      {failure.reason === undefined ? null : (
        <span className="max-w-[34ch] truncate text-fg-3">{failure.reason}</span>
      )}
    </span>
  );
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
