export const API_BASE = (process.env.NEXT_PUBLIC_API_URL || "/api").replace(/\/$/, "");
// Public origin used to display/open short links (the real backend domain).
export const SHORT_BASE = (process.env.NEXT_PUBLIC_SHORT_BASE || "https://snip-seo2.onrender.com").replace(/\/$/, "");
const TOKEN_KEY = "signal.token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}
export function setToken(token: string) {
  window.localStorage.setItem(TOKEN_KEY, token);
}
export function clearToken() {
  window.localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

type Opts = { method?: string; body?: unknown; auth?: boolean };

async function request<T>(path: string, opts: Opts = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.auth) {
    const t = getToken();
    if (t) headers["Authorization"] = `Bearer ${t}`;
  }

  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method: opts.method || "GET",
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    });
  } catch {
    throw new ApiError("Can't reach the server. Is the API running?", 0);
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  let data: any = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }

  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || `Request failed (${res.status})`;
    throw new ApiError(msg, res.status);
  }
  return data as T;
}

// ---- types ----
export interface AuthResponse { token: string; user_id?: number; email?: string }
export interface ShortenResponse { short_code: string; short_url: string }
export interface UrlRow {
  short_code: string;
  original_url: string;
  created_at: string;
  clicks: number;
  short_url: string;
}
export interface Stats {
  short_code: string;
  original_url: string;
  clicks: number;
  created_at: string;
}

// ---- endpoints ----
export const api = {
  register: (username: string, email: string, password: string) =>
    request<AuthResponse>("/register", { method: "POST", body: { username, email, password } }),

  login: (email: string, password: string) =>
    request<AuthResponse>("/login", { method: "POST", body: { email, password } }),

  shorten: (url: string, alias?: string) =>
    request<ShortenResponse>("/shorten", {
      method: "POST",
      auth: true,
      body: alias ? { url, alias } : { url },
    }),

  myUrls: () => request<UrlRow[]>("/my-urls", { auth: true }),

  stats: (code: string) => request<Stats>(`/stats/${encodeURIComponent(code)}`, {}),

  remove: (code: string) =>
    request<void>(`/${encodeURIComponent(code)}`, { method: "DELETE", auth: true }),
};
