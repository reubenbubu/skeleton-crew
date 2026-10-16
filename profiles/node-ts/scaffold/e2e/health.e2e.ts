import assert from "node:assert/strict";
import { test } from "node:test";

const base = process.env.BASE_URL ?? "http://localhost:3000";

test("Scaffold: the running app is healthy", async () => {
  const res = await fetch(`${base}/health`);
  assert.equal(res.status, 200);
});
