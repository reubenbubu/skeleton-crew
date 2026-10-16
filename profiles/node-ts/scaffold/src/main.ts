import { createServer } from "./server.ts";

const port = Number(process.env.PORT ?? 3000);
const server = createServer().listen(port, () => {
  console.log(`listening on ${port}`);
});
process.on("SIGTERM", () => server.close(() => process.exit(0)));
