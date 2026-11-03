import { MaxClient } from "@max/sdk";

import { getEnv } from "@/config/env";

let client: MaxClient | null = null;

/** One SDK client per tab. Browsers authenticate with the HttpOnly session cookie. */
export function max(): MaxClient {
  if (!client) client = new MaxClient({ baseUrl: getEnv().apiUrl, credentials: "include" });
  return client;
}
