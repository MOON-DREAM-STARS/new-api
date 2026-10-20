package common

// ContextKeyResponsesTerminalEventSent marks a Responses SSE stream whose
// terminal event has already been written to the client. Relay's generic error
// defer must not append a JSON error body after that terminal event.
const ContextKeyResponsesTerminalEventSent = "responses_terminal_event_sent"
