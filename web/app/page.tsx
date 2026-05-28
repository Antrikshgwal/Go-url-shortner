"use client";

import { useState } from "react";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { api, ApiError, ShortenResponse } from "@/lib/api";
import Copy from "@/components/Copy";

export default function Home() {
  const { token, ready } = useAuth();
  const [url, setUrl] = useState("");
  const [result, setResult] = useState<ShortenResponse | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setResult(null);
    if (!url.trim()) return;
    setBusy(true);
    try {
      const res = await api.shorten(url.trim());
      setResult(res);
      setUrl("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="wrap" style={{ paddingTop: 84, paddingBottom: 120 }}>
      {/* hero */}
      <section style={{ display: "grid", gridTemplateColumns: "1fr", gap: 0 }}>


        <h1 className="display rise d2" style={{ marginTop: 26 }}>
          Long links<br />
          collapse into <em>snip.</em>
        </h1>

        <p className="lede rise d3" style={{ marginTop: 28 }}>
          Paste anything sprawling. Get back a short, trackable code with
          real-time click telemetry.
        </p>
      </section>

      {/* shorten console */}
      <section
        className="panel rise d4"
        style={{ marginTop: 56, overflow: "hidden" }}
      >
        <div
          className="row spread"
          style={{
            padding: "16px 24px",
            borderBottom: "1px solid var(--line)",
            background: "var(--ink-2)",
          }}
        >
          <span className="label" style={{ margin: 0 }}>
            <span className="live-dot" style={{ marginRight: 9 }} />
            New short link
          </span>

        </div>

        <form className="panel-pad" onSubmit={submit}>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr auto",
              gap: 12,
            }}
          >
            <input
              className="field"
              type="text"
              inputMode="url"
              placeholder="https://example.com/a/very/long/path?with=params"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            {ready && token ? (
              <button className="btn btn-primary" disabled={busy}>
                {busy ? "Compressing…" : "Compress →"}
              </button>
            ) : (
              <Link className="btn btn-primary" href="/login">
                Sign in to shorten
              </Link>
            )}
          </div>

          {error && (
            <p className="msg msg-err" style={{ marginTop: 16 }}>
              {error}
            </p>
          )}

          {result && (
            <div
              className="rise"
              style={{
                marginTop: 20,
                padding: 20,
                border: "1px solid rgba(200,241,53,0.28)",
                borderRadius: "var(--r)",
                background: "rgba(200,241,53,0.05)",
              }}
            >
              <span className="label acid">Live link</span>
              <div
                className="row spread"
                style={{ gap: 16, flexWrap: "wrap" }}
              >
                <a
                  className="code-link"
                  href={result.short_url}
                  target="_blank"
                  rel="noreferrer"
                  style={{ fontSize: "1.1rem", wordBreak: "break-all" }}
                >
                  {result.short_url}
                </a>
                <div className="row" style={{ gap: 10 }}>
                  <Copy value={result.short_url} />
                  <Link
                    className="btn btn-ghost"
                    href={`/stats/${result.short_code}`}
                    style={{ padding: "9px 14px", fontSize: "0.7rem" }}
                  >
                    Stats
                  </Link>
                </div>
              </div>
            </div>
          )}
        </form>
      </section>

      {/* feature strip */}
      <section
        className="rise d5"
        style={{
          marginTop: 64,
          display: "grid",
          gridTemplateColumns: "repeat(3, 1fr)",
          gap: 1,
          background: "var(--line-soft)",
          border: "1px solid var(--line-soft)",
          borderRadius: 8,
          overflow: "hidden",
        }}
      >
        {[
          { k: "01", t: "Collision-safe codes", d: "Cryptographically random short codes with retry on collision." },
          { k: "02", t: "Cached redirects", d: "Redis-backed lookups keep the hot path fast under load." },
          { k: "03", t: "Click telemetry", d: "Every visit is recorded for per-link analytics." },
        ].map((f) => (
          <div key={f.k} style={{ background: "var(--ink-2)", padding: 28 }}>
            <span className="mono muted" style={{ fontSize: "0.72rem" }}>{f.k}</span>
            <h3
              className="display"
              style={{ fontSize: "1.45rem", margin: "12px 0 8px", lineHeight: 1.1 }}
            >
              {f.t}
            </h3>
            <p className="muted" style={{ fontSize: "0.9rem", lineHeight: 1.55 }}>{f.d}</p>
          </div>
        ))}
      </section>
    </main>
  );
}
