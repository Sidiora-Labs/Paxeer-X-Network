import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("the web scaffold declares both human-interface planes", async () => {
  const source = await readFile(new URL("../src/app/scaffold.ts", import.meta.url), "utf8");
  assert.match(source, /"\/app"/u);
  assert.match(source, /"\/explorer"/u);
});
