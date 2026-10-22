// The cookie policy page controls and the global banner share one consent store (issue #710).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import { cookieConsentStore } from "../src/hooks/ui/useCookieConsent";

describe("cookie consent store", () => {
  it("a choice from one consumer is seen by every consumer", () => {
    let calls = 0;
    const unsubscribe = cookieConsentStore.subscribe(() => {
      calls += 1;
    });

    assert.equal(cookieConsentStore.getState().loading, true);
    cookieConsentStore.getState().hydrate();
    assert.equal(cookieConsentStore.getState().value, null);
    assert.equal(cookieConsentStore.getState().loading, false);

    const before = calls;
    cookieConsentStore.getState().decline();
    assert.equal(cookieConsentStore.getState().value, "declined");
    assert.ok(calls > before, "listener fired on decline");

    const afterDecline = calls;
    cookieConsentStore.getState().accept();
    assert.equal(cookieConsentStore.getState().value, "accepted");
    assert.ok(calls > afterDecline, "listener fired on accept");

    cookieConsentStore.getState().hydrate();
    assert.equal(cookieConsentStore.getState().value, "accepted", "hydrate after a choice keeps it");

    unsubscribe();
  });
});
