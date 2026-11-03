"use client";

import { useEffect, useState } from "react";

import { discoverEvmProviders, type EvmProviderInfo } from "@/services/wallets";

export function useEvmProviders(): EvmProviderInfo[] {
  const [list, setList] = useState<EvmProviderInfo[]>([]);
  useEffect(() => discoverEvmProviders(setList), []);
  return list;
}
