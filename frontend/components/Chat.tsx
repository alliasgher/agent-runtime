"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import { AgentEvent, Message } from "@/lib/types";
import { getOrCreateSession, createSession, connectWebSocket, fetchSession } from "@/lib/websocket";
import ToolCard from "./ToolCard";

function formatTime(ts: number) {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export default function Chat() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isConnected, setIsConnected] = useState(false);
  const [isProcessing, setIsProcessing] = useState(false);
  const [currentEvents, setCurrentEvents] = useState<AgentEvent[]>([]);
  const [thinkingStep, setThinkingStep] = useState(0);
  const [sessionId, setSessionId] = useState<string | null>(null);

  const wsRef = useRef<{ send: (s: string) => void; cancel: () => void; close: () => void } | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const currentEventsRef = useRef<AgentEvent[]>([]);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, currentEvents, scrollToBottom]);

  useEffect(() => {
    currentEventsRef.current = currentEvents;
  }, [currentEvents]);

  function currentEventsSnapshot() {
    return currentEventsRef.current;
  }

  const connectToSession = useCallback((sid: string, mounted: { current: boolean }) => {
    const ws = connectWebSocket(
      sid,
      (event) => {
        if (!mounted.current) return;
        handleEvent(event);
      },
      () => { if (mounted.current) setIsConnected(false); },
      () => { if (mounted.current) setIsConnected(true); }
    );
    wsRef.current = ws;
    if (mounted.current) setIsConnected(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const mounted = { current: true };

    async function connect() {
      try {
        const sid = await getOrCreateSession();
        setSessionId(sid);

        const existing = await fetchSession(sid);
        if (existing && existing.messages && existing.messages.length > 0 && mounted.current) {
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
      case "tool_call":
      case "tool_result":
        setCurrentEvents((prev) => [...prev, event]);
        break;
      case "response":
        setMessages((prev) => [
          ...prev,
          {
            id: `msg-${Date.now()}`,
            role: "assistant",
            content: event.content || "",
            events: [...currentEventsSnapshot()],
            timestamp: event.timestamp,
          },
        ]);
        setCurrentEvents([]);
        setThinkingStep(0);
        setIsProcessing(false);
        break;
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
            } as Message,
          ]);
        }
        setCurrentEvents([]);
        setThinkingStep(0);
        setIsProcessing(false);
        break;
      }
    }
  }

  function handleSend() {
    const text = input.trim();
    if (!text || !isConnected || isProcessing) return;

    setMessages((prev) => [...prev, {
      id: `msg-${Date.now()}`,
      role: "user",
      content: text,
      timestamp: Date.now(),
    }]);
    setInput("");
    setIsProcessing(true);
    setCurrentEvents([]);
    setThinkingStep(0);
    wsRef.current?.send(text);
    if (inputRef.current) inputRef.current.style.height = "auto";
  }

  function handleCancel() {
    wsRef.current?.cancel();
    setIsProcessing(false);
    setCurrentEvents([]);
    setThinkingStep(0);
  }

  async function handleNewChat() {
    wsRef.current?.close();
    setMessages([]);
    setCurrentEvents([]);
    setThinkingStep(0);
    setIsProcessing(false);
    setIsConnected(false);

    const mounted = { current: true };
    const sid = await createSession();
    setSessionId(sid);
    connectToSession(sid, mounted);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  const toolPairs = groupToolEvents(currentEvents);

  return (
    <div className="flex flex-col h-screen max-w-4xl mx-auto">
      {/* Header */}
      <header className="flex items-center justify-between px-4 sm:px-6 py-4 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-blue-600 flex items-center justify-center flex-shrink-0">
            <svg className="w-5 h-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div>
            <h1 className="text-base sm:text-lg font-semibold text-white">Agent Runtime</h1>
            <p className="text-xs text-slate-400 hidden sm:block">AI Agent Orchestration Engine</p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {messages.length > 0 && !isProcessing && (
            <button
              onClick={handleNewChat}
              className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition-colors"
            >
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
              </svg>
              New chat
            </button>
          )}
          <div className="flex items-center gap-2">
            <div className={`w-2 h-2 rounded-full transition-colors ${isConnected ? "bg-emerald-400" : "bg-red-400 animate-pulse"}`} />
            <span className="text-xs text-slate-400 hidden sm:block">
              {isConnected ? "Connected" : "Reconnecting..."}
            </span>
          </div>
        </div>
      </header>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-4 sm:px-6 py-6 space-y-6">
        {messages.length === 0 && !isProcessing && (
          <div className="flex flex-col items-center justify-center h-full text-center px-4">
            <div className="w-16 h-16 rounded-2xl bg-slate-800 flex items-center justify-center mb-4">
              <svg className="w-8 h-8 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
            <h2 className="text-xl font-semibold text-slate-200 mb-2">Agent Runtime</h2>
            <p className="text-slate-400 max-w-md mb-6 text-sm">
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
                  onClick={() => {
                    if (!isConnected || isProcessing) return;
                    setMessages((prev) => [...prev, {
                      id: `msg-${Date.now()}`,
                      role: "user",
                      content: suggestion,
                      timestamp: Date.now(),
                    }]);
                    setIsProcessing(true);
                    setCurrentEvents([]);
                    setThinkingStep(0);
                    wsRef.current?.send(suggestion);
                  }}
                  className="text-left text-sm px-4 py-3 rounded-xl bg-slate-800/50 border border-slate-700/50 text-slate-300 hover:bg-slate-700/50 hover:border-slate-600 transition-all"
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
                <span className="text-xs text-slate-600 mb-1">{formatTime(msg.timestamp)}</span>
                <div className="bg-blue-600 text-white rounded-2xl rounded-tr-sm px-4 py-3 max-w-[85%] sm:max-w-[75%]">
                  <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
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
                <div className="flex justify-start items-end gap-2">
                  <div className={`rounded-2xl rounded-tl-sm px-4 py-3 max-w-[85%] sm:max-w-[75%] border ${
                    (msg as Message & { isError?: boolean }).isError
                      ? "bg-red-900/20 border-red-700/50"
                      : "bg-slate-800 border-slate-700/50"
                  }`}>
                    {(msg as Message & { isError?: boolean }).isError ? (
                      <div className="flex items-center gap-2">
                        <svg className="w-4 h-4 text-red-400 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                        </svg>
                        <p className="text-sm text-red-300">{msg.content}</p>
                      </div>
                    ) : (
                      <div className="text-sm prose prose-invert prose-sm max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
                        <ReactMarkdown>{msg.content}</ReactMarkdown>
                      </div>
                    )}
                  </div>
                  <span className="text-xs text-slate-600 mb-1">{formatTime(msg.timestamp)}</span>
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
            {toolPairs.length === 0 && (
              <div className="flex justify-start">
                <div className="bg-slate-800 rounded-2xl rounded-tl-sm px-4 py-3 border border-slate-700/50">
                  <div className="flex items-center gap-2">
                    <div className="flex gap-1">
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "0ms" }} />
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "150ms" }} />
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "300ms" }} />
                    </div>
                    <span className="text-sm text-slate-400">
                      Thinking
                      {thinkingStep > 0 && <span className="text-slate-600 ml-1">· step {thinkingStep}</span>}
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
      <div className="px-4 sm:px-6 py-4 border-t border-slate-800">
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
            className="flex-1 bg-slate-800 border border-slate-700 rounded-xl px-4 py-3 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50 resize-none disabled:opacity-50 transition-all"
          />
          {isProcessing ? (
            <button
              onClick={handleCancel}
              title="Cancel"
              className="bg-red-600 hover:bg-red-500 text-white rounded-xl px-4 py-3 transition-colors flex-shrink-0"
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          ) : (
            <button
              onClick={handleSend}
              disabled={!input.trim() || !isConnected}
              title="Send"
              className="bg-blue-600 hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-500 text-white rounded-xl px-4 py-3 transition-colors flex-shrink-0"
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19V5m0 0l-7 7m7-7l7 7" />
              </svg>
            </button>
          )}
        </div>
        <p className="text-xs text-slate-600 mt-2 text-center">
          Powered by a custom agent runtime with real-time tool orchestration
        </p>
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
