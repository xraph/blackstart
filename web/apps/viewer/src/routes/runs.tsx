/**
 * The scenario runs index.
 *
 * The dense, non-globe surface. Comparison selection lives in the URL, so a
 * diff between two runs can be linked rather than described.
 *
 * Below the rail breakpoint the table becomes a card list carrying the run,
 * both time axes, status and the repro verdict — the columns a reader
 * actually scans. A horizontally scrolling twelve-column grid is not a
 * narrow-viewport design, it is a desktop grid someone has to drag.
 */

import { useNavigate } from "@tanstack/react-router";
import {
  QuantityValue,
  RAIL_BREAKPOINT,
  ReproBadge,
  RunsGrid,
  StatusGlyph,
  useMediaQuery,
} from "@blackstart/ui";
import { compareRuns, formatInstant, isBacktest, type ScenarioRun } from "@blackstart/schema";

import { runsRoute } from "../router";
import { RUNS } from "../fixtures/derecho";

export function RunsScreen(): React.JSX.Element {
  const search = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const wide = useMediaQuery(RAIL_BREAKPOINT);

  const selected = new Set(search.compare);

  /* Keeps the two most recent picks. Silently ignoring a third would leave
   * the reader wondering which one took; dropping the oldest is at least
   * predictable. */
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

  const picked = RUNS.filter((r) => selected.has(r.id));
  const [a, b] = picked;
  const diff = a !== undefined && b !== undefined ? compareRuns(a, b) : null;

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 overflow-x-auto border-b border-line bg-panel px-3">
        <Filter label="KIND" value={search.kind} />
        <Filter label="STATUS" value={search.status} />
        <span className="flex-1" />
        <span className="whitespace-nowrap font-mono text-[10px] tabular text-fg-3">
          {RUNS.length} runs
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {wide ? (
          <RunsGrid runs={RUNS} selected={selected} onToggle={toggle} />
        ) : (
          <ul className="m-0 list-none p-0">
            {RUNS.map((r) => (
              <RunCard
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
        <span className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
          Selected
        </span>

        {picked.length === 0 ? (
          <span className="text-[11px] text-fg-3">Pick two runs to compare them.</span>
        ) : (
          <div className="flex flex-col gap-0.5 font-mono text-[10px] tabular text-fg">
            {picked.map((r) => (
              <span key={r.id}>
                {r.id} <span className="text-fg-3">· {r.scenario}</span>
              </span>
            ))}
          </div>
        )}

        <span className="flex-1" />

        {diff !== null &&
          (diff.comparable ? (
            <span className="font-mono text-[10px] tabular text-fg-3">
              Δ EUE{" "}
              {diff.deltaEue === null
                ? "—"
                : `${diff.deltaEue > 0 ? "+" : "−"}${Math.abs(diff.deltaEue)} MW·h`}{" "}
              · same valid and knowledge time
            </span>
          ) : (
            /* Naming the reason rather than disabling a control with no
             * explanation: a delta across different knowledge states mixes a
             * change in the scenario with a change in what was known. */
            <span className="max-w-[46ch] text-[11px] leading-snug text-amber">
              These runs do not share both time axes, so a difference between them would mix
              a change in the scenario with a change in what was known.
            </span>
          ))}
      </div>
    </div>
  );
}

function Filter({
  label,
  value,
}: {
  readonly label: string;
  readonly value: string;
}): React.JSX.Element {
  return (
    <span className="flex h-6 shrink-0 items-center gap-1.5 rounded-[3px] border border-line-2 bg-panel-2 px-2.5">
      <span className="font-mono text-[9px] tracking-[0.09em] text-fg-3">{label}</span>
      <span className="font-mono text-[10px] text-fg">{value}</span>
      <span className="text-[8px] text-fg-3">▾</span>
    </span>
  );
}

/**
 * One run, for a narrow viewport.
 *
 * Carries both time axes rather than collapsing to one. They are the whole
 * reason this screen has two date columns, and a card showing only the valid
 * time cannot tell a backtest from a live forecast.
 */
function RunCard({
  run,
  selected,
  onToggle,
}: {
  readonly run: ScenarioRun;
  readonly selected: boolean;
  readonly onToggle: () => void;
}): React.JSX.Element {
  const back =
    run.temporal.validTime !== undefined &&
    isBacktest({ validTime: run.temporal.validTime, knowledgeTime: run.temporal.knowledgeTime });

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
        <span className="flex items-center gap-2">
          <span className="font-mono text-[11px] text-fg">{run.id}</span>
          <span className="ml-auto text-[10px]">
            <StatusGlyph status={run.status} />
          </span>
        </span>

        <span className="text-[11px] text-fg-2">{run.scenario}</span>

        <span className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-[9.5px] tabular text-fg-3">
          <span className={back ? "text-amber" : undefined}>
            valid{" "}
            {run.temporal.validTime === undefined
              ? "—"
              : formatInstant(run.temporal.validTime)}
          </span>
          <span className={back ? "text-amber" : undefined}>
            known {formatInstant(run.temporal.knowledgeTime)}
          </span>
          <span>
            EUE{" "}
            {run.expectedUnservedEnergy === null ? (
              "—"
            ) : (
              <QuantityValue q={run.expectedUnservedEnergy} className="text-[9.5px]" />
            )}
          </span>
          <ReproBadge verdict={run.repro} />
        </span>
      </button>
    </li>
  );
}
