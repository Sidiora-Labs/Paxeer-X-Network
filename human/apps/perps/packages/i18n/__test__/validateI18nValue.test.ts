import { validateI18nValue, i18nValidErrors } from "../scripts/utils";

describe("validateI18nValue", () => {
  it("should return error for non-string input", () => {
    expect(validateI18nValue(123 as any)).toEqual({
      valid: false,
      error: i18nValidErrors.string,
    });
    expect(validateI18nValue(null as any)).toEqual({
      valid: false,
      error: i18nValidErrors.string,
    });
    expect(validateI18nValue(undefined as any)).toEqual({
      valid: false,
      error: i18nValidErrors.string,
    });
  });

  it("should return error for empty string", () => {
    expect(validateI18nValue("")).toEqual({
      valid: false,
      error: i18nValidErrors.empty,
    });
    expect(validateI18nValue("   ")).toEqual({
      valid: false,
      error: i18nValidErrors.empty,
    });
  });

  it("should validate interpolation correctly", () => {
    expect(validateI18nValue("Hello, {{name}}!")).toEqual({
      valid: true,
      error: null,
    });

    expect(validateI18nValue("{{user.name}}")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("{{ user.name }}")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("Invalid {{ name ")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });
    expect(validateI18nValue("{{user.name")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });

    expect(validateI18nValue("Invalid name}}")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });
    expect(validateI18nValue("user.name }}")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });

    // Lone `}` is allowed (e.g. embedded JS); invalid `}}` / `{{` are caught above.
    expect(validateI18nValue("Invalid name}")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("Invalid name }")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("{Invalid name")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });

    expect(validateI18nValue("{ Invalid name")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });
    expect(validateI18nValue("{ user.name }")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });
    expect(validateI18nValue("{user.name}")).toEqual({
      valid: false,
      error: i18nValidErrors.interpolation,
    });
  });

  it("should validate HTML tags correctly", () => {
    // valid tag
    expect(validateI18nValue("<b>{{user}}</b>")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("<strong>Hello</strong>")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("<1>Hello</1>")).toEqual({
      valid: true,
      error: null,
    });
    expect(validateI18nValue("<0/><1>Text</1>")).toEqual({
      valid: true,
      error: null,
    });

    // invalid tag
    expect(validateI18nValue("<strong>Hello")).toEqual({
      valid: false,
      error: i18nValidErrors.unclosedTag("strong"),
    });
    expect(validateI18nValue("<div><span>Hello</div>")).toEqual({
      valid: false,
      error: i18nValidErrors.mismatchedClosingTag("div"),
    });
  });

  it("should handle complex combinations of placeholders and tags", () => {
    expect(validateI18nValue("<b>Hello {{user}}</b>")).toEqual({
      valid: true,
      error: null,
    });
    expect(
      validateI18nValue(
        "<div><span>{{name}}</span> <strong>{{age}}</strong></div>",
      ),
    ).toEqual({
      valid: true,
      error: null,
    });
  });
});
