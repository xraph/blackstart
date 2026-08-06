/**
 * The signup form, as the platform defines it.
 *
 * Authsome lets an operator's account fields be declared server-side rather
 * than compiled into this bundle: a FormConfig per app, with typed fields,
 * validation and an order. This reads that declaration so a deployment can add
 * "control area" or "NERC certification" to registration without a release.
 *
 * Two decisions worth stating, because both look like inconsistencies.
 *
 * **The config is read from the dashboard contract, and the submission is not
 * sent back through it.** Authsome exposes `auth.dynamicRegister` alongside
 * `auth.dynamicConfig`, and using it would be the symmetrical choice. It takes
 * a single `name` and splits it on the first space — so an operator called
 * "Ana Maria Vasquez" becomes Ana / Maria Vasquez. `POST /identity/v1/signup`
 * takes `first_name` and `last_name` as first-class fields and accepts the
 * same `metadata`, validated against the same FormConfig by the same engine
 * call. Same validation, better names, and no second auth transport.
 *
 * **Only the query side of the contract is used.** A dashboard `command`
 * additionally requires a CSRF token and an idempotency key; a `query` does
 * not. Reading a form definition is a query, so this stays a single anonymous
 * POST — and the credential path remains the REST surface the rest of this
 * folder already speaks.
 */

/** Where the dashboard mounts its contract dispatcher. */
const DISPATCH = "/_forge/dashboard/api/dashboard/v1";

/** Field types Authsome's FormConfig can declare. */
export type FieldType =
  | "text"
  | "email"
  | "number"
  | "tel"
  | "url"
  | "date"
  | "textarea"
  | "select"
  | "checkbox"
  | "radio"
  | "switch"
  | "object_array";

export interface SelectOption {
  readonly label: string;
  readonly value: string;
}

export interface FieldValidation {
  readonly required?: boolean;
  readonly min_len?: number;
  readonly max_len?: number;
  readonly pattern?: string;
  readonly min?: number;
  readonly max?: number;
}

export interface FormField {
  readonly key: string;
  readonly label: string;
  readonly type: FieldType;
  readonly placeholder?: string;
  readonly description?: string;
  readonly options?: readonly SelectOption[];
  readonly default?: string;
  readonly validation?: FieldValidation;
  readonly order: number;
}

/**
 * The declared form, or the absence of one.
 *
 * `active: false` is the ordinary case rather than an error: most deployments
 * declare no custom fields, and the screen then renders what it has always
 * rendered. Distinguishing "no config" from "could not reach the host" matters
 * — the first is a working system with nothing extra to ask.
 */
export interface SignupForm {
  readonly active: boolean;
  readonly title?: string;
  readonly description?: string;
  readonly fields: readonly FormField[];
}

const INACTIVE: SignupForm = { active: false, fields: [] };

interface DispatchEnvelope {
  ok?: boolean;
  data?: unknown;
  error?: { code?: string; message?: string };
}

/**
 * Reads the declared signup form.
 *
 * Never throws. A deployment that does not mount the dashboard, a host that is
 * briefly unreachable, or a malformed field list all resolve to "no custom
 * fields" — because the alternative is a registration screen that refuses to
 * render over an optional enhancement to it. The static fields below it are
 * the ones that actually create the account.
 */
export async function signupForm(signal?: AbortSignal): Promise<SignupForm> {
  let payload: DispatchEnvelope;

  try {
    const response = await fetch(DISPATCH, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        envelope: "v1",
        kind: "query",
        contributor: "auth",
        intent: "auth.dynamicConfig",
        payload: {},
        context: {},
      }),
      ...(signal === undefined ? {} : { signal }),
    });

    if (!response.ok) return INACTIVE;

    payload = (await response.json()) as DispatchEnvelope;
  } catch {
    return INACTIVE;
  }

  if (payload.ok !== true) return INACTIVE;

  const data = payload.data as
    { active?: boolean; title?: string; description?: string; fields?: unknown } | undefined;

  if (data?.active !== true) return INACTIVE;

  const fields = usableFields(data.fields);

  // Active with nothing renderable is the same to this screen as inactive.
  if (fields.length === 0) return INACTIVE;

  return {
    active: true,
    ...(data.title === undefined ? {} : { title: data.title }),
    ...(data.description === undefined ? {} : { description: data.description }),
    fields,
  };
}

/**
 * Keeps the fields this screen can actually render, in declared order.
 *
 * `object_array` is dropped rather than approximated. It is a repeating group
 * of sub-fields, and rendering it as a text box would collect a string the
 * server stores under a key that was meant to hold structure — a field that
 * looks answered and is wrong. A field with no key is dropped for the same
 * reason: its value would have nowhere to go.
 */
function usableFields(raw: unknown): readonly FormField[] {
  if (!Array.isArray(raw)) return [];

  const out = (raw as FormField[]).filter(
    (f) =>
      typeof f?.key === "string" &&
      f.key !== "" &&
      typeof f.label === "string" &&
      f.type !== "object_array",
  );

  // Order is the platform's to decide — it is a column on the field.
  return [...out].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
}

/** A field's starting value: its declared default, or empty. */
export function initialValues(fields: readonly FormField[]): Record<string, string> {
  const out: Record<string, string> = {};

  for (const field of fields) {
    // A switch or checkbox is "false" until someone turns it on. Left empty it
    // would submit as absent, which for a required boolean is not the same
    // answer as "no".
    if (field.type === "checkbox" || field.type === "switch") {
      out[field.key] = field.default ?? "false";
      continue;
    }

    out[field.key] = field.default ?? "";
  }

  return out;
}

/**
 * Validates a submission the way the host will.
 *
 * A deliberate second implementation of `formconfig.ValidateSubmission`, and
 * the duplication is the point: the server's copy is the one that decides, and
 * this one exists so a reader is told which box is wrong instead of receiving
 * one sentence about a form they have to re-scan. Rules that disagree fail
 * safe — anything this accepts and the server rejects still surfaces as the
 * server's own message.
 *
 * Returns a message per failing key, empty when the submission passes.
 */
export function validate(
  fields: readonly FormField[],
  values: Record<string, string>,
): Record<string, string> {
  const errors: Record<string, string> = {};

  for (const field of fields) {
    const rules = field.validation ?? {};
    const value = (values[field.key] ?? "").trim();

    if (rules.required === true && value === "") {
      errors[field.key] = `${field.label} is required`;
      continue;
    }

    // An optional field left blank is answered, not skipped.
    if (value === "") continue;

    if (rules.min_len !== undefined && rules.min_len > 0 && value.length < rules.min_len) {
      errors[field.key] = `${field.label} must be at least ${rules.min_len} characters`;
      continue;
    }

    if (rules.max_len !== undefined && rules.max_len > 0 && value.length > rules.max_len) {
      errors[field.key] = `${field.label} must be at most ${rules.max_len} characters`;
      continue;
    }

    if (rules.pattern !== undefined && rules.pattern !== "") {
      // A pattern the platform wrote may not be valid in this engine. The
      // server compiles it too and skips it on failure; matching that here
      // keeps a bad pattern from locking everyone out of registration.
      try {
        if (!new RegExp(rules.pattern).test(value)) {
          errors[field.key] = `${field.label} has an invalid format`;
          continue;
        }
      } catch {
        // Unenforceable here; the host makes the same call.
      }
    }

    if (field.type === "number") {
      const num = Number(value);

      if (!Number.isInteger(num)) {
        errors[field.key] = `${field.label} must be a number`;
        continue;
      }

      if (rules.min !== undefined && num < rules.min) {
        errors[field.key] = `${field.label} must be at least ${rules.min}`;
        continue;
      }

      if (rules.max !== undefined && num > rules.max) {
        errors[field.key] = `${field.label} must be at most ${rules.max}`;
      }
    }
  }

  return errors;
}
