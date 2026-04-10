"use client";

import { useState } from "react";
import { AgentEvent } from "@/lib/types";

const TOOL_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  web_search: { bg: "bg-blue-50",    border: "border-blue-200",    text: "text-blue-600" },
  read_url:   { bg: "bg-purple-50",  border: "border-purple-200",  text: "text-purple-600" },
  run_python: { bg: "bg-emerald-50", border: "border-emerald-200", text: "text-emerald-600" },
  wikipedia:  { bg: "bg-amber-50",   border: "border-amber-200",   text: "text-amber-600" },
};

const DEFAULT_COLOR = { bg: "bg-prism-surface", border: "border-prism-border", text: "text-prism-secondary" };

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function ToolCard({
  call,
  result,
  isActive,
}: {
  call: AgentEvent;
  result?: AgentEvent;
  isActive: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const color = TOOL_COLORS[call.tool_name || ""] || DEFAULT_COLOR;

  let parsedInput: Record<string, unknown> = {};
  try {
    parsedInput = JSON.parse(call.tool_input || "{}");
  } catch { /* ignore */ }

  const inputPreview = Object.entries(parsedInput)
    .map(([k, v]) => `${k}: ${String(v).slice(0, 100)}`)
    .join(" · ");

  const resultText = result?.content || "";
  const duration = result ? result.timestamp - call.timestamp : null;

  return (
    <div className={`rounded-xl border overflow-hidden transition-all duration-200 shadow-sm ${
      isActive
        ? "border-prism-mint/50 shadow-[0_0_0_1px_rgba(0,201,167,0.15)] tool-card-active"
        : "border-prism-border"
    } bg-white`}>

      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-3 py-2.5 hover:bg-prism-surface transition-colors text-left"
      >
        <div className={`w-7 h-7 rounded-lg ${color.bg} ${color.border} border flex items-center justify-center flex-shrink-0`}>
          <ToolIcon name={call.tool_name || ""} className={`w-3.5 h-3.5 ${color.text}`} />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-prism-deep">{call.tool_name}</span>
            {isActive && !result ? (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-prism-navy/10 text-prism-navy animate-pulse font-medium">
                Running
              </span>
            ) : result ? (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-prism-mint/15 text-prism-mint-dark font-medium">
                Done
              </span>
            ) : null}
            {duration !== null && duration > 0 && (
              <span className="text-[10px] text-prism-muted font-mono">
                {formatDuration(duration)}
              </span>
            )}
          </div>
          <p className="text-[11px] text-prism-muted truncate mt-0.5 leading-tight">{inputPreview}</p>
        </div>

        <svg
          className={`w-3.5 h-3.5 text-prism-muted flex-shrink-0 transition-transform ${expanded ? "rotate-180" : ""}`}
          fill="none" viewBox="0 0 24 24" stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {expanded && (
        <div className="border-t border-prism-border divide-y divide-prism-border">
          <div className="px-3 py-2.5">
            <p className="text-[10px] font-semibold text-prism-muted uppercase tracking-wider mb-1.5">Input</p>
            <pre className="text-[11px] text-prism-secondary bg-prism-surface rounded-lg p-2.5 overflow-x-auto whitespace-pre-wrap leading-relaxed border border-prism-border">
              {JSON.stringify(parsedInput, null, 2)}
            </pre>
          </div>

          {resultText && (
            <div className="px-3 py-2.5">
              <p className="text-[10px] font-semibold text-prism-muted uppercase tracking-wider mb-1.5">Result</p>
              <pre className="text-[11px] text-prism-secondary bg-prism-surface rounded-lg p-2.5 overflow-x-auto whitespace-pre-wrap max-h-56 overflow-y-auto leading-relaxed border border-prism-border">
                {resultText}
              </pre>
            </div>
          )}

          {isActive && !result && (
            <div className="px-3 py-2.5 flex items-center gap-2">
              <div className="flex gap-1">
                <div className="w-1 h-1 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "0ms" }} />
                <div className="w-1 h-1 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "150ms" }} />
                <div className="w-1 h-1 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "300ms" }} />
              </div>
              <span className="text-[11px] text-prism-muted">Waiting for result...</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolIcon({ name, className }: { name: string; className?: string }) {
  const icons: Record<string, React.ReactNode> = {
    web_search: (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
      </svg>
    ),
    read_url: (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    ),
    run_python: (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
      </svg>
    ),
    wikipedia: (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253" />
      </svg>
    ),
  };

  return <>{icons[name] || (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
    </svg>
  )}</>;
}
