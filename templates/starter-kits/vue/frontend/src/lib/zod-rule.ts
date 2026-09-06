import type { ZodType } from 'zod'

// zodRule adapts one Zod field schema to VeeValidate's rule result contract.
export function zodRule(schema: ZodType): (value: unknown) => true | string {
  return (value: unknown) => {
    const result = schema.safeParse(value)
    return result.success ? true : result.error.issues[0].message
  }
}
