// Output-schema builder for workflow nodes (spec 495). Each builder returns a
// JSON Schema value plus a phantom TypeScript type, so `node.out.<field>` is
// typed in the DSL while the IR carries plain JSON Schema — the same schema a
// node's `submit_result` call would be validated against.

export type JsonSchema =
  | { type: 'string'; enum?: string[] }
  | { type: 'number' }
  | { type: 'boolean' }
  | { type: 'array'; items: JsonSchema }
  | {
      type: 'object';
      properties: Record<string, JsonSchema>;
      required: string[];
      additionalProperties: false;
    };

export interface S<T> {
  readonly json: JsonSchema;
  // Phantom: never set at run time, only carries T for inference.
  readonly __type?: T;
}

export type Infer<X> = X extends S<infer T> ? T : never;

export type ObjectSchema = Extract<JsonSchema, { type: 'object' }>;

export const s = {
  string: (): S<string> => ({ json: { type: 'string' } }),
  number: (): S<number> => ({ json: { type: 'number' } }),
  boolean: (): S<boolean> => ({ json: { type: 'boolean' } }),
  enum: <const V extends readonly [string, ...string[]]>(...values: V): S<V[number]> => ({
    json: { type: 'string', enum: [...values] },
  }),
  array: <T>(items: S<T>): S<T[]> => ({ json: { type: 'array', items: items.json } }),
  object: <P extends Record<string, S<unknown>>>(props: P): S<{ [K in keyof P]: Infer<P[K]> }> => {
    const properties: Record<string, JsonSchema> = {};
    for (const [k, v] of Object.entries(props)) properties[k] = v.json;
    return { json: { type: 'object', properties, required: Object.keys(props), additionalProperties: false } };
  },
};
