/**
 * Turns a title into a resource id: lower-case ASCII words joined by single
 * hyphens, at most 63 characters.
 *
 * It is deliberately lossy. A title that carries no ASCII letters or digits —
 * a Chinese one, for instance — slugifies to an empty string, which the caller
 * has to treat as "ask the user for an id" rather than as a valid id.
 */
export function slugify(value: string): string {
  return (
    value
      .normalize("NFKD")
      // Split accented letters into base letter plus combining mark, then drop the
      // marks: "Café" becomes "cafe", not "caf".
      .replace(/[\u0300-\u036f]/g, "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 63)
      .replace(/-+$/g, "")
  );
}
