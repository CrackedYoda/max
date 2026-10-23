#!/usr/bin/env node
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { MaxClient } from "@max/sdk";

import { createMaxMcpServer } from "./server.js";

// stdout carries the MCP protocol; diagnostics go to stderr only.
const log = (...a: unknown[]) => console.error("[max-mcp]", ...a);

async function main() {
  const baseUrl = process.env.MAX_API_URL;
  if (!baseUrl) throw new Error("MAX_API_URL is required");
  const apiKey = process.env.MAX_AGENT_KEY;
  const sender = process.env.MAX_AGENT_SENDER;

  const client = new MaxClient({ baseUrl });
  let agentClient: MaxClient | undefined;
  if (apiKey) {
    if (!sender) throw new Error("MAX_AGENT_SENDER is required with MAX_AGENT_KEY");
    agentClient = new MaxClient({ baseUrl, apiKey });
    const me = await agentClient.agent.me();
    for (const forbidden of ["payments:submit", "authorizations:read"]) {
      if (me.scopes.includes(forbidden as never)) {
        log(`warning: this key has ${forbidden}; give the MCP server a key with only payments:propose and payments:read`);
      }
    }
    log(`agent ${me.agentId} (scopes: ${me.scopes.join(", ")}); proposals send from ${sender}`);
  } else {
    log("no MAX_AGENT_KEY: read-only tools only");
  }

  const server = createMaxMcpServer({ client, agentClient, sender });
  await server.connect(new StdioServerTransport());
}

main().catch((err) => {
  log(err instanceof Error ? err.message : err);
  process.exit(1);
});
