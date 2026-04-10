"use client";

import { useState } from "react";
import { AgentEvent } from "@/lib/types";

const TOOL_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  web_search: { bg: "bg-blue-500/10",    border: "border-blue-500/30",    text: "text-blue-400" },
  read_url:   { bg: "bg-purple-500/10",  border: "border-purple-500/30",  text: "text-purple-400" },
  run_python: { bg: "bg-emerald-500/10", border: "border-emerald-500/30", text: "text-emerald-400" },
  wikipedia:  { bg: "bg-amber-500/10",   border: "border-amber-500/30",   text: "text-amber-400" },
};

const DEFAULT_COLOR = { bg: "bg-slate-500/10", border: "border-slate-500/30", text: "text-slate-400" };

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

  return (
    <div className={`rounded-xl border overflow-hidden transition-all duration-200 ${
      isActive
        ? "border-blue-500/50 shadow-[0_0_0_1px_rgba(59,130,246,0.15)] tool-card-active"
        : "border-slate-700/50"
    } bg-slate-800/50`}>

      {/* Header — always visible */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-3 py-2.5 hover:bg-slate-700/30 transition-colors text-left"
      >
        {/* Icon */}
        <div className={`w-7 h-7 rounded-lg ${color.bg} ${color.border} border flex items-center justify-center flex-shrink-0`}>
          <ToolIcon name={call.tool_name || ""} className={`w-3.5 h-3.5 ${color.text}`} />
        </div>

        {/* Name + preview */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-slate-200">{call.tool_name}</span>
            {isActive && !result ? (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-blue-500/20 text-blue-300 animate-pulse font-medium">
                Running
              </span>
            ) : result ? (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-500/20 text-emerald-300 font-medium">
                Done
              </span>
            ) : null}
          </div>
          <p className="text-[11px] text-slate-500 truncate mt-0.5 leading-tight">{inputPreview}</p>
        </div>

        {/* Chevron */}
        <svg
          className={`w-3.5 h-3.5 text-slate-500 flex-shrink-0 transition-transform ${expanded ? "rotate-180" : ""}`}
          fill="none" viewBox="0 0 24 24" stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {/* Expanded detail */}
      {expanded && (
        <div className="border-t border-slate-700/50 divide-y divide-slate-700/50">
          <div className="px-3 py-2.5">
            <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1.5">Input</p>
            <pre className="text-[11px] text-slate-300 bg-slate-900/60 rounded-lg p-2.5 overflow-x-auto whitespace-pre-wrap leading-relaxed">
              {JSON.stringify(parsedInput, null, 2)}
            </pre>
          </div>

          {resultText && (
            <div className="px-3 py-2.5">
              <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1.5">Result</p>
              <pre className="text-[11px] text-slate-300 bg-slate-900/60 rounded-lg p-2.5 overflow-x-auto whitespace-pre-wrap max-h-56 overflow-y-auto leading-relaxed">
                {resultText}
              </pre>
            </div>
          )}

          {isActive && !result && (
            <div className="px-3 py-2.5 flex items-center gap-2">
              <div className="flex gap-1">
                <div className="w-1 h-1 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "0ms" }} />
                <div className="w-1 h-1 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "150ms" }} />
                <div className="w-1 h-1 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "300ms" }} />
              </div>
              <span className="text-[11px] text-slate-500">Waiting for result...</span>
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
