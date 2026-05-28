"use client";

import { useState } from "react";

export default function Copy({ value, label = "Copy" }: { value: string; label?: string }) {
  const [done, setDone] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setDone(true);
      setTimeout(() => setDone(false), 1400);
    } catch {
      /* clipboard blocked */
    }
  }

  return (
    <button className="btn btn-ghost" onClick={copy} style={{ padding: "9px 14px", fontSize: "0.7rem" }}>
      {done ? "Copied ✓" : label}
    </button>
  );
}
