import { AgentEvent } from "./types";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const WS_BASE = API_BASE.replace(/^http/, "ws");

export async function createSession(): Promise<string> {
  const res = await fetch(`${API_BASE}/api/sessions`, { method: "POST" });
  const data = await res.json();
  return data.id;
}

export function connectWebSocket(
  sessionId: string,
  onEvent: (event: AgentEvent) => void,
  onClose?: () => void
): {
  send: (content: string) => void;
  close: () => void;
} {
  const ws = new WebSocket(`${WS_BASE}/ws/${sessionId}`);

  ws.onopen = () => {
    console.log("WebSocket connected");
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
  };

  ws.onerror = (err) => {
    console.error("WebSocket error:", err);
  };

  return {
    send: (content: string) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "message", content }));
      }
    },
    close: () => ws.close(),
  };
}

export async function fetchTools() {
  const res = await fetch(`${API_BASE}/api/tools`);
  return res.json();
}
