import type { Metadata } from "next";
import "./globals.css";
import { AuthProvider } from "@/lib/auth";
import TopBar from "@/components/TopBar";

export const metadata: Metadata = {
  title: "snip — link console",
  description: "Compress, route, and measure every link. A dark editorial URL console.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <div id="root-shell">
          <AuthProvider>
            <TopBar />
            {children}
          </AuthProvider>
        </div>
      </body>
    </html>
  );
}
