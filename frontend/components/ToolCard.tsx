"use client";

import { useState } from "react";
import { AgentEvent } from "@/lib/types";

const TOOL_ICONS: Record<string, string> = {
  web_search: "magnifying-glass",
  read_url: "globe",
  run_python: "code",
  wikipedia: "book",
};

const TOOL_COLORS: Record<string, string> = {
  web_search: "blue",
  read_url: "purple",
  run_python: "emerald",
  wikipedia: "amber",
};

function getColor(tool: string) {
  const color = TOOL_COLORS[tool] || "slate";
  return {
    bg: `bg-${color}-500/10`,
    border: `border-${color}-500/30`,
    text: `text-${color}-400`,
    badge: `bg-${color}-500/20 text-${color}-300`,
  };
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
  const color = getColor(call.tool_name || "");

  let parsedInput: Record<string, unknown> = {};
  try {
    parsedInput = JSON.parse(call.tool_input || "{}");
  } catch {
    // ignore
  }

  const inputPreview = Object.entries(parsedInput)
    .map(([k, v]) => `${k}: ${String(v).slice(0, 80)}`)
    .join(", ");

  const resultText = result?.content || "";
  const truncatedResult =
    resultText.length > 300 ? resultText.slice(0, 300) + "..." : resultText;

  return (
    <div
      className={`rounded-lg border ${
        isActive ? "tool-card-active border-blue-500/50" : "border-slate-700/50"
      } bg-slate-800/50 overflow-hidden transition-all duration-200`}
    >
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-slate-700/30 transition-colors text-left"
      >
        <div
          className={`w-8 h-8 rounded-lg ${color.bg} ${color.border} border flex items-center justify-center flex-shrink-0`}
        >
          <ToolIcon name={call.tool_name || ""} className={color.text} />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-slate-200">
              {call.tool_name}
            </span>
            {isActive && !result && (
              <span className="text-xs px-2 py-0.5 rounded-full bg-blue-500/20 text-blue-300 animate-pulse">
                Running...
              </span>
            )}
            {result && (
              <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-500/20 text-emerald-300">
                Done
              </span>
            )}
          </div>
          <p className="text-xs text-slate-400 truncate mt-0.5">
            {inputPreview}
          </p>
        </div>

        <svg
          className={`w-4 h-4 text-slate-500 transition-transform ${
            expanded ? "rotate-180" : ""
          }`}
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 9l-7 7-7-7"
          />
        </svg>
      </button>

      {/* Expanded content */}
      {expanded && (
        <div className="border-t border-slate-700/50 px-4 py-3 space-y-3">
          <div>
            <p className="text-xs font-medium text-slate-400 mb-1">Input</p>
            <pre className="text-xs text-slate-300 bg-slate-900/50 rounded p-2 overflow-x-auto whitespace-pre-wrap">
              {JSON.stringify(parsedInput, null, 2)}
            </pre>
          </div>
          {result && (
            <div>
              <p className="text-xs font-medium text-slate-400 mb-1">Result</p>
              <pre className="text-xs text-slate-300 bg-slate-900/50 rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-64 overflow-y-auto">
                {expanded ? resultText : truncatedResult}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolIcon({
  name,
  className,
}: {
  name: string;
  className?: string;
}) {
  const icons: Record<string, React.ReactNode> = {
    web_search: (
      <svg className={`w-4 h-4 ${className}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
      </svg>
    ),
    read_url: (
      <svg className={`w-4 h-4 ${className}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    ),
    run_python: (
      <svg className={`w-4 h-4 ${className}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
      </svg>
    ),
    wikipedia: (
      <svg className={`w-4 h-4 ${className}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253" />
      </svg>
    ),
  };

  return icons[name] || (
    <svg className={`w-4 h-4 ${className}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
    </svg>
  );
}
