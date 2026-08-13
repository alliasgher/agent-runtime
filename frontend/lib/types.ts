export type EventType =
  | "thinking"
  | "token"
  | "tool_call"
  | "tool_result"
  | "response"
  | "error";

export interface AgentEvent {
  type: EventType;
  content?: string;
  tool_name?: string;
  tool_input?: string;
  tool_id?: string;
  step: number;
  timestamp: number;
}

export interface Message {
  id: string;
  role: "user" | "assistant";
  content: string;
  events?: AgentEvent[];
  timestamp: number;
  isError?: boolean;
}

/** A file uploaded to a session and indexed for retrieval. */
export interface DocumentInfo {
  id: string;
  session_id: string;
  name: string;
  size_bytes: number;
  chars: number;
  num_chunks: number;
  created_at: string;
}

export interface ToolInfo {
  name: string;
  description: string;
  parameters: Record<
    string,
    { type: string; description: string }
  >;
}
