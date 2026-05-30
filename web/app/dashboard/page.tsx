"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { api, ApiError, UrlRow } from "@/lib/api";
import Copy from "@/components/Copy";

export default function Dashboard() {
  const { token, ready } = useAuth();
  const router = useRouter();

  const [rows, setRows] = useState<UrlRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [url, setUrl] = useState("");
  const [alias, setAlias] = useState("");
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState("");

  useEffect(() => {
    if (ready && !token) router.replace("/login");
  }, [ready, token, router]);

  const load = useCallback(async () => {
    try {
      const data = await api.myUrls();
      setRows(Array.isArray(data) ? data : []);
      setError("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to load links");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (ready && token) load();
  }, [ready, token, load]);

  async function shorten(e: React.FormEvent) {
    e.preventDefault();
    if (!url.trim()) return;
    setFormErr("");
    setBusy(true);
    try {
      await api.shorten(url.trim(), alias.trim() || undefined);
      setUrl("");
      setAlias("");
      await load();
    } catch (err) {
      setFormErr(err instanceof ApiError ? err.message : "Failed to shorten");
    } finally {
      setBusy(false);
    }
  }

  async function remove(code: string) {
    const prev = rows;
    setRows((r) => r.filter((x) => x.short_code !== code));
    try {
      await api.remove(code);
    } catch {
      setRows(prev); // restore on failure
    }
  }

  const totalClicks = rows.reduce((s, r) => s + (r.clicks || 0), 0);

  if (!ready || (ready && !token)) return null;

  return (
    <main className="wrap" style={{ paddingTop: 56, paddingBottom: 120 }}>
      {/* header + metrics */}
      <div className="row spread rise d1" style={{ flexWrap: "wrap", gap: 24, alignItems: "flex-end" }}>
        <div>
          <p className="eyebrow">Your console</p>
          <h1 className="display" style={{ fontSize: "clamp(2.2rem,5vw,3.4rem)", marginTop: 18 }}>
            Active <em>links</em>
          </h1>
        </div>
        <div className="row" style={{ gap: 40 }}>
          <Metric value={rows.length} label="Links" />
          <Metric value={totalClicks} label="Total clicks" />
        </div>
      </div>

      {/* inline shorten */}
      <form className="panel panel-pad rise d2" onSubmit={shorten} style={{ marginTop: 40 }}>
        <label className="label">Compress a new URL</label>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 200px auto", gap: 12 }}>
          <input
            className="field"
            placeholder="https://…"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
          <input
            className="field"
            placeholder="custom alias (optional)"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            maxLength={30}
          />
          <button className="btn btn-primary" disabled={busy}>
            {busy ? "…" : "Compress →"}
          </button>
        </div>
        <p className="muted" style={{ marginTop: 10, fontSize: "0.78rem" }}>
          Leave the alias blank for a random 6-char code. Letters, numbers, <code>-</code> and <code>_</code>; 3–30 chars.
        </p>
        {formErr && <p className="msg msg-err" style={{ marginTop: 14 }}>{formErr}</p>}
      </form>

      {/* table */}
      <section className="panel rise d3" style={{ marginTop: 28, overflow: "hidden" }}>
        {loading ? (
          <p className="panel-pad muted mono" style={{ fontSize: "0.85rem" }}>Loading links…</p>
        ) : error ? (
          <p className="panel-pad msg msg-err" style={{ margin: 20 }}>{error}</p>
        ) : rows.length === 0 ? (
          <div className="panel-pad" style={{ textAlign: "center", padding: "64px 24px" }}>
            <p className="display" style={{ fontSize: "1.6rem", marginBottom: 8 }}>No links yet</p>
            <p className="muted">Compress your first URL above to see it here.</p>
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>Code</th>
                  <th>Destination</th>
                  <th style={{ textAlign: "right" }}>Clicks</th>
                  <th className="hide-sm">Created</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.short_code}>
                    <td>
                      <a className="code-link" href={r.short_url} target="_blank" rel="noreferrer">
                        /{r.short_code}
                      </a>
                    </td>
                    <td>
                      <span className="dim truncate" style={{ display: "block" }} title={r.original_url}>
                        {r.original_url}
                      </span>
                    </td>
                    <td style={{ textAlign: "right" }}>
                      <span className="metric" style={{ fontSize: "1.3rem" }}>{r.clicks}</span>
                    </td>
                    <td className="hide-sm muted mono" style={{ fontSize: "0.78rem" }}>
                      {fmtDate(r.created_at)}
                    </td>
                    <td>
                      <div className="row" style={{ gap: 8, justifyContent: "flex-end" }}>
                        <Copy value={r.short_url} label="Copy" />
                        <Link
                          className="btn btn-ghost"
                          href={`/stats/${r.short_code}`}
                          style={{ padding: "9px 13px", fontSize: "0.68rem" }}
                        >
                          Stats
                        </Link>
                        <button
                          className="btn btn-danger"
                          onClick={() => remove(r.short_code)}
                          style={{ padding: "9px 13px", fontSize: "0.68rem" }}
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </main>
  );
}

function Metric({ value, label }: { value: number; label: string }) {
  return (
    <div className="stack">
      <span className="metric acid" style={{ fontSize: "2.6rem" }}>{value}</span>
      <span className="label" style={{ margin: 0 }}>{label}</span>
    </div>
  );
}

function fmtDate(s: string) {
  const d = new Date(s);
  if (isNaN(d.getTime())) return s?.slice(0, 10) || "—";
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
