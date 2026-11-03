import { normalizeName } from "@max/sdk";
import { z } from "zod";

/** Mirrors the API's rules for instant feedback; the API stays the source of truth. */
export const handleSchema = z
  .string()
  .trim()
  .transform((v, ctx) => {
    const n = normalizeName(v);
    if (!n) {
      ctx.addIssue({ code: "custom", message: "Use 3–32 lowercase letters, numbers or hyphens, not starting or ending with a hyphen." });
      return z.NEVER;
    }
    return n;
  });

export function stripPrefix(v: string): string {
  return v.trim().replace(/^max\./i, "");
}
