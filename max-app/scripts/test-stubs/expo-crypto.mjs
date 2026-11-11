import { randomBytes, randomUUID as uuid } from 'node:crypto';

export const randomUUID = () => uuid();
export const getRandomBytes = (n) => new Uint8Array(randomBytes(n));
