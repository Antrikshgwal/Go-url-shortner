"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { api, ApiError } from "@/lib/api";

export default function LoginPage() {
  const { token, ready, signIn } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPw, setShowPw] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (ready && token) router.replace("/dashboard");
  }, [ready, token, router]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const res = await api.login(email.trim(), password);
      signIn(res.token);
      router.push("/dashboard");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Sign in failed");
      setBusy(false);
    }
  }

  return (
    <div className="auth-shell">
      <aside className="auth-aside">
        <div className="rise d1">

          <h1 className="display" style={{ fontSize: "clamp(2.4rem,5vw,4rem)", marginTop: 24 }}>
            Back to the<br /><em>console.</em>
          </h1>
        </div>

      </aside>

      <main className="auth-main">
        <div className="rise d2" style={{ width: "100%", maxWidth: 380 }}>
          <h2 className="display" style={{ fontSize: "2rem", marginBottom: 6 }}>Sign in</h2>
          <p className="muted" style={{ marginBottom: 30, fontSize: "0.92rem" }}>
            No account? <Link href="/register" className="acid">Request access</Link>
          </p>

          <form className="stack" style={{ gap: 18 }} onSubmit={submit}>
            <div>
              <label className="label">Email</label>
              <input
                className="field"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
               
                autoComplete="email"
              />
            </div>
            <div>
              <label className="label">Password</label>
              <div style={{ position: "relative" }}>
                <input
                  className="field"
                  type={showPw ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  autoComplete="current-password"
                  style={{ paddingRight: 64 }}
                />
                <button
                  type="button"
                  onClick={() => setShowPw((s) => !s)}
                  aria-label={showPw ? "Hide password" : "Show password"}
                  className="mono muted"
                  style={{ position: "absolute", right: 12, top: "50%", transform: "translateY(-50%)", background: "transparent", border: 0, padding: 0, fontSize: "0.7rem", letterSpacing: "0.08em", cursor: "pointer" }}
                >
                  {showPw ? "HIDE" : "SHOW"}
                </button>
              </div>
            </div>

            {error && <p className="msg msg-err">{error}</p>}

            <button className="btn btn-primary" disabled={busy} style={{ marginTop: 6 }}>
              {busy ? "Authenticating…" : "Enter console →"}
            </button>
          </form>
        </div>
      </main>
    </div>
  );
}
