/**
 * Signing in to the console.
 *
 * Built on this project's own tokens rather than dropped in from a component
 * library. The console has no accent colour and a deliberately tight density,
 * and a pre-styled auth form would arrive with its own opinion about both —
 * one screen looking like a different product is exactly the seam an operator
 * notices.
 *
 * The advisory boundary is stated here too. It is the first surface anyone
 * sees, and what this system does not do is worth saying before they are
 * looking at numbers.
 */

import { useState, type FormEvent } from "react";
import { AdvisoryChip } from "@blackstart/ui";

import { IdentityError } from "./authsome";
import { useAuth } from "./provider";

type Mode = "signin" | "register";

export function LoginScreen(): React.JSX.Element {
  const { signIn, signUp } = useAuth();
  const [mode, setMode] = useState<Mode>("signin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent): Promise<void> => {
    event.preventDefault();
    setProblem(null);
    setBusy(true);

    try {
      await (mode === "signin" ? signIn : signUp)({ email, password });
    } catch (error: unknown) {
      // Authsome distinguishes an unverified address from a wrong password,
      // and so should this: one is fixed by checking your email, the other by
      // typing again. Collapsing them sends the reader down the wrong path.
      if (error instanceof IdentityError && error.kind === "email_not_verified") {
        setProblem(
          "This address has not been verified yet. Check your email for the link, " +
            "or register a new operator to get a session immediately.",
        );
      } else {
        setProblem(error instanceof Error ? error.message : String(error));
      }
    } finally {
      setBusy(false);
    }
  };

  const field =
    "h-8 w-full rounded-[3px] border border-line-2 bg-panel-2 px-2.5 font-mono text-[12px] " +
    "text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 " +
    "focus-visible:outline-fg-2";

  return (
    <div className="flex h-full items-center justify-center bg-bg px-4">
      <form
        onSubmit={(e) => void submit(e)}
        className="w-full max-w-[380px] rounded-[4px] border border-line bg-panel p-5"
      >
        <div className="font-mono text-[11.5px] font-bold tracking-[0.16em] text-fg">
          BLACKSTART
        </div>
        <h1 className="mt-1 text-[15px] font-semibold tracking-[-0.018em] text-fg">
          {mode === "signin" ? "Sign in" : "Register an operator"}
        </h1>

        <div className="mt-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1.5">
            <span className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
              Email
            </span>
            <input
              type="email"
              required
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className={field}
            />
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3">
              Password
            </span>
            <input
              type="password"
              required
              autoComplete={mode === "signin" ? "current-password" : "new-password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className={field}
            />
          </label>
        </div>

        {problem !== null && (
          <p
            role="alert"
            className="mt-3 border-l-2 border-alarm bg-panel-2 px-2.5 py-2 text-[11.5px] leading-snug text-fg-2"
          >
            {problem}
          </p>
        )}

        <button
          type="submit"
          disabled={busy}
          className="mt-4 h-8 w-full rounded-[3px] bg-fg text-[11.5px] font-semibold text-ink disabled:opacity-60 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
        >
          {busy ? "Working…" : mode === "signin" ? "Sign in" : "Register"}
        </button>

        <button
          type="button"
          onClick={() => {
            setMode(mode === "signin" ? "register" : "signin");
            setProblem(null);
          }}
          className="mt-2.5 w-full text-[11px] text-fg-3 hover:text-fg-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
        >
          {mode === "signin" ? "Register an operator instead" : "I already have an account"}
        </button>

        <div className="mt-5 flex justify-center border-t border-line pt-4">
          <AdvisoryChip />
        </div>

        <p className="mt-3 text-center text-[10.5px] leading-relaxed text-fg-3">
          Sessions are held in memory only, so reloading signs you out.
        </p>
      </form>
    </div>
  );
}
