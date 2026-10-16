import assert from "node:assert/strict";
import { once } from "node:events";
import type { AddressInfo } from "node:net";
import { test } from "node:test";
import { createServer } from "./server.ts";

test("Scaffold: the server answers its health check", async () => {
  const server = createServer().listen(0);
  await once(server, "listening");
  const { port } = server.address() as AddressInfo;
  try {
    const res = await fetch(`http://localhost:${port}/health`);
    assert.equal(res.status, 200);
  } finally {
    server.close();
  }
});
