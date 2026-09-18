package webworkspace

import "errors"

// ErrResourceNotFound deliberately covers both unknown resources and resources
// owned by another user. Callers must not be able to tell the two apart, so the
// API layer maps a single error to a single deny response.
var ErrResourceNotFound = errors.New("web workspace resource not found")
