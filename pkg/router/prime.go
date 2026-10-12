package router

import (
	"context"

	"amux-accounts/pkg/types"
)

// startStream sends req to a and waits for the stream's first meaningful
// chunk. Web adapters report usage limits, expired cookies and dead upstreams
// as an Error chunk after SendMessageStream has already returned; surfacing
// that error here lets the router cool the account down and fail over to the
// next one, exactly as for a synchronous error. Once any content, thinking or
// tool call has arrived the stream belongs to that account.
func startStream(ctx context.Context, a types.ProviderAdapter, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	src, err := a.SendMessageStream(ctx, req)
	if err != nil || src == nil {
		return src, err
	}
	var head []types.StreamChunk
	for {
		select {
		case <-ctx.Done():
			go drainChunks(src)
			return nil, ctx.Err()
		case c, ok := <-src:
			if !ok {
				return replayChunks(ctx, head, nil), nil
			}
			if c.Error != nil && !hasPayload(c) {
				go drainChunks(src)
				return nil, c.Error
			}
			head = append(head, c)
			if hasPayload(c) || c.Done {
				return replayChunks(ctx, head, src), nil
			}
		}
	}
}

func hasPayload(c types.StreamChunk) bool {
	return c.Content != "" || c.Thinking != "" || len(c.ToolCalls) > 0
}

// replayChunks re-emits head and then forwards the rest of src (if any).
func replayChunks(ctx context.Context, head []types.StreamChunk, src <-chan types.StreamChunk) <-chan types.StreamChunk {
	out := make(chan types.StreamChunk, len(head)+1)
	for _, c := range head {
		out <- c
	}
	if src == nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		for c := range src {
			select {
			case out <- c:
			case <-ctx.Done():
				drainChunks(src)
				return
			}
		}
	}()
	return out
}

func drainChunks(src <-chan types.StreamChunk) {
	for range src {
	}
}
