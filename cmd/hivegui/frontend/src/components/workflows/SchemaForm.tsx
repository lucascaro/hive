// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A form generated from the engine's JSON Schema subset. Controlled from a
// raw-value record the caller keeps in the store (store/workflows.ts), so
// unmounting the form never loses typed input. Field names are workflow
// text and render as React text. Markup follows components.md > Form fields.

import { type ReactNode, useId } from 'react';
import {
  type Field,
  type Raw,
  describe,
  initialRaw,
} from '../../lib/schema-form.js';
import type { JsonSchema } from '../../lib/workflow-types.js';
import { plainText } from '../../lib/workflow-text.js';

type Values = { [name: string]: Raw };

export function SchemaForm({
  schema,
  value,
  onChange,
}: {
  schema: JsonSchema;
  /** Undefined until the user types; the form shows the schema's blanks. */
  value: Values | undefined;
  onChange: (v: Values) => void;
}): ReactNode {
  const fields = describe(schema);
  const values = { ...initialRaw(fields), ...(value ?? {}) };
  return (
    <FieldList fields={fields} values={values} onChange={onChange} depth={0} />
  );
}

function FieldList({
  fields,
  values,
  onChange,
  depth,
}: {
  fields: Field[];
  values: Values;
  onChange: (v: Values) => void;
  depth: number;
}): ReactNode {
  return (
    <>
      {fields.map((f) => (
        <FieldView
          key={f.name}
          field={f}
          raw={values[f.name]}
          depth={depth}
          onChange={(r) => onChange({ ...values, [f.name]: r })}
        />
      ))}
    </>
  );
}

function FieldView({
  field: f,
  raw,
  depth,
  onChange,
}: {
  field: Field;
  raw: Raw | undefined;
  depth: number;
  onChange: (r: Raw) => void;
}): ReactNode {
  const id = useId();
  const text = typeof raw === 'string' ? raw : '';
  const label = (
    <label className="hv-field__label" htmlFor={id}>
      {plainText(f.name)}
      {f.required ? ' *' : ''}
    </label>
  );
  switch (f.kind) {
    case 'boolean':
      return (
        <div className="hv-field hv-field--check">
          <label className="hv-field__label hv-field__check">
            <input
              id={id}
              type="checkbox"
              checked={raw === true}
              onChange={(e) => onChange(e.target.checked)}
            />
            <span>{plainText(f.name)}</span>
          </label>
        </div>
      );
    case 'enum': {
      const opts = f.options ?? [];
      if (opts.length <= 4) {
        return (
          <fieldset className="hv-field hv-field--group">
            <legend className="hv-field__label">
              {plainText(f.name)}
              {f.required ? ' *' : ''}
            </legend>
            {opts.map((o) => (
              <label key={o} className="hv-field__check">
                <input
                  type="radio"
                  name={id}
                  value={o}
                  checked={text === o}
                  required={f.required}
                  onChange={() => onChange(o)}
                />
                <span>{plainText(o)}</span>
              </label>
            ))}
          </fieldset>
        );
      }
      return (
        <div className="hv-field">
          {label}
          <select
            id={id}
            className="hv-input"
            value={text}
            required={f.required}
            onChange={(e) => onChange(e.target.value)}
          >
            <option value="">-</option>
            {opts.map((o) => (
              <option key={o} value={o}>
                {plainText(o)}
              </option>
            ))}
          </select>
        </div>
      );
    }
    case 'number':
      return (
        <div className="hv-field">
          {label}
          <input
            id={id}
            className="hv-input"
            type="number"
            step="any"
            value={text}
            required={f.required}
            onChange={(e) => onChange(e.target.value)}
          />
        </div>
      );
    case 'list':
      return (
        <div className="hv-field">
          {label}
          <textarea
            id={id}
            className="hv-input"
            rows={3}
            value={text}
            required={f.required}
            onChange={(e) => onChange(e.target.value)}
          />
          <span className="hv-field__hint">One value per line.</span>
        </div>
      );
    case 'json':
      return (
        <div className="hv-field">
          {label}
          <textarea
            id={id}
            className="hv-input hv-input--mono"
            rows={4}
            value={text}
            required={f.required}
            spellCheck={false}
            onChange={(e) => onChange(e.target.value)}
          />
          <span className="hv-field__hint">JSON.</span>
        </div>
      );
    case 'object': {
      const nested: Values =
        typeof raw === 'object' && raw ? raw : initialRaw(f.fields ?? []);
      return (
        <fieldset className="hv-field hv-field--group" data-depth={depth}>
          <legend className="hv-field__label">{plainText(f.name)}</legend>
          <FieldList
            fields={f.fields ?? []}
            values={nested}
            onChange={onChange}
            depth={depth + 1}
          />
        </fieldset>
      );
    }
    default:
      return (
        <div className="hv-field">
          {label}
          <input
            id={id}
            className="hv-input"
            type="text"
            value={text}
            required={f.required}
            onChange={(e) => onChange(e.target.value)}
          />
        </div>
      );
  }
}
