/**
 * Loading the network the globe draws.
 *
 * The data path is a static file rather than a live API call. The REST surface
 * exists, but a static default keeps CORS, auth and streaming off the critical
 * path and makes a screenshot reproducible from a build with no server
 * running:
 *
 *   bsctl model export <id> --format viewer --coords coords.csv \
 *     --out web/apps/viewer/public/model.json
 *
 * This is the one piece the generated API client will replace. Isolating it in
 * a hook means that swap is a change of source, not a change of screen.
 */

import { useEffect, useState } from "react";
import { assertConsistent, emptyModel, type ViewerModel } from "@blackstart/globe";

/** Where the static export is served from. */
const MODEL_URL = "/model.json";

export interface ViewerModelState {
  readonly model: ViewerModel;
  /** The reason the network could not be loaded, phrased for a person. */
  readonly problem: string | null;
}

export function useViewerModel(): ViewerModelState {
  const [model, setModel] = useState<ViewerModel>(emptyModel);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load(): Promise<void> {
      const response = await fetch(MODEL_URL);
      if (!response.ok) {
        throw new Error(
          `${MODEL_URL} returned ${response.status}. Export one with ` +
            "`bsctl model export <id> --format viewer --out public/model.json`.",
        );
      }

      const parsed = (await response.json()) as ViewerModel;

      // Checked before the scene touches it. A short array becomes an
      // invisible object or a crash inside a geometry update, and neither
      // names the file that was wrong.
      assertConsistent(parsed);

      if (!cancelled) setModel(parsed);
    }

    load().catch((error: unknown) => {
      if (!cancelled) setProblem(error instanceof Error ? error.message : String(error));
    });

    return () => {
      cancelled = true;
    };
  }, []);

  return { model, problem };
}
