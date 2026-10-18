import { describe, expect, it } from "vitest";
import { isWithinCalendarMonths, isWithinDays } from "./calendarRange";

describe("calendar month range", () => {
  it("allows the same date three calendar months later", () => {
    expect(
      isWithinCalendarMonths(new Date(2026, 5, 28), new Date(2026, 8, 28), 3),
    ).toBe(true);
    expect(
      isWithinCalendarMonths(new Date(2026, 5, 28), new Date(2026, 8, 29), 3),
    ).toBe(false);
  });

  it("handles month-end clamping and a reversed selection", () => {
    expect(
      isWithinCalendarMonths(new Date(2024, 0, 31), new Date(2024, 3, 30), 3),
    ).toBe(true);
    expect(
      isWithinCalendarMonths(new Date(2024, 4, 1), new Date(2024, 0, 31), 3),
    ).toBe(false);
  });
});

describe("day range", () => {
  it("allows exactly 90 days and rejects 91 days", () => {
    expect(isWithinDays(new Date(2026, 6, 2), new Date(2026, 8, 29), 90)).toBe(
      true,
    );
    expect(isWithinDays(new Date(2026, 6, 1), new Date(2026, 8, 29), 90)).toBe(
      false,
    );
  });

  it("handles a reversed selection", () => {
    expect(isWithinDays(new Date(2026, 8, 29), new Date(2026, 6, 2), 90)).toBe(
      true,
    );
    expect(isWithinDays(new Date(2026, 8, 29), new Date(2026, 6, 1), 90)).toBe(
      false,
    );
  });
});
