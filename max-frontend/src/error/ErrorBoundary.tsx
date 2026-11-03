"use client";

import { Component, type ReactNode } from "react";

type State = { error: Error | null };

/** Last-resort boundary: says nothing was sent, offers a reload. */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error("[max] render error", error);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <main className="mx-auto flex max-w-[640px] flex-col gap-4 px-6 py-24">
        <h1 className="m-0 font-display text-[40px] font-medium">Something went wrong</h1>
        <p className="m-0 text-[17px] text-muted">This page hit an error. Nothing was sent or signed.</p>
        <button type="button" onClick={() => location.reload()}
          className="self-start rounded-xl bg-clay px-6 py-3 font-semibold text-white hover:bg-clay-hover">Reload</button>
      </main>
    );
  }
}
