"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import { AgentEvent, Message } from "@/lib/types";
import { createSession, connectWebSocket } from "@/lib/websocket";
import ToolCard from "./ToolCard";

export default function Chat() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isConnected, setIsConnected] = useState(false);
  const [isProcessing, setIsProcessing] = useState(false);
  const [currentEvents, setCurrentEvents] = useState<AgentEvent[]>([]);
  const [thinkingText, setThinkingText] = useState<string | null>(null);

  const wsRef = useRef<{ send: (s: string) => void; close: () => void } | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, currentEvents, scrollToBottom]);

  // Connect on mount
  useEffect(() => {
    let mounted = true;

    async function connect() {
      try {
        const sessionId = await createSession();

        const ws = connectWebSocket(
          sessionId,
          (event) => {
            if (!mounted) return;
            handleEvent(event);
          },
          () => {
            if (mounted) setIsConnected(false);
          }
        );

        wsRef.current = ws;
        if (mounted) setIsConnected(true);
      } catch (err) {
        console.error("Failed to connect:", err);
      }
    }

    connect();
    return () => {
      mounted = false;
      wsRef.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handleEvent(event: AgentEvent) {
    switch (event.type) {
      case "thinking":
        setThinkingText(event.content || "Thinking...");
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
        setThinkingText(null);
        setIsProcessing(false);
        break;

      case "error":
        setMessages((prev) => [
          ...prev,
          {
            id: `msg-${Date.now()}`,
            role: "assistant",
            content: `Error: ${event.content}`,
            events: [],
            timestamp: event.timestamp,
          },
        ]);
        setCurrentEvents([]);
        setThinkingText(null);
        setIsProcessing(false);
        break;
    }
  }

  // We need a way to get current events at the time of response
  const currentEventsRef = useRef<AgentEvent[]>([]);
  useEffect(() => {
    currentEventsRef.current = currentEvents;
  }, [currentEvents]);

  function currentEventsSnapshot() {
    return currentEventsRef.current;
  }

  function handleSend() {
    const text = input.trim();
    if (!text || !isConnected || isProcessing) return;

    setMessages((prev) => [
      ...prev,
      {
        id: `msg-${Date.now()}`,
        role: "user",
        content: text,
        timestamp: Date.now(),
      },
    ]);
    setInput("");
    setIsProcessing(true);
    setCurrentEvents([]);
    setThinkingText(null);

    wsRef.current?.send(text);

    // Reset textarea height
    if (inputRef.current) {
      inputRef.current.style.height = "auto";
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  // Group current events into tool call/result pairs
  const toolPairs = groupToolEvents(currentEvents);

  return (
    <div className="flex flex-col h-screen max-w-4xl mx-auto">
      {/* Header */}
      <header className="flex items-center justify-between px-6 py-4 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-blue-600 flex items-center justify-center">
            <svg className="w-5 h-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div>
            <h1 className="text-lg font-semibold text-white">Agent Runtime</h1>
            <p className="text-xs text-slate-400">AI Agent Orchestration Engine</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div
            className={`w-2 h-2 rounded-full ${
              isConnected ? "bg-emerald-400" : "bg-red-400"
            }`}
          />
          <span className="text-xs text-slate-400">
            {isConnected ? "Connected" : "Disconnected"}
          </span>
        </div>
      </header>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
        {messages.length === 0 && !isProcessing && (
          <div className="flex flex-col items-center justify-center h-full text-center">
            <div className="w-16 h-16 rounded-2xl bg-slate-800 flex items-center justify-center mb-4">
              <svg className="w-8 h-8 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
            <h2 className="text-xl font-semibold text-slate-200 mb-2">
              Agent Runtime
            </h2>
            <p className="text-slate-400 max-w-md mb-6">
              An AI agent that can search the web, read pages, run Python code,
              and look up Wikipedia — all orchestrated through a real-time tool
              execution engine.
            </p>
            <div className="grid grid-cols-2 gap-3 max-w-lg w-full">
              {[
                "Research the latest SpaceX launches and summarize them",
                "Write a Python script to generate a multiplication table",
                "What is quantum computing? Give me a comprehensive overview",
                "Search for the top programming languages in 2025 and compare them",
              ].map((suggestion, i) => (
                <button
                  key={i}
                  onClick={() => {
                    setInput(suggestion);
                    inputRef.current?.focus();
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
              <div className="flex justify-end">
                <div className="bg-blue-600 text-white rounded-2xl rounded-tr-sm px-4 py-3 max-w-[80%]">
                  <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
                </div>
              </div>
            ) : (
              <div className="space-y-3">
                {/* Show tool cards for this message */}
                {msg.events && msg.events.length > 0 && (
                  <div className="space-y-2 ml-2">
                    {groupToolEvents(msg.events).map((pair, i) => (
                      <ToolCard
                        key={i}
                        call={pair.call}
                        result={pair.result}
                        isActive={false}
                      />
                    ))}
                  </div>
                )}
                <div className="flex justify-start">
                  <div className="bg-slate-800 rounded-2xl rounded-tl-sm px-4 py-3 max-w-[80%] border border-slate-700/50">
                    <div className="text-sm prose prose-invert prose-sm max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
                      <ReactMarkdown>{msg.content}</ReactMarkdown>
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        ))}

        {/* Active processing */}
        {isProcessing && (
          <div className="space-y-3">
            {/* Active tool cards */}
            {toolPairs.length > 0 && (
              <div className="space-y-2 ml-2">
                {toolPairs.map((pair, i) => (
                  <ToolCard
                    key={i}
                    call={pair.call}
                    result={pair.result}
                    isActive={!pair.result}
                  />
                ))}
              </div>
            )}

            {/* Thinking indicator */}
            {thinkingText && toolPairs.length === 0 && (
              <div className="flex justify-start">
                <div className="bg-slate-800 rounded-2xl rounded-tl-sm px-4 py-3 border border-slate-700/50">
                  <div className="flex items-center gap-2">
                    <div className="flex gap-1">
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "0ms" }} />
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "150ms" }} />
                      <div className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-bounce" style={{ animationDelay: "300ms" }} />
                    </div>
                    <span className="text-sm text-slate-400">Thinking...</span>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Input */}
      <div className="px-6 py-4 border-t border-slate-800">
        <div className="flex gap-3 items-end">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => {
              setInput(e.target.value);
              // Auto-resize
              e.target.style.height = "auto";
              e.target.style.height = Math.min(e.target.scrollHeight, 150) + "px";
            }}
            onKeyDown={handleKeyDown}
            placeholder={
              isConnected
                ? "Ask the agent anything..."
                : "Connecting..."
            }
            disabled={!isConnected}
            rows={1}
            className="flex-1 bg-slate-800 border border-slate-700 rounded-xl px-4 py-3 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50 resize-none disabled:opacity-50 transition-all"
          />
          <button
            onClick={handleSend}
            disabled={!input.trim() || !isConnected || isProcessing}
            className="bg-blue-600 hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-500 text-white rounded-xl px-4 py-3 transition-colors flex-shrink-0"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19V5m0 0l-7 7m7-7l7 7" />
            </svg>
          </button>
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
      if (idx !== undefined) {
        pairs[idx].result = event;
      }
    }
  }

  return pairs;
}
