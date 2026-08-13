import { AgentEvent, DocumentInfo } from "./types";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const WS_BASE = API_BASE.replace(/^http/, "ws");

const SESSION_KEY = "agent_session_id";

/** Mirrors docs.MaxFileSize on the backend, so oversized files fail instantly. */
export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

/** Populates the file picker's filter. The backend is the real gatekeeper. */
export const ACCEPTED_UPLOAD_TYPES =
  ".pdf,.docx,.txt,.md,.markdown,.csv,.tsv,.json,.yaml,.yml,.toml,.log,.rst,.tex," +
  ".go,.py,.js,.jsx,.ts,.tsx,.java,.c,.h,.cpp,.hpp,.cs,.rb,.rs,.php,.swift,.kt,.sql,.html,.css,.sh";

/**
 * Render's free tier spins the container down after 15 minutes idle, so the
 * first request after a quiet period blocks while it boots. Poll /api/health
 * until it answers rather than firing a real request that appears to hang.
 */
export async function wakeBackend(opts: {
  onProgress?: (elapsedMs: number) => void;
  timeoutMs?: number;
} = {}): Promise<boolean> {
  const { onProgress, timeoutMs = 120_000 } = opts;
  const started = Date.now();

  while (Date.now() - started < timeoutMs) {
    try {
      const controller = new AbortController();
      const t = setTimeout(() => controller.abort(), 15_000);
      const res = await fetch(`${API_BASE}/api/health`, {
        signal: controller.signal,
        cache: "no-store",
      });
      clearTimeout(t);
      if (res.ok) return true;
    } catch {
      // Boot in progress (connection refused / 502 / aborted) — keep waiting.
    }
    onProgress?.(Date.now() - started);
    await new Promise((r) => setTimeout(r, 2000));
  }

  return false;
}

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

export async function fetchDocuments(sessionId: string): Promise<DocumentInfo[]> {
  const res = await fetch(`${API_BASE}/api/sessions/${sessionId}/documents`);
  if (!res.ok) return [];
  return res.json();
}

/**
 * Uploads one file. The backend's rejection messages are written for the user
 * (wrong format, scanned PDF, too large), so they're surfaced verbatim.
 */
export async function uploadDocument(sessionId: string, file: File): Promise<DocumentInfo> {
  if (file.size > MAX_UPLOAD_BYTES) {
    throw new Error(`${file.name} is larger than ${MAX_UPLOAD_BYTES / (1024 * 1024)}MB`);
  }

  const form = new FormData();
  form.append("file", file);

  const res = await fetch(`${API_BASE}/api/sessions/${sessionId}/documents`, {
    method: "POST",
    body: form,
  });

  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || `Upload failed (${res.status})`);
  }
  return data as DocumentInfo;
}

export async function deleteDocument(sessionId: string, docId: string): Promise<void> {
  await fetch(`${API_BASE}/api/sessions/${sessionId}/documents/${docId}`, {
    method: "DELETE",
  });
}
