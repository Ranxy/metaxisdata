import { describe, expect, it } from "vitest";
import { slugify } from "@/utils/slug";

describe("slugify", () => {
  it("lower-cases and hyphenates a title", () => {
    expect(slugify("Acme Corp SSO")).toBe("acme-corp-sso");
  });

  it("collapses runs of separators and trims the edges", () => {
    expect(slugify("  Acme   --  Corp!  ")).toBe("acme-corp");
  });

  it("keeps digits", () => {
    expect(slugify("Okta 2")).toBe("okta-2");
  });

  it("strips accents rather than the letter they sit on", () => {
    expect(slugify("Café Général")).toBe("cafe-general");
  });

  it("returns an empty string when the title has no ASCII letters or digits", () => {
    // The caller has to ask for an id in this case: an empty string is not a
    // valid resource id.
    expect(slugify("企业单点登录")).toBe("");
  });

  it("caps the length at 63 characters and leaves no trailing hyphen", () => {
    const slug = slugify("a".repeat(80));
    expect(slug).toHaveLength(63);
    expect(slug.endsWith("-")).toBe(false);

    // A title whose 63rd character would land on a separator must not keep the
    // hyphen either.
    const truncated = slugify(`${"b".repeat(62)} tail`);
    expect(truncated).toBe("b".repeat(62));
  });
});
