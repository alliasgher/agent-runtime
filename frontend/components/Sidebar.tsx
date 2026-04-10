"use client";

import { useEffect, useState, useCallback } from "react";
import { fetchSessions, deleteSession } from "@/lib/websocket";

interface SessionEntry {
  id: string;
  title: string;
  message_count: number;
  updated_at: string;
}

function timeAgo(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export default function Sidebar({
  currentSessionId,
  onSelectSession,
  onNewChat,
  refreshTrigger,
}: {
  currentSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewChat: () => void;
  refreshTrigger: number;
}) {
  const [sessions, setSessions] = useState<SessionEntry[]>([]);
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const data = await fetchSessions();
      setSessions(data.filter((s) => s.message_count > 0));
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { load(); }, [load, refreshTrigger]);

  async function handleDelete(e: React.MouseEvent, id: string) {
    e.stopPropagation();
    setDeletingId(id);
    await deleteSession(id);
    setSessions((prev) => prev.filter((s) => s.id !== id));
    setDeletingId(null);
    if (id === currentSessionId) onNewChat();
  }

  function handleSelect(id: string) {
    onSelectSession(id);
    setMobileOpen(false);
  }

  function handleNew() {
    onNewChat();
    setMobileOpen(false);
  }

  const sessionList = (
    <div className="flex-1 overflow-y-auto py-2 [&::-webkit-scrollbar-track]:bg-prism-deep [&::-webkit-scrollbar-thumb]:bg-white/10 [&::-webkit-scrollbar-thumb:hover]:bg-white/20">
      {sessions.length === 0 ? (
        <p className="text-xs text-white/30 text-center mt-6 px-4">No conversations yet</p>
      ) : (
        sessions.map((s) => (
          <div
            key={s.id}
            onClick={() => handleSelect(s.id)}
            className={`group relative flex items-start gap-2 px-3 py-2.5 mx-1 rounded-lg cursor-pointer transition-colors ${
              s.id === currentSessionId
                ? "bg-prism-navy text-white"
                : "hover:bg-white/10 text-white/60 hover:text-white/90"
            }`}
          >
            <svg className="w-3.5 h-3.5 mt-0.5 flex-shrink-0 opacity-60" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-3 3v-3z" />
            </svg>
            <div className="flex-1 min-w-0">
              <p className="text-xs font-medium truncate leading-tight">{s.title}</p>
              <p className="text-[10px] text-white/30 mt-0.5">{timeAgo(s.updated_at)}</p>
            </div>
            <button
              onClick={(e) => handleDelete(e, s.id)}
              disabled={deletingId === s.id}
              className="opacity-0 group-hover:opacity-100 w-5 h-5 flex items-center justify-center rounded hover:bg-prism-coral/20 hover:text-prism-coral text-white/30 transition-all flex-shrink-0 mt-0.5"
              title="Delete"
            >
              <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          </div>
        ))
      )}
    </div>
  );

  const headerButtons = (onClose?: () => void) => (
    <div className="flex items-center justify-between px-3 py-4 border-b border-white/10">
      <span className="text-[10px] font-semibold text-white/40 uppercase tracking-widest font-sora">History</span>
      <div className="flex items-center gap-1">
        <button
          onClick={handleNew}
          className="w-7 h-7 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
          title="New chat"
        >
          <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
        </button>
        {onClose ? (
          <button
            onClick={onClose}
            className="w-7 h-7 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
            title="Close"
          >
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        ) : (
          <button
            onClick={() => setCollapsed(true)}
            className="w-7 h-7 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
            title="Collapse"
          >
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
            </svg>
          </button>
        )}
      </div>
    </div>
  );

  return (
    <>
      {/* ── Mobile drawer ── */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 flex md:hidden">
          <div
            className="absolute inset-0 bg-black/50 backdrop-blur-sm"
            onClick={() => setMobileOpen(false)}
          />
          <div className="relative flex flex-col w-72 bg-prism-deep border-r border-white/10 h-full">
            {headerButtons(() => setMobileOpen(false))}
            {sessionList}
          </div>
        </div>
      )}

      {/* ── Mobile trigger strip ── */}
      <div className="flex md:hidden flex-col items-center w-12 border-r border-white/10 bg-prism-deep py-4 gap-3">
        <button
          onClick={() => setMobileOpen(true)}
          className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
          title="Open history"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <button
          onClick={handleNew}
          className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
          title="New chat"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
        </button>
      </div>

      {/* ── Desktop sidebar ── */}
      {collapsed ? (
        <div className="hidden md:flex flex-col items-center w-12 border-r border-white/10 bg-prism-deep py-4 gap-3">
          <button
            onClick={() => setCollapsed(false)}
            className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
            title="Open sidebar"
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
          </button>
          <button
            onClick={handleNew}
            className="w-8 h-8 flex items-center justify-center rounded-lg hover:bg-white/10 text-white/50 hover:text-white transition-colors"
            title="New chat"
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
          </button>
        </div>
      ) : (
        <div className="hidden md:flex flex-col w-60 flex-shrink-0 border-r border-white/10 bg-prism-deep">
          {headerButtons()}
          {sessionList}
        </div>
      )}
    </>
  );
}
