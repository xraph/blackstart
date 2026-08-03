import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Per-file `@vitest-environment` comments override this. Schema and
    // fixture suites are pure and run faster in node.
    environment: "node",
  },
});
