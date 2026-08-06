import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    // Generated and build output.
    //
    // This once anticipated shadcn/ui vendored into packages/ui by its CLI.
    // Nothing ever was, and the 2026-08-05 primitives design settled the
    // question the other way: the console's controls are hand-rolled on its
    // own tokens, so there is no vendored tree to exempt.
    ignores: [
      "**/dist/**",
      "**/node_modules/**",
      "**/.vite/**",
      "**/*.gen.ts",
      "packages/api-client/src/generated/**",
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,

  {
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser, ...globals.es2021 },
    },
    rules: {
      // An unused variable is usually a half-finished thought. The underscore
      // prefix is the explicit escape hatch.
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],

      // `any` defeats the point of the strict tsconfig. Warn rather than error
      // so that a third-party type gap does not block a build.
      "@typescript-eslint/no-explicit-any": "warn",

      // Float comparison and unit confusion are this project's characteristic
      // bug class on the Go and Rust side; the same care applies here, where
      // the viewer converts between geodetic, ECEF, and screen space.
      eqeqeq: ["error", "always", { null: "ignore" }],
      "no-console": ["warn", { allow: ["warn", "error"] }],
    },
  },

  {
    // Config files run in Node, not the browser.
    files: ["*.config.{js,ts}", "**/vite.config.ts"],
    languageOptions: { globals: globals.node },
  },
);
