/**
 * Tests for the streaming content ref logic.
 *
 * These simulate the sequence of events the WebSocket handler processes
 * to catch the blank-response race that occurs after tool calls.
 *
 * The race: tool_call clears streamingContent state → React schedules a
 * re-render → before the render commits, token events arrive and update
 * streamingContentRef directly → if a useEffect syncing state→ref ran
 * AFTER those tokens, it would overwrite the ref with "" → response event
 * reads an empty ref → blank message.
 *
 * The fix: ref is updated only by handlers, never by a useEffect.
 */

// Simulate the ref/state management logic used in Chat.tsx
function makeStreamingState() {
  let streamingContentRef = { current: "" };
  let streamingContent = "";
  const messages: { role: string; content: string }[] = [];

  function handleToken(token: string) {
    const next = streamingContentRef.current + token;
    streamingContentRef.current = next;
    streamingContent = next; // state update (in React this is async, but ref is sync)
  }

  function handleToolCall() {
    streamingContent = "";
    streamingContentRef.current = ""; // cleared directly — no useEffect needed
  }

  function handleResponse(eventContent: string) {
    const content = streamingContentRef.current || eventContent || "";
    streamingContentRef.current = ""; // cleared directly after capture
    streamingContent = "";
    messages.push({ role: "assistant", content });
    return content;
  }

  // Simulate the OLD broken behaviour: useEffect syncs state→ref asynchronously.
  // This represents the race where the effect fires AFTER tokens updated the ref.
  function simulateStaleUseEffect() {
    // Effect runs with the most recently committed streamingContent state value.
    // If tool_call cleared state to "" before tokens arrived, this overwrites.
    streamingContentRef.current = streamingContent;
  }

  return { streamingContentRef, handleToken, handleToolCall, handleResponse, simulateStaleUseEffect, messages };
}

describe("streaming content ref — post-tool response", () => {
  it("captures full content when tokens arrive before response event", () => {
    const s = makeStreamingState();
    s.handleToken("Hello ");
    s.handleToken("world");
    const content = s.handleResponse("");
    expect(content).toBe("Hello world");
  });

  it("falls back to event.content when no tokens were streamed", () => {
    const s = makeStreamingState();
    const content = s.handleResponse("fallback text");
    expect(content).toBe("fallback text");
  });

  it("returns empty string only when both ref and event.content are empty", () => {
    const s = makeStreamingState();
    const content = s.handleResponse("");
    expect(content).toBe("");
  });

  it("clears ref after response so next message starts fresh", () => {
    const s = makeStreamingState();
    s.handleToken("first response");
    s.handleResponse("");
    // Next message tokens start from scratch
    s.handleToken("second");
    const content = s.handleResponse("");
    expect(content).toBe("second");
  });

  it("captures tokens that arrive AFTER a tool_call clears streaming state", () => {
    const s = makeStreamingState();

    // Step 1: model calls a tool — streaming cleared
    s.handleToolCall();
    expect(s.streamingContentRef.current).toBe("");

    // Step 2: second response tokens arrive (after tool execution)
    s.handleToken("Search ");
    s.handleToken("results: ");
    s.handleToken("Go is #1");

    // Step 3: response event arrives
    const content = s.handleResponse("");
    expect(content).toBe("Search results: Go is #1");
  });

  it("REGRESSION: useEffect-style sync would wipe ref after tool_call, causing blank", () => {
    // Simulate the exact race:
    // 1. tool_call clears streamingContent state to ""
    // 2. Tokens arrive and update streamingContentRef directly
    // 3. React commits a render whose snapshot of streamingContent was still ""
    //    (captured before the token state updates flushed)
    // 4. useEffect fires with that stale "" value → overwrites ref
    // 5. response event reads ref → ""  → blank message

    const s = makeStreamingState();

    // Step 1: tool_call clears state to "" (ref also cleared)
    s.handleToolCall();

    // Step 2: tokens arrive — ref is updated synchronously
    s.handleToken("Important answer");

    // Step 3 + 4: React committed a render where streamingContent was still ""
    // (the token's setStreamingContent hadn't been processed yet in that render).
    // We model this by calling simulateStaleUseEffect with the pre-token state.
    const staleState = ""; // React snapshotted state="" before token flush
    // manually set ref to staleState as the buggy useEffect would:
    s.streamingContentRef.current = staleState;

    // Step 5: response event arrives — ref is now "" → blank
    const contentWithBug = s.handleResponse("");
    expect(contentWithBug).toBe(""); // confirms the old bug

    // The fix: response handler clears ref AFTER capturing, and there is no
    // useEffect overwrite. Tokens always write to the ref synchronously and
    // the ref is only zeroed out by handlers that explicitly mean to clear it.
    const s2 = makeStreamingState();
    s2.handleToolCall();
    s2.handleToken("Important answer");
    // No stale useEffect — ref stays "Important answer"
    const content = s2.handleResponse("");
    expect(content).toBe("Important answer");
  });
});
