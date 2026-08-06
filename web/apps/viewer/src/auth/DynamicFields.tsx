/**
 * Rendering the fields a deployment declared.
 *
 * Every control here is the same size and weight as the static ones beside it.
 * A declared field is not a lesser field — an operator filling in "control
 * area" should not be able to tell that it arrived over the wire while their
 * email did not.
 *
 * Values are carried as strings throughout, because that is what the platform
 * stores: `User.Metadata` is `map[string]string`, and a boolean or a number
 * converted here would only be converted back before it was sent.
 */

import type { ReactElement } from "react";

import type { FormField } from "./formconfig";

const CONTROL =
  "h-8 w-full rounded-[3px] border border-line-2 bg-panel-2 px-2.5 font-mono text-[12px] " +
  "text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 " +
  "focus-visible:outline-fg-2";

const LABEL = "font-mono text-[9px] uppercase tracking-[0.13em] text-fg-3";

/** Which HTML input type a declared field maps to. */
const INPUT_TYPE: Record<string, string> = {
  text: "text",
  email: "email",
  number: "number",
  tel: "tel",
  url: "url",
  date: "date",
};

export function DynamicFields({
  fields,
  values,
  errors,
  onChange,
  disabled,
}: {
  readonly fields: readonly FormField[];
  readonly values: Record<string, string>;
  /** Per-key messages, from the client's own check or the host's refusal. */
  readonly errors: Record<string, string>;
  readonly onChange: (key: string, value: string) => void;
  readonly disabled: boolean;
}): ReactElement {
  return (
    <>
      {fields.map((field) => {
        const value = values[field.key] ?? "";
        const problem = errors[field.key];
        const invalid = problem !== undefined;

        // The message is bound to the control rather than merely placed near
        // it, so a screen reader announces which field it belongs to.
        const describedBy = invalid ? `${field.key}-error` : undefined;

        return (
          <div key={field.key} className="flex flex-col gap-1.5">
            <Control
              field={field}
              value={value}
              invalid={invalid}
              disabled={disabled}
              describedBy={describedBy}
              onChange={onChange}
            />

            {field.description !== undefined && field.description !== "" && !invalid && (
              <p className="text-[10px] leading-snug text-fg-3">{field.description}</p>
            )}

            {invalid && (
              <p id={describedBy} className="text-[10.5px] leading-snug text-alarm">
                {problem}
              </p>
            )}
          </div>
        );
      })}
    </>
  );
}

function Control({
  field,
  value,
  invalid,
  disabled,
  describedBy,
  onChange,
}: {
  readonly field: FormField;
  readonly value: string;
  readonly invalid: boolean;
  readonly disabled: boolean;
  readonly describedBy: string | undefined;
  readonly onChange: (key: string, value: string) => void;
}): ReactElement {
  const required = field.validation?.required === true;

  // aria-required rather than the HTML attribute, and the distinction is
  // load-bearing. Native constraint validation blocks submit before the form's
  // own handler runs, so a browser bubble saying "please fill in this field"
  // would appear instead of the message this screen composed — and the
  // platform's richer rules (min_len, pattern, integer bounds) would never be
  // checked at all. The credential fields keep native `required` because their
  // rule is exactly what the browser implements; a declared field's is not.
  const shared = {
    id: field.key,
    "aria-required": required,
    disabled,
    "aria-invalid": invalid,
    ...(describedBy === undefined ? {} : { "aria-describedby": describedBy }),
    className: invalid ? `${CONTROL} border-alarm` : CONTROL,
  };

  const label = (
    <label htmlFor={field.key} className={LABEL}>
      {field.label}
      {!required && <span className="ml-1.5 normal-case tracking-normal">(optional)</span>}
    </label>
  );

  // A boolean reads as a statement to agree with, so its label sits beside the
  // box rather than above it.
  if (field.type === "checkbox" || field.type === "switch") {
    return (
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          id={field.key}
          aria-required={required}
          disabled={disabled}
          aria-invalid={invalid}
          {...(describedBy === undefined ? {} : { "aria-describedby": describedBy })}
          checked={value === "true"}
          onChange={(e) => onChange(field.key, e.target.checked ? "true" : "false")}
          className="size-3.5 rounded-[2px] border border-line-2 bg-panel-2 accent-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
        />
        <span className="text-[11.5px] text-fg-2">{field.label}</span>
      </label>
    );
  }

  if (field.type === "textarea") {
    return (
      <>
        {label}
        <textarea
          {...shared}
          rows={3}
          {...(field.placeholder === undefined ? {} : { placeholder: field.placeholder })}
          value={value}
          onChange={(e) => onChange(field.key, e.target.value)}
          className={`${shared.className} h-auto py-2 leading-snug`}
        />
      </>
    );
  }

  if (field.type === "select") {
    return (
      <>
        {label}
        <select {...shared} value={value} onChange={(e) => onChange(field.key, e.target.value)}>
          {/* Present even when the field is required: a select that opens
              already showing the first option collects an answer nobody
              chose. */}
          <option value="">Select…</option>
          {(field.options ?? []).map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </>
    );
  }

  if (field.type === "radio") {
    return (
      <>
        <span className={LABEL}>{field.label}</span>
        <div
          role="radiogroup"
          aria-label={field.label}
          {...(describedBy === undefined ? {} : { "aria-describedby": describedBy })}
          className="flex flex-col gap-1.5 pt-0.5"
        >
          {(field.options ?? []).map((option) => (
            <label key={option.value} className="flex items-center gap-2">
              <input
                type="radio"
                name={field.key}
                value={option.value}
                disabled={disabled}
                checked={value === option.value}
                onChange={() => onChange(field.key, option.value)}
                className="size-3.5 accent-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-fg-2"
              />
              <span className="text-[11.5px] text-fg-2">{option.label}</span>
            </label>
          ))}
        </div>
      </>
    );
  }

  return (
    <>
      {label}
      <input
        {...shared}
        // Anything unrecognised is a text box. A field type this build has not
        // been taught is still a field somebody must fill in, and refusing to
        // render it would block registration outright.
        type={INPUT_TYPE[field.type] ?? "text"}
        {...(field.placeholder === undefined ? {} : { placeholder: field.placeholder })}
        {...(field.validation?.min === undefined ? {} : { min: field.validation.min })}
        {...(field.validation?.max === undefined ? {} : { max: field.validation.max })}
        value={value}
        onChange={(e) => onChange(field.key, e.target.value)}
      />
    </>
  );
}
