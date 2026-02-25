// CLAUDE:SUMMARY Sentinel errors for veille service: duplicate source, invalid input, quota exceeded.
package veille

import "errors"

// ErrDuplicateSource is returned when a source with the same URL already exists.
var ErrDuplicateSource = errors.New("veille: source with this URL already exists")

// ErrInvalidInput is returned when source input fails validation.
var ErrInvalidInput = errors.New("veille: invalid input")

// ErrQuotaExceeded is returned when a resource limit is reached.
var ErrQuotaExceeded = errors.New("veille: quota exceeded")
