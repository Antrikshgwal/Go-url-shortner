"use client";

import { createContext, useContext, useEffect, useState, useCallback, ReactNode } from "react";
import { getToken, setToken as persist, clearToken } from "./api";

interface AuthState {
  token: string | null;
  ready: boolean;
  signIn: (token: string) => void;
  signOut: () => void;
}

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTok] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setTok(getToken());
    setReady(true);
  }, []);

  const signIn = useCallback((t: string) => {
    persist(t);
    setTok(t);
  }, []);

  const signOut = useCallback(() => {
    clearToken();
    setTok(null);
  }, []);

  return <Ctx.Provider value={{ token, ready, signIn, signOut }}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
