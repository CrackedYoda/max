import { MaxClient } from '@max/sdk';

import { getEnv } from '@/config/env';

let client: MaxClient | null = null;

/**
 * One SDK client for the app. Native apps authenticate with a Bearer session
 * token (tokenDelivery "body"), kept in SecureStore by services/session.
 */
export function max(): MaxClient {
  if (!client) client = new MaxClient({ baseUrl: getEnv().apiUrl });
  return client;
}
