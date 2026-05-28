"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { api, ApiError, SHORT_BASE, Stats } from "@/lib/api";
import Copy from "@/components/Copy";

export default function StatsPage() {
  const params = useParams<{ code: string }>();
  const code = params.code;

  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const s = await api.stats(code);
        if (alive) setStats(s);
      } catch (err) {
        if (alive) setError(err instanceof ApiError ? err.message : "Failed to load stats");
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => { alive = false; };
  }, [code]);

  const shortUrl = `${SHORT_BASE}/${code}`;

  return (
    <main className="wrap" style={{ paddingTop: 56, paddingBottom: 120, maxWidth: 860 }}>
      <Link href="/dashboard" className="mono muted rise d1" style={{ fontSize: "0.74rem", letterSpacing: "0.08em" }}>
        ← Back to console
      </Link>

      <div className="rise d2" style={{ marginTop: 22 }}>
        <p className="eyebrow">Link telemetry</p>
        <h1 className="display" style={{ fontSize: "clamp(2.2rem,6vw,4rem)", marginTop: 16 }}>
          <em>/{code}</em>
        </h1>
      </div>

      {loading ? (
        <p className="muted mono rise d3" style={{ marginTop: 40, fontSize: "0.85rem" }}>Loading telemetry…</p>
      ) : error ? (
        <p className="msg msg-err rise d3" style={{ marginTop: 40 }}>{error}</p>
      ) : stats ? (
        <div className="rise d3" style={{ marginTop: 44 }}>
          {/* big clicks metric */}
          <div className="panel panel-pad" style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between", gap: 24, flexWrap: "wrap" }}>
            <div>
              <span className="label">Total clicks</span>
              <div className="metric acid" style={{ fontSize: "clamp(3.5rem,12vw,7rem)" }}>
                {stats.clicks}
              </div>
            </div>
            <div className="row" style={{ gap: 10 }}>
              <Copy value={shortUrl} label="Copy link" />
              <a className="btn btn-ghost" href={shortUrl} target="_blank" rel="noreferrer" style={{ fontSize: "0.7rem" }}>
                Open ↗
              </a>
            </div>
          </div>

          {/* detail rows */}
          <div className="panel" style={{ marginTop: 16 }}>
            <Detail label="Short code" value={`/${stats.short_code}`} mono />
            <hr className="rule" />
            <Detail label="Destination" value={stats.original_url} mono link />
            <hr className="rule" />
            <Detail label="Created" value={fmtDateTime(stats.created_at)} />
          </div>
        </div>
      ) : null}
    </main>
  );
}

function Detail({ label, value, mono, link }: { label: string; value: string; mono?: boolean; link?: boolean }) {
  return (
    <div className="panel-pad" style={{ display: "grid", gridTemplateColumns: "160px 1fr", gap: 16, alignItems: "center" }}>
      <span className="label" style={{ margin: 0 }}>{label}</span>
      {link ? (
        <a href={value} target="_blank" rel="noreferrer" className={mono ? "mono acid" : "acid"} style={{ wordBreak: "break-all", fontSize: "0.92rem" }}>
          {value}
        </a>
      ) : (
        <span className={mono ? "mono dim" : "dim"} style={{ wordBreak: "break-all", fontSize: "0.92rem" }}>{value}</span>
      )}
    </div>
  );
}

function fmtDateTime(s: string) {
  const d = new Date(s);
  if (isNaN(d.getTime())) return s || "—";
  return d.toLocaleString(undefined, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
