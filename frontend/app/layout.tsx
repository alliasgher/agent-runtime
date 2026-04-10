import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agent Runtime",
  description: "AI Agent Orchestration Platform",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
