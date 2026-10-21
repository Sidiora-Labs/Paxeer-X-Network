import assert from "node:assert/strict";
import test from "node:test";

import {
  CURRENT_ROUTES,
  CURRENT_SHELLS,
  REQUIRED_SCREEN_STATES,
  human_state_matrix_registry,
} from "./state-matrix.ts";

test("every current screen has a renderable state matrix in both shells", () => {
  const registry = human_state_matrix_registry();
  registry.assertComplete(CURRENT_ROUTES);
  const screens = registry.enumerate();
  assert.equal(screens.length, CURRENT_ROUTES.length + CURRENT_SHELLS.length);
  assert.deepEqual(Object.keys(screens[0]?.states ?? {}).sort(), [...REQUIRED_SCREEN_STATES].sort());
});
