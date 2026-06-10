package emailingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all" // register all providers
)

// llmTimeout bounds a single LLM completion. The ingest pipeline runs on a
// process-lifetime context, so without a per-call deadline a stalled provider
// would block the email drain (and the HTTP propose handler) indefinitely.
const llmTimeout = 120 * time.Second

// RealLLM wraps an llm.Client and satisfies the LLM interface used by Extractor.
type RealLLM struct {
	Client llm.Client
}

// NewRealLLM constructs an LLM client via pgedge-go-llm-lib for the given
// provider ("anthropic", "openai", "gemini", "ollama") and model.
func NewRealLLM(provider, model, apiKey string) (*RealLLM, error) {
	c, err := llm.NewClient(provider, llm.Options{APIKey: apiKey, Model: model})
	if err != nil {
		return nil, fmt.Errorf("llm client (%s): %w", provider, err)
	}
	return &RealLLM{Client: c}, nil
}

// Complete sends prompt + any document attachments as a single user
// message and returns the model's first text content block, asking for
// JSON-formatted output. If the provider rejects documents
// (llm.ErrNotSupported, currently OpenAI and Ollama), Complete logs a
// warning and retries text-only so the email can still be partially
// processed.
// maxLLMAttempts bounds transient retries within the single llmTimeout
// deadline, so a flaky provider gets a few tries without re-processing the
// whole message (and re-sending docs) on every poll tick.
const maxLLMAttempts = 3

func (r *RealLLM) Complete(ctx context.Context, prompt string, docs []Document) (string, error) {
	// One overall deadline shared across all attempts (and the text-only
	// fallback), rather than a fresh timeout per recursion.
	callCtx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	withDocs := len(docs) > 0
	blocks := func() []llm.ContentBlock {
		bs := make([]llm.ContentBlock, 0, 1+len(docs))
		bs = append(bs, llm.TextBlock(prompt))
		if withDocs {
			for _, d := range docs {
				bs = append(bs, llm.DocumentBlock(d.Data, d.MediaType, d.Filename))
			}
		}
		return bs
	}

	var lastErr error
	for attempt := 0; attempt < maxLLMAttempts; attempt++ {
		if attempt > 0 {
			// Exponential-ish backoff, never beyond the shared deadline.
			select {
			case <-callCtx.Done():
				return "", callCtx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		resp, err := r.Client.Chat(callCtx, llm.ChatRequest{
			Messages:       []llm.Message{llm.UserBlocks(blocks()...)},
			ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSON},
		})
		if errors.Is(err, llm.ErrNotSupported) && withDocs {
			slog.Warn("emailingest: LLM provider rejected documents, retrying text-only",
				"docs", len(docs))
			withDocs = false
			attempt-- // the doc→text switch doesn't consume a retry
			continue
		}
		if err != nil {
			lastErr = err
			// A cancelled/expired context won't recover on retry.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			continue
		}
		for _, b := range resp.Content {
			if b.Type == llm.BlockText {
				return b.Text, nil
			}
		}
		return "", fmt.Errorf("llm response had no text block")
	}
	return "", fmt.Errorf("llm completion failed after %d attempts: %w", maxLLMAttempts, lastErr)
}
