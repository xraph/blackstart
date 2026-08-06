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
 *
 * The theme switcher lives on this screen rather than only behind a session,
 * because the ground is a working condition: somebody signing in from a lit
 * control room at midday should not have to authenticate against a near-black
 * page first to be allowed to change it.
 */

import { useEffect, useState, type FormEvent, type ReactElement } from "react";
import { AdvisoryChip, ThemeToggle, useTheme } from "@blackstart/ui";

import { IdentityError } from "./authsome";
import { DynamicFields } from "./DynamicFields";
import { initialValues, signupForm, validate, type SignupForm } from "./formconfig";
import { useAuth } from "./provider";

type Mode = "signin" | "register";

const FIELD =
  "h-8 w-full rounded-[3px] border border-line-2 bg-panel-2 px-2.5 font-mono text-[12px] " +
  "text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 " +
  "focus-visible:outline-fg-2";

const LABEL = "font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3";

function Field({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactElement;
}): ReactElement {
  return (
    <label className="flex min-w-0 flex-1 flex-col gap-1.5">
      <span className={LABEL}>{label}</span>
      {children}
    </label>
  );
}

export function LoginScreen(): React.JSX.Element {
  const { signIn, signUp } = useAuth();
  const { theme, setTheme } = useTheme();

  const [mode, setMode] = useState<Mode>("signin");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  /**
   * The declared form, once the host has been asked.
   *
   * Null while unasked. The fetch is not awaited before the static fields
   * render — a deployment with no custom fields is the common case, and
   * blocking sign-in on a request that will almost always answer "nothing to
   * add" would put a spinner in front of everyone to serve a few.
   */
  const [form, setForm] = useState<SignupForm | null>(null);
  const [extra, setExtra] = useState<Record<string, string>>({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const registering = mode === "register";

  // Asked once, and only when registering: signing in never reads it.
  useEffect(() => {
    if (!registering || form !== null) return;

    const abort = new AbortController();

    void signupForm(abort.signal).then((found) => {
      setForm(found);
      setExtra(initialValues(found.fields));
    });

    return () => abort.abort();
  }, [registering, form]);

  const declared = form?.active === true ? form : null;

  const submit = async (event: FormEvent): Promise<void> => {
    event.preventDefault();
    setProblem(null);
    setBusy(true);

    if (declared !== null) {
      // Checked before the request so a reader is told which box is wrong,
      // rather than getting one sentence about a form they must re-scan. The
      // host validates the same rules regardless — this only decides where
      // the message appears.
      const failures = validate(declared.fields, extra);

      setFieldErrors(failures);

      if (Object.keys(failures).length > 0) {
        setBusy(false);

        return;
      }
    }

    try {
      if (registering) {
        await signUp({
          email,
          password,
          firstName,
          lastName,
          ...(declared === null ? {} : { metadata: extra }),
        });
      } else await signIn({ email, password });
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

  return (
    <div className="flex h-full flex-col bg-bg">
      {/* The switcher sits in its own bar rather than inside the card. It is
          chrome, not part of signing in, and putting it in the form would make
          it the first thing tab order reaches. */}
      <div className="flex shrink-0 items-center justify-end px-3 py-2.5">
        <ThemeToggle theme={theme} onChange={setTheme} />
      </div>

      <div className="flex min-h-0 flex-1 items-center justify-center px-4 pb-14">
        <form
          onSubmit={(e) => void submit(e)}
          className="w-full max-w-[380px] rounded-[4px] border border-line bg-panel p-5 shadow-[0_1px_2px_rgb(0_0_0/0.18)]"
        >
          <div className="font-mono text-[11.5px] font-bold tracking-[0.16em] text-fg">
            BLACKSTART
          </div>
          <h1 className="mt-1 text-[15px] font-semibold tracking-[-0.018em] text-fg">
            {registering
              ? declared?.title !== undefined && declared.title !== ""
                ? declared.title
                : "Register an operator"
              : "Sign in"}
          </h1>
          <p className="mt-1 text-[11px] leading-relaxed text-fg-3">
            {registering
              ? declared?.description !== undefined && declared.description !== ""
                ? declared.description
                : "Your name is attributed to the runs and decisions you record."
              : "Weather-driven outage risk for the transmission network."}
          </p>

          <div className="mt-4 flex flex-col gap-3">
            {registering && (
              // One row on any width the card allows. Given names and family
              // names are both short enough, and stacking them would push the
              // password below the fold on a phone in landscape.
              <div className="flex gap-2.5">
                <Field label="First name">
                  <input
                    type="text"
                    required
                    autoComplete="given-name"
                    value={firstName}
                    onChange={(e) => setFirstName(e.target.value)}
                    className={FIELD}
                  />
                </Field>
                <Field label="Last name">
                  <input
                    type="text"
                    required
                    autoComplete="family-name"
                    value={lastName}
                    onChange={(e) => setLastName(e.target.value)}
                    className={FIELD}
                  />
                </Field>
              </div>
            )}

            <Field label="Email">
              <input
                type="email"
                required
                autoComplete="username"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className={FIELD}
              />
            </Field>

            <Field label="Password">
              <input
                type="password"
                required
                autoComplete={registering ? "new-password" : "current-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className={FIELD}
              />
            </Field>

            {/* Below the credential, because email and password are what
                create the account and everything the deployment added is
                about the person, not the sign-in. */}
            {registering && declared !== null && (
              <DynamicFields
                fields={declared.fields}
                values={extra}
                errors={fieldErrors}
                disabled={busy}
                onChange={(key, value) => {
                  setExtra((prev) => ({ ...prev, [key]: value }));

                  // Cleared as they type. Leaving a message under a box that
                  // has since been corrected is worse than never showing one.
                  setFieldErrors((prev) => {
                    if (prev[key] === undefined) return prev;

                    const next = { ...prev };
                    delete next[key];

                    return next;
                  });
                }}
              />
            )}
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
            {busy ? "Working…" : registering ? "Register" : "Sign in"}
          </button>

          <button
            type="button"
            onClick={() => {
              setMode(registering ? "signin" : "register");
              setProblem(null);
            }}
            className="mt-2.5 w-full text-[11px] text-fg-3 hover:text-fg-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
          >
            {registering ? "I already have an account" : "Register an operator instead"}
          </button>

          <div className="mt-5 flex justify-center border-t border-line pt-4">
            <AdvisoryChip />
          </div>

          <p className="mt-3 text-center text-[10.5px] leading-relaxed text-fg-3">
            Sessions are held in a cookie this page cannot read. Sign out to end one.
          </p>
        </form>
      </div>
    </div>
  );
}
