import { useEffect, useState } from "react";

import {
  assertConsistent,
  emptyModel,
  Scene,
  type Selection,
  type ViewerModel,
} from "@blackstart/globe";
import { Field, Panel } from "@blackstart/ui";

/** Where the static export is served from. */
const MODEL_URL = "/model.json";

/**
 * The viewer.
 *
 * The data path is a static file rather than a live API call. The REST surface
 * exists and is tested, but a static default keeps CORS, auth and streaming
 * off the critical path and makes the README's screenshot reproducible from a
 * build with no server running:
 *
 *   bsctl model export <id> --format viewer --coords coords.csv \
 *     --out web/apps/viewer/public/model.json
 */
export function App(): React.JSX.Element {
  const [model, setModel] = useState<ViewerModel>(emptyModel);
  const [selected, setSelected] = useState<Selection | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load(): Promise<void> {
      const response = await fetch(MODEL_URL);
      if (!response.ok) {
        throw new Error(
          `${MODEL_URL} returned ${response.status}. Export one with ` +
            `\`bsctl model export <id> --format viewer --out public/model.json\`.`,
        );
      }

      const parsed = (await response.json()) as ViewerModel;

      // Checked before the scene touches it. A short array becomes an
      // invisible object or a crash inside a geometry update, and neither
      // names the file that was wrong.
      assertConsistent(parsed);

      if (!cancelled) {
        setModel(parsed);
      }
    }

    load().catch((error: unknown) => {
      if (!cancelled) {
        setProblem(error instanceof Error ? error.message : String(error));
      }
    });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="relative h-full w-full">
      <Scene model={model} onSelect={setSelected} />

      <div className="pointer-events-none absolute inset-0 flex flex-col justify-between p-4">
        <div className="flex items-start justify-between gap-4">
          <Panel title="Network" className="w-72">
            {problem ? (
              <p className="text-amber-300">{problem}</p>
            ) : (
              <>
                <Field label="Model" value={model.modelId || "—"} mono />
                {/* The hash, not just the identifier. A screenshot of a
                    network nobody can reproduce is not evidence of
                    anything. */}
                <Field
                  label="Hash"
                  value={model.contentHash ? model.contentHash.slice(0, 16) : "—"}
                  mono
                />
                <Field label="Buses" value={model.buses.ids.length} />
                <Field label="Branches" value={model.branches.ids.length} />
              </>
            )}
          </Panel>

          {selected && (
            <Panel title={selected.kind === "bus" ? "Bus" : "Branch"} className="w-72">
              <Field label="Name" value={selected.label} />
              <Field label="Id" value={selected.id} mono />
              {selected.kind === "bus" && (
                <>
                  <Field
                    label="Nominal"
                    value={`${model.buses.nominalKv[selected.index] ?? 0} kV`}
                  />
                  <Field label="Position" value={formatPosition(model, selected.index)} mono />
                </>
              )}
              {selected.kind === "branch" && (
                <Field
                  label="State"
                  value={model.branches.inService[selected.index] ? "in service" : "out of service"}
                />
              )}
            </Panel>
          )}
        </div>

        <p className="text-xs text-slate-500">
          Advisory only. Drag to rotate, scroll to zoom, click an element to inspect it.
        </p>
      </div>
    </div>
  );
}

/** Renders a bus position the way a coordinate is normally written. */
function formatPosition(model: ViewerModel, index: number): string {
  const lon = model.buses.coordinates[index * 3] ?? 0;
  const lat = model.buses.coordinates[index * 3 + 1] ?? 0;

  return `${lat.toFixed(4)}, ${lon.toFixed(4)}`;
}
