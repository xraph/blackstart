import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    // Generated, vendored, and build output. shadcn/ui components are vendored
    // into packages/ui by the shadcn CLI rather than imported as a dependency,
    // so they are linted for correctness but not for our stylistic rules.
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
