"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import remarkBreaks from "remark-breaks";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/cjs/styles/prism";
import { AgentEvent, Message } from "@/lib/types";
import { getOrCreateSession, createSession, connectWebSocket, fetchSession } from "@/lib/websocket";
import ToolCard from "./ToolCard";
import Sidebar from "./Sidebar";

function formatTime(ts: number) {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function CopyButton({ text, className = "" }: { text: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }
  return (
    <button
      onClick={copy}
      title="Copy"
      className={`flex items-center gap-1 text-[10px] px-2 py-1 rounded-md transition-colors ${
        copied
          ? "bg-prism-mint/20 text-prism-mint-dark"
          : "bg-prism-border/60 hover:bg-prism-border text-prism-muted hover:text-prism-navy"
      } ${className}`}
    >
      {copied ? (
        <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      ) : (
        <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
        </svg>
      )}
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const markdownComponents: any = {
  code({ className, children, ...props }: {
    className?: string;
    children?: React.ReactNode;
  }) {
    const match = /language-(\w+)/.exec(className || "");
    const codeText = String(children).replace(/\n$/, "");
    // react-markdown v9 doesn't reliably pass `inline`. Detect it by:
    // no language class + no newlines in content = inline code span.
    const isInline = !match && !codeText.includes("\n");

    if (isInline) {
      return (
        <code className="text-xs bg-prism-surface border border-prism-border text-prism-navy rounded px-1 py-0.5 font-mono" {...props}>
          {children}
        </code>
      );
    }

    if (match) {
      return (
        <div className="relative group my-2">
          <div className="absolute top-2 right-2 z-10 opacity-0 group-hover:opacity-100 transition-opacity">
            <CopyButton text={codeText} />
          </div>
          <SyntaxHighlighter
            style={oneDark}
            language={match[1]}
            PreTag="div"
            customStyle={{
              borderRadius: "0.5rem",
              fontSize: "0.75rem",
              margin: 0,
              background: "rgb(15 23 42 / 0.8)",
            }}
            {...props}
          >
            {codeText}
          </SyntaxHighlighter>
        </div>
      );
    }

    // Fenced block without a language tag
    return (
      <div className="relative group my-2">
        <div className="absolute top-2 right-2 z-10 opacity-0 group-hover:opacity-100 transition-opacity">
          <CopyButton text={codeText} />
        </div>
        <pre className="text-xs bg-slate-900/80 rounded-lg p-3 overflow-x-auto whitespace-pre-wrap">
          <code {...props}>{children}</code>
        </pre>
      </div>
    );
  },
};

export default function Chat() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isConnected, setIsConnected] = useState(false);
  const [isProcessing, setIsProcessing] = useState(false);
  const [currentEvents, setCurrentEvents] = useState<AgentEvent[]>([]);
  const [thinkingStep, setThinkingStep] = useState(0);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [sidebarRefresh, setSidebarRefresh] = useState(0);
  const [streamingContent, setStreamingContent] = useState("");

  const wsRef = useRef<{ send: (s: string) => void; cancel: () => void; close: () => void } | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const currentEventsRef = useRef<AgentEvent[]>([]);
  const streamingContentRef = useRef("");

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => { scrollToBottom(); }, [messages, currentEvents, streamingContent, scrollToBottom]);
  useEffect(() => { currentEventsRef.current = currentEvents; }, [currentEvents]);
  // streamingContentRef is kept in sync directly by each handler (not via useEffect)
  // to avoid races where a stale render-cycle effect overwrites a ref that was just updated.

  const connectToSession = useCallback((sid: string, mounted: { current: boolean }) => {
    const ws = connectWebSocket(
      sid,
      (event) => { if (mounted.current) handleEvent(event); },
      () => { if (mounted.current) setIsConnected(false); },
      (isReconnect) => {
        if (!mounted.current) return;
        setIsConnected(true);
        if (isReconnect) {
          // The agent goroutine was killed when the connection dropped — clear
          // stuck processing state so the UI doesn't show "Thinking..." forever.
          streamingContentRef.current = "";
          setIsProcessing(false);
          setCurrentEvents([]);
          setStreamingContent("");
          setThinkingStep(0);
        }
      }
    );
    wsRef.current = ws;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const mounted = { current: true };

    async function connect() {
      try {
        const sid = await getOrCreateSession();
        setSessionId(sid);
        const existing = await fetchSession(sid);
        if (existing?.messages?.length && mounted.current) {
          setMessages(existing.messages.map((m, i) => ({
            id: `history-${i}`,
            role: m.role as "user" | "assistant",
            content: m.content,
            events: [],
            timestamp: Date.now(),
          })));
        }
        connectToSession(sid, mounted);
      } catch (err) {
        console.error("Failed to connect:", err);
      }
    }

    connect();
    return () => {
      mounted.current = false;
      wsRef.current?.close();
    };
  }, [connectToSession]);

  function handleEvent(event: AgentEvent) {
    switch (event.type) {
      case "thinking":
        setThinkingStep(event.step);
        setCurrentEvents((prev) => [...prev, event]);
        break;

      case "token": {
        // Update ref immediately so EventResponse sees the full content even if
        // React hasn't flushed the state update yet (useEffect is async).
        const newStreaming = streamingContentRef.current + (event.content || "");
        streamingContentRef.current = newStreaming;
        setStreamingContent(newStreaming);
        break;
      }

      case "tool_call":
        // Model decided to use a tool — clear any streamed text (it was preamble)
        setStreamingContent("");
        streamingContentRef.current = "";
        setCurrentEvents((prev) => [...prev, event]);
        break;

      case "tool_result":
        setCurrentEvents((prev) => [...prev, event]);
        break;

      case "response": {
        // Capture before clearing — ref is the authoritative source since it's
        // updated synchronously in the token handler, unlike the state which
        // flushes asynchronously.
        const content = streamingContentRef.current || event.content || "";
        streamingContentRef.current = "";
        if (!content.trim()) {
          // Backend sent an empty response — don't add a blank bubble.
          // The backend should have logged a warning about this.
          console.warn("[agent] received empty response event — skipping blank bubble", event);
          setStreamingContent("");
          setCurrentEvents([]);
          setThinkingStep(0);
          setIsProcessing(false);
          setSidebarRefresh((n) => n + 1);
          break;
        }
        setMessages((prev) => [
          ...prev,
          {
            id: `msg-${Date.now()}`,
            role: "assistant",
            content,
            events: [...currentEventsRef.current],
            timestamp: event.timestamp,
          },
        ]);
        setStreamingContent("");
        setCurrentEvents([]);
        setThinkingStep(0);
        setIsProcessing(false);
        setSidebarRefresh((n) => n + 1);
        break;
      }

      case "error": {
        const isCancel = event.content?.toLowerCase().includes("cancel");
        if (!isCancel) {
          setMessages((prev) => [
            ...prev,
            {
              id: `msg-${Date.now()}`,
              role: "assistant",
              content: event.content || "Something went wrong.",
              events: [],
              timestamp: event.timestamp,
              isError: true,
            },
          ]);
        }
        setStreamingContent("");
        setCurrentEvents([]);
        setThinkingStep(0);
        setIsProcessing(false);
        break;
      }
    }
  }

  function handleSend(text?: string) {
    const msg = (text ?? input).trim();
    if (!msg || !isConnected || isProcessing) return;

    setMessages((prev) => [...prev, {
      id: `msg-${Date.now()}`,
      role: "user",
      content: msg,
      timestamp: Date.now(),
    }]);
    setInput("");
    setIsProcessing(true);
    setCurrentEvents([]);
    setStreamingContent("");
    setThinkingStep(0);
    wsRef.current?.send(msg);
    if (inputRef.current) inputRef.current.style.height = "auto";
  }

  function handleCancel() {
    wsRef.current?.cancel();
    setIsProcessing(false);
    setCurrentEvents([]);
    setStreamingContent("");
    setThinkingStep(0);
  }

  async function handleNewChat() {
    wsRef.current?.close();
    setMessages([]);
    setCurrentEvents([]);
    setStreamingContent("");
    setThinkingStep(0);
    setIsProcessing(false);
    setIsConnected(false);

    const mounted = { current: true };
    const sid = await createSession();
    setSessionId(sid);
    connectToSession(sid, mounted);
  }

  async function handleSelectSession(sid: string) {
    if (sid === sessionId) return;
    wsRef.current?.close();
    setMessages([]);
    setCurrentEvents([]);
    setStreamingContent("");
    setThinkingStep(0);
    setIsProcessing(false);
    setIsConnected(false);
    setSessionId(sid);

    localStorage.setItem("agent_session_id", sid);
    const existing = await fetchSession(sid);
    if (existing?.messages) {
      setMessages(existing.messages.map((m, i) => ({
        id: `history-${i}`,
        role: m.role as "user" | "assistant",
        content: m.content,
        events: [],
        timestamp: Date.now(),
      })));
    }
    const mounted = { current: true };
    connectToSession(sid, mounted);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function exportChat() {
    if (messages.length === 0) return;
    const lines = messages.map((m) =>
      `**${m.role === "user" ? "You" : "Agent"}:** ${m.content}`
    );
    const md = `# Chat Export\n\n${lines.join("\n\n")}`;
    const blob = new Blob([md], { type: "text/markdown" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `chat-${new Date().toISOString().slice(0, 10)}.md`;
    a.click();
    URL.revokeObjectURL(url);
  }

  const toolPairs = groupToolEvents(currentEvents);

  return (
    <div className="flex h-screen bg-prism-surface">
      <Sidebar
        currentSessionId={sessionId}
        onSelectSession={handleSelectSession}
        onNewChat={handleNewChat}
        refreshTrigger={sidebarRefresh}
      />

      <div className="flex flex-col flex-1 min-w-0">
        {/* Header */}
        <header className="flex items-center justify-between px-4 sm:px-6 py-3.5 border-b border-prism-border bg-white">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-prism-navy flex items-center justify-center flex-shrink-0">
              <svg className="w-4.5 h-4.5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
            <div>
              <h1 className="text-base sm:text-lg font-semibold text-prism-deep font-sora tracking-tight">Agent Runtime</h1>
              <p className="text-xs text-prism-muted hidden sm:block">AI Agent Orchestration Engine</p>
            </div>
          </div>
          <div className="flex items-center gap-3">
            {messages.length > 0 && (
              <button
                onClick={exportChat}
                title="Export chat as Markdown"
                className="hidden sm:flex items-center gap-1.5 text-xs text-prism-muted hover:text-prism-navy px-2.5 py-1.5 rounded-lg hover:bg-prism-surface transition-colors border border-transparent hover:border-prism-border"
              >
                <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
                </svg>
                Export
              </button>
            )}
            <div className="flex items-center gap-2">
              <div className={`w-2 h-2 rounded-full transition-colors ${isConnected ? "bg-prism-mint" : "bg-prism-coral animate-pulse"}`} />
              <span className="text-xs text-prism-muted hidden sm:block">
                {isConnected ? "Connected" : "Reconnecting..."}
              </span>
            </div>
          </div>
        </header>

        {/* Messages */}
        <div className="flex-1 overflow-y-auto px-4 sm:px-6 py-6 space-y-6">
          {messages.length === 0 && !isProcessing && !streamingContent && (
            <div className="flex flex-col items-center justify-center h-full text-center px-4">
              <div className="w-16 h-16 rounded-2xl bg-prism-navy flex items-center justify-center mb-4 shadow-lg">
                <svg className="w-8 h-8 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13 10V3L4 14h7v7l9-11h-7z" />
                </svg>
              </div>
              <h2 className="text-xl font-semibold text-prism-deep mb-2 font-sora">Agent Runtime</h2>
              <p className="text-prism-secondary max-w-md mb-6 text-sm leading-relaxed">
                An AI agent that can search the web, read pages, run Python code,
                and look up Wikipedia — all orchestrated in real time.
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-w-lg w-full">
                {[
                  "Research the latest SpaceX launches and summarize them",
                  "Write a Python script to generate a multiplication table",
                  "What is quantum computing? Give me a comprehensive overview",
                  "Search for the top programming languages in 2025 and compare them",
                ].map((suggestion, i) => (
                  <button
                    key={i}
                    onClick={() => handleSend(suggestion)}
                    className="text-left text-sm px-4 py-3 rounded-xl bg-white border border-prism-border text-prism-secondary hover:border-prism-navy hover:text-prism-navy transition-all shadow-sm"
                  >
                    {suggestion}
                  </button>
                ))}
              </div>
            </div>
          )}

          {messages.map((msg) => (
            <div key={msg.id}>
              {msg.role === "user" ? (
                <div className="flex justify-end items-end gap-2">
                  <span className="text-xs text-prism-muted mb-1">{formatTime(msg.timestamp)}</span>
                  <div className="bg-prism-navy text-white rounded-2xl rounded-tr-sm px-4 py-3 max-w-[85%] sm:max-w-[75%] shadow-sm">
                    <p className="text-sm whitespace-pre-wrap leading-relaxed">{msg.content}</p>
                  </div>
                </div>
              ) : (
                <div className="space-y-3">
                  {msg.events && msg.events.length > 0 && (
                    <div className="space-y-2 ml-1">
                      {groupToolEvents(msg.events).map((pair, i) => (
                        <ToolCard key={i} call={pair.call} result={pair.result} isActive={false} />
                      ))}
                    </div>
                  )}
                  <div className="flex justify-start items-end gap-2 group">
                    <div className={`rounded-2xl rounded-tl-sm px-4 py-3 max-w-[85%] sm:max-w-[75%] border shadow-sm ${
                      msg.isError
                        ? "bg-red-50 border-prism-coral/30"
                        : "bg-white border-prism-border"
                    }`}>
                      {msg.isError ? (
                        <div className="flex items-center gap-2">
                          <svg className="w-4 h-4 text-prism-coral flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                          </svg>
                          <p className="text-sm text-prism-coral">{msg.content}</p>
                        </div>
                      ) : (
                        <div className="text-sm prose prose-sm max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0 prose-p:text-prism-deep prose-headings:text-prism-deep prose-headings:font-sora prose-a:text-prism-mint prose-strong:text-prism-deep">
                          <ReactMarkdown remarkPlugins={[remarkBreaks]} components={markdownComponents}>{msg.content}</ReactMarkdown>
                        </div>
                      )}
                    </div>
                    <div className="flex flex-col items-center gap-1 mb-1">
                      <span className="text-xs text-prism-muted">{formatTime(msg.timestamp)}</span>
                      {!msg.isError && (
                        <div className="opacity-0 group-hover:opacity-100 transition-opacity">
                          <CopyButton text={msg.content} />
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </div>
          ))}

          {/* Active processing */}
          {isProcessing && (
            <div className="space-y-3">
              {toolPairs.length > 0 && (
                <div className="space-y-2 ml-1">
                  {toolPairs.map((pair, i) => (
                    <ToolCard key={i} call={pair.call} result={pair.result} isActive={!pair.result} />
                  ))}
                </div>
              )}

              {/* Streaming text */}
              {streamingContent && (
                <div className="flex justify-start">
                  <div className="bg-white rounded-2xl rounded-tl-sm px-4 py-3 border border-prism-border max-w-[85%] sm:max-w-[75%] shadow-sm">
                    <p className="text-sm text-prism-deep whitespace-pre-wrap leading-relaxed">
                      {streamingContent}
                      <span className="inline-block w-0.5 h-[1em] bg-prism-mint animate-pulse align-middle ml-0.5" />
                    </p>
                  </div>
                </div>
              )}

              {/* Thinking indicator (shown when no stream and no tools yet) */}
              {!streamingContent && toolPairs.length === 0 && (
                <div className="flex justify-start">
                  <div className="bg-white rounded-2xl rounded-tl-sm px-4 py-3 border border-prism-border shadow-sm">
                    <div className="flex items-center gap-2">
                      <div className="flex gap-1">
                        <div className="w-1.5 h-1.5 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "0ms" }} />
                        <div className="w-1.5 h-1.5 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "150ms" }} />
                        <div className="w-1.5 h-1.5 rounded-full bg-prism-mint animate-bounce" style={{ animationDelay: "300ms" }} />
                      </div>
                      <span className="text-sm text-prism-muted">
                        Thinking
                        {thinkingStep > 0 && <span className="text-prism-muted/60 ml-1">· step {thinkingStep}</span>}
                      </span>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          <div ref={messagesEndRef} />
        </div>

        {/* Input */}
        <div className="px-4 sm:px-6 py-4 border-t border-prism-border bg-white">
          <div className="flex gap-2 sm:gap-3 items-end">
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                e.target.style.height = "auto";
                e.target.style.height = Math.min(e.target.scrollHeight, 150) + "px";
              }}
              onKeyDown={handleKeyDown}
              placeholder={isConnected ? "Ask the agent anything..." : "Reconnecting..."}
              disabled={!isConnected}
              rows={1}
              className="flex-1 bg-prism-surface border border-prism-border rounded-xl px-4 py-3 text-sm text-prism-deep placeholder:text-prism-muted focus:outline-none focus:ring-2 focus:ring-prism-mint/40 focus:border-prism-mint resize-none disabled:opacity-50 transition-all"
            />
            {isProcessing ? (
              <button
                onClick={handleCancel}
                title="Cancel"
                className="bg-prism-coral hover:opacity-90 text-white rounded-xl px-4 py-3 transition-opacity flex-shrink-0"
              >
                <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            ) : (
              <button
                onClick={() => handleSend()}
                disabled={!input.trim() || !isConnected}
                title="Send"
                className="bg-prism-navy hover:bg-prism-navy-light disabled:bg-prism-border disabled:text-prism-muted text-white rounded-xl px-4 py-3 transition-colors flex-shrink-0"
              >
                <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19V5m0 0l-7 7m7-7l7 7" />
                </svg>
              </button>
            )}
          </div>
          <p className="text-xs text-prism-muted mt-2 text-center">
            Powered by a custom agent runtime with real-time tool orchestration
          </p>
        </div>
      </div>
    </div>
  );
}

interface ToolPair {
  call: AgentEvent;
  result?: AgentEvent;
}

function groupToolEvents(events: AgentEvent[]): ToolPair[] {
  const pairs: ToolPair[] = [];
  const callMap = new Map<string, number>();

  for (const event of events) {
    if (event.type === "tool_call" && event.tool_id) {
      callMap.set(event.tool_id, pairs.length);
      pairs.push({ call: event });
    } else if (event.type === "tool_result" && event.tool_id) {
      const idx = callMap.get(event.tool_id);
      if (idx !== undefined) pairs[idx].result = event;
    }
  }

  return pairs;
}
