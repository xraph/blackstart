import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Per-file `@vitest-environment` comments override this; the default is
    // jsdom because everything in this package renders.
    environment: "jsdom",
  },
});
