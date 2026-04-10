import { AgentEvent } from "./types";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const WS_BASE = API_BASE.replace(/^http/, "ws");

const SESSION_KEY = "agent_session_id";

export async function getOrCreateSession(): Promise<string> {
  const stored = localStorage.getItem(SESSION_KEY);
  if (stored) {
    // Verify the session still exists on the server
    const res = await fetch(`${API_BASE}/api/sessions`);
    const sessions: { id: string }[] = await res.json();
    if (sessions.some((s) => s.id === stored)) {
      return stored;
    }
  }
  const res = await fetch(`${API_BASE}/api/sessions`, { method: "POST" });
  const data = await res.json();
  localStorage.setItem(SESSION_KEY, data.id);
  return data.id;
}

export async function createSession(): Promise<string> {
  const res = await fetch(`${API_BASE}/api/sessions`, { method: "POST" });
  const data = await res.json();
  localStorage.setItem(SESSION_KEY, data.id);
  return data.id;
}

export function connectWebSocket(
  sessionId: string,
  onEvent: (event: AgentEvent) => void,
  onClose?: () => void,
  onOpen?: (isReconnect: boolean) => void
): {
  send: (content: string) => void;
  cancel: () => void;
  close: () => void;
} {
  let ws: WebSocket;
  let closed = false;
  let retryDelay = 1000;
  let everOpened = false;

  function connect() {
    ws = new WebSocket(`${WS_BASE}/ws/${sessionId}`);

    ws.onopen = () => {
      retryDelay = 1000;
      const isReconnect = everOpened;
      everOpened = true;
      console.log(isReconnect ? "WebSocket reconnected" : "WebSocket connected");
      onOpen?.(isReconnect);
    };

    ws.onmessage = (e) => {
      try {
        const event: AgentEvent = JSON.parse(e.data);
        onEvent(event);
      } catch (err) {
        console.error("Failed to parse event:", err);
      }
    };

    ws.onclose = () => {
      console.log("WebSocket disconnected");
      onClose?.();
      if (!closed) {
        // Reconnect with exponential backoff (max 30s)
        setTimeout(() => {
          if (!closed) {
            retryDelay = Math.min(retryDelay * 2, 30000);
            connect();
          }
        }, retryDelay);
      }
    };

    ws.onerror = (err) => {
      console.error("WebSocket error:", err);
    };
  }

  connect();

  return {
    send: (content: string) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "message", content }));
      }
    },
    cancel: () => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "cancel" }));
      }
    },
    close: () => {
      closed = true;
      ws.close();
    },
  };
}

export async function fetchSession(sessionId: string): Promise<{ id: string; messages: { role: string; content: string }[] } | null> {
  const res = await fetch(`${API_BASE}/api/sessions/${sessionId}`);
  if (!res.ok) return null;
  return res.json();
}

export async function fetchSessions(): Promise<{ id: string; title: string; message_count: number; updated_at: string }[]> {
  const res = await fetch(`${API_BASE}/api/sessions`);
  return res.json();
}

export async function deleteSession(sessionId: string): Promise<void> {
  await fetch(`${API_BASE}/api/sessions/${sessionId}`, { method: "DELETE" });
}

export async function fetchTools() {
  const res = await fetch(`${API_BASE}/api/tools`);
  return res.json();
}
