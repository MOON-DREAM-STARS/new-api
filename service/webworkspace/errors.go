package webworkspace

import "errors"

// ErrResourceNotFound deliberately covers both unknown resources and resources
// owned by another user. Callers must not be able to tell the two apart, so the
// API layer maps a single error to a single deny response.
var ErrResourceNotFound = errors.New("web workspace resource not found")

// File chooser bridge errors are stable control-plane classifications. The
// agent reports the underlying cause without exposing a path or CDP identifier.
var (
	ErrFileChooserExpired = errors.New("web workspace file chooser expired")
	ErrFileTooLarge       = errors.New("web workspace file too large")
	ErrFileLimitExceeded  = errors.New("web workspace file limit exceeded")
	ErrFileInjectFailed   = errors.New("web workspace file injection failed")
	ErrFileBridgeBusy     = errors.New("web workspace file bridge busy")
	ErrInvalidFileChooser = errors.New("web workspace file chooser request invalid")

	ErrClipboardUnavailable     = errors.New("web workspace clipboard unavailable")
	ErrClipboardPayloadTooLarge = errors.New("web workspace clipboard payload too large")
	ErrClipboardMIMEUnsupported = errors.New("web workspace clipboard mime unsupported")
	ErrClipboardFailed          = errors.New("web workspace clipboard failed")
	ErrInvalidClipboard         = errors.New("web workspace clipboard request invalid")

	ErrInputUnavailable = errors.New("web workspace input unavailable")
	ErrInputTimeout     = errors.New("web workspace input timeout")
	ErrInputRejected    = errors.New("web workspace input rejected")
)
