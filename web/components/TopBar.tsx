"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";

export default function TopBar() {
  const { token, ready, signOut } = useAuth();
  const path = usePathname();
  const router = useRouter();

  const is = (p: string) => (path === p ? "active" : "");

  function handleSignOut() {
    signOut();
    router.push("/");
  }

  return (
    <header className="topbar">
      <div className="wrap topbar-inner">
        <Link href="/" className="brand">
          <span className="brand-mark">SIG</span>
          <span className="brand-word">snip</span>
        </Link>

        <nav className="nav-links">
          <Link href="/" className={is("/")}>Shorten</Link>
          {ready && token && (
            <Link href="/dashboard" className={is("/dashboard")}>Links</Link>
          )}
          {ready && !token && (
            <>
              <Link href="/login" className={is("/login")}>Sign in</Link>
              <Link href="/register" className={is("/register")}>Get access</Link>
            </>
          )}
          {ready && token && <button onClick={handleSignOut}>Sign out</button>}
        </nav>
      </div>
    </header>
  );
}
