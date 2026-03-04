import { writable, derived } from "svelte/store";
import type { ChatMessage, Conversation } from "../types";

export interface AiState {
  conversations: Conversation[];
  activeConversationId: string;
  messages: ChatMessage[];
  streaming: boolean;
  currentChunks: string;
  visible: boolean;
}

export const ai = writable<AiState>({
  conversations: [],
  activeConversationId: "",
  messages: [],
  streaming: false,
  currentChunks: "",
  visible: false,
});

export const activeConversation = derived(ai, ($ai) =>
  $ai.conversations.find((c) => c.id === $ai.activeConversationId)
);

export function toggleAiPanel() {
  ai.update((s) => ({ ...s, visible: !s.visible }));
}

export function setConversations(convs: Conversation[]) {
  ai.update((s) => ({ ...s, conversations: convs }));
}

export function setActiveConversation(id: string, messages: ChatMessage[]) {
  ai.update((s) => ({
    ...s,
    activeConversationId: id,
    messages,
    streaming: false,
    currentChunks: "",
  }));
}

export function addConversation(conv: Conversation) {
  ai.update((s) => ({
    ...s,
    conversations: [conv, ...s.conversations],
    activeConversationId: conv.id,
    messages: [],
    streaming: false,
    currentChunks: "",
  }));
}

export function removeConversation(id: string) {
  ai.update((s) => {
    const filtered = s.conversations.filter((c) => c.id !== id);
    const newActive =
      s.activeConversationId === id
        ? filtered[0]?.id ?? ""
        : s.activeConversationId;
    return {
      ...s,
      conversations: filtered,
      activeConversationId: newActive,
      messages: newActive !== s.activeConversationId ? [] : s.messages,
    };
  });
}

export function updateConversationTitle(id: string, title: string) {
  ai.update((s) => ({
    ...s,
    conversations: s.conversations.map((c) =>
      c.id === id ? { ...c, title } : c
    ),
  }));
}

export function addUserMessage(content: string) {
  ai.update((s) => ({
    ...s,
    messages: [
      ...s.messages,
      { role: "user" as const, content, timestamp: Date.now() },
    ],
    streaming: true,
    currentChunks: "",
  }));
}

export function appendChunk(text: string) {
  ai.update((s) => ({
    ...s,
    currentChunks: s.currentChunks + text,
  }));
}

export function finishStreaming() {
  ai.update((s) => ({
    ...s,
    messages: [
      ...s.messages,
      {
        role: "assistant" as const,
        content: s.currentChunks,
        timestamp: Date.now(),
      },
    ],
    streaming: false,
    currentChunks: "",
  }));
}

export function clearChat() {
  ai.update((s) => ({
    ...s,
    messages: [],
    streaming: false,
    currentChunks: "",
  }));
}
