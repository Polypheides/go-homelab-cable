package player

import (
	"context"
	"io"
	"sync"
)

// StreamHub manages a shared ring buffer of MPEG-TS chunks
type StreamHub struct {
	buffer [][]byte
	size   int
	head   int64 // Monotonic index of the next chunk to be written
	closed bool  // Whether the hub is closed and no longer accepting data
	cond   *sync.Cond
}

// NewStreamHub initializes a new ring buffer hub for managing streaming data packets.
func NewStreamHub(size int) *StreamHub {
	return &StreamHub{
		buffer: make([][]byte, size),
		size:   size,
		cond:   sync.NewCond(&sync.Mutex{}),
	}
}

// Write adds a new data chunk to the ring buffer and notifies waiting readers.
func (h *StreamHub) Write(chunk []byte) {
	h.cond.L.Lock()
	if h.closed {
		h.cond.L.Unlock()
		return
	}
	idx := h.head % int64(h.size)
	if cap(h.buffer[idx]) >= len(chunk) {
		h.buffer[idx] = h.buffer[idx][:len(chunk)]
	} else {
		h.buffer[idx] = make([]byte, len(chunk))
	}
	copy(h.buffer[idx], chunk)
	h.head++
	h.cond.L.Unlock()
	h.cond.Broadcast()
}

// Close marks the hub as closed and notifies all blocked readers to exit.
func (h *StreamHub) Close() {
	h.cond.L.Lock()
	h.closed = true
	h.cond.L.Unlock()
	h.cond.Broadcast()
}

// Get retrieves a data chunk at the specified position, blocking until it becomes available.
func (h *StreamHub) Get(pos int64) ([]byte, int64, bool) {
	h.cond.L.Lock()
	defer h.cond.L.Unlock()

	if pos < 0 || h.head-pos > int64(h.size) {
		pos = h.head - 1
		if pos < 0 {
			pos = 0
		}
	}

	for h.head <= pos && !h.closed {
		h.cond.Wait()
	}

	if h.closed {
		return nil, pos, false
	}

	idx := pos % int64(h.size)
	chunk := h.buffer[idx]
	if chunk == nil {
		return nil, pos, false
	}
	return chunk, pos + 1, true
}

// Stream pipes data from the hub to a writer until the context is canceled.
func (h *StreamHub) Stream(ctx context.Context, w io.Writer) error {
	go func() {
		<-ctx.Done()
		h.cond.Broadcast()
	}()

	pos := h.LiveIndex() - 20
	if pos < 0 {
		pos = 0
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		chunk, nextPos, ok := h.Get(pos)
		if !ok {
			return nil
		}
		_, err := w.Write(chunk)
		if err != nil {
			return err
		}
		pos = nextPos
	}
}

// LiveIndex returns the current monotonic head position of the ring buffer.
func (h *StreamHub) LiveIndex() int64 {
	h.cond.L.Lock()
	defer h.cond.L.Unlock()
	return h.head
}
