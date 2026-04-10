export type EventType =
  | "thinking"
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
}

export interface ToolInfo {
  name: string;
  description: string;
  parameters: Record<
    string,
    { type: string; description: string }
  >;
}
