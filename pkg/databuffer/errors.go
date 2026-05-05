package databuffer

import "errors"

// ErrBufferClosed is returned when operations are attempted on a closed buffer.
var ErrBufferClosed = errors.New("buffer is closed")

// ErrBufferEmpty is returned when trying to read from an empty buffer.
var ErrBufferEmpty = errors.New("buffer is empty")
