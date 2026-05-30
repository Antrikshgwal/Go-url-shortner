"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { api, ApiError, SHORT_BASE, Stats, Analytics, AnalyticsBreakdown } from "@/lib/api";
import Copy from "@/components/Copy";

export default function StatsPage() {
  const params = useParams<{ code: string }>();
  const code = params.code;

  const [stats, setStats] = useState<Stats | null>(null);
  const [analytics, setAnalytics] = useState<Analytics | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [rangeDays, setRangeDays] = useState(7);

  useEffect(() => {
    let alive = true;
    (async () => {
      // Fetch stats and analytics independently so a missing/old analytics
      // endpoint doesn't take down the whole page.
      try {
        const s = await api.stats(code);
        if (alive) setStats(s);
      } catch (err) {
        if (alive) setError(err instanceof ApiError ? err.message : "Failed to load stats");
      } finally {
        if (alive) setLoading(false);
      }
      try {
        const a = await api.analytics(code, rangeDays);
        if (alive) setAnalytics(a);
      } catch {
        if (alive) setAnalytics(null); // analytics is optional; silently skip
      }
    })();
    return () => { alive = false; };
  }, [code, rangeDays]);

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

          {/* timeseries chart */}
          {analytics && analytics.timeseries.length > 0 && (
            <div className="panel panel-pad" style={{ marginTop: 16 }}>
              <div className="row spread" style={{ alignItems: "center", marginBottom: 18 }}>
                <span className="label" style={{ margin: 0 }}>Clicks over time</span>
                <div className="row" style={{ gap: 6 }}>
                  {[7, 14, 30].map((d) => (
                    <button
                      key={d}
                      onClick={() => setRangeDays(d)}
                      className={`btn btn-ghost ${rangeDays === d ? "acid" : ""}`}
                      style={{ fontSize: "0.7rem", padding: "4px 10px", opacity: rangeDays === d ? 1 : 0.55 }}
                    >
                      {d}d
                    </button>
                  ))}
                </div>
              </div>
              <Sparkline data={analytics.timeseries} />
              <p className="muted mono" style={{ fontSize: "0.7rem", marginTop: 12, letterSpacing: "0.06em" }}>
                {analytics.total_clicks} clicks · last {analytics.range_days} days
              </p>
            </div>
          )}

          {/* breakdowns */}
          {analytics && (analytics.top_referrers.length > 0 || analytics.browser_breakdown.length > 0) && (
            <div style={{ marginTop: 16, display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(280px,1fr))", gap: 16 }}>
              {analytics.top_referrers.length > 0 && (
                <BreakdownPanel title="Top referrers" rows={analytics.top_referrers} />
              )}
              {analytics.browser_breakdown.length > 0 && (
                <BreakdownPanel title="Browsers" rows={analytics.browser_breakdown} />
              )}
            </div>
          )}

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

function Sparkline({ data }: { data: { bucket: string; clicks: number }[] }) {
  const W = 720, H = 140, P = 6;
  const max = Math.max(1, ...data.map((d) => d.clicks));
  const step = data.length > 1 ? (W - P * 2) / (data.length - 1) : 0;
  const points = data.map((d, i) => {
    const x = P + i * step;
    const y = H - P - (d.clicks / max) * (H - P * 2);
    return [x, y] as const;
  });
  const line = points.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `${line} L${points[points.length - 1][0].toFixed(1)},${H - P} L${points[0][0].toFixed(1)},${H - P} Z`;

  return (
    <svg viewBox={`0 0 ${W} ${H}`} width="100%" height={H} preserveAspectRatio="none" style={{ display: "block" }}>
      <defs>
        <linearGradient id="spark-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="rgba(212,255,77,0.28)" />
          <stop offset="100%" stopColor="rgba(212,255,77,0)" />
        </linearGradient>
      </defs>
      <path d={area} fill="url(#spark-fill)" />
      <path d={line} fill="none" stroke="#d4ff4d" strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
      {points.map(([x, y], i) => (
        <circle key={i} cx={x} cy={y} r="2.2" fill="#d4ff4d" opacity={data[i].clicks > 0 ? 1 : 0.25} />
      ))}
    </svg>
  );
}

function BreakdownPanel({ title, rows }: { title: string; rows: AnalyticsBreakdown[] }) {
  const total = rows.reduce((s, r) => s + r.clicks, 0) || 1;
  return (
    <div className="panel panel-pad">
      <span className="label" style={{ marginBottom: 16, display: "block" }}>{title}</span>
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {rows.map((r, i) => {
          const pct = (r.clicks / total) * 100;
          return (
            <div key={i}>
              <div className="row spread" style={{ marginBottom: 4, fontSize: "0.82rem" }}>
                <span className="mono dim truncate" style={{ maxWidth: "70%" }} title={r.label}>{r.label}</span>
                <span className="mono muted">{r.clicks}</span>
              </div>
              <div style={{ height: 3, background: "rgba(255,255,255,0.06)", borderRadius: 2, overflow: "hidden" }}>
                <div style={{ width: `${pct}%`, height: "100%", background: "#d4ff4d" }} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
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
