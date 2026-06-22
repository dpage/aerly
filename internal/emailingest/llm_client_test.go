package emailingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
)

// stubClient is a minimal in-memory llm.Client used to drive RealLLM.Complete
// without contacting any provider. Only Chat is exercised by Complete; the other
// interface methods are present to satisfy llm.Client and panic if called so a
// future change that starts using them is caught loudly.
type stubClient struct {
	// chat is invoked for each Chat call; attempt is the 0-based call index so a
	// stub can change its behaviour across retries.
	chat func(attempt int, req llm.ChatRequest) (*llm.ChatResponse, error)
	n    int
}

func (s *stubClient) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	attempt := s.n
	s.n++
	return s.chat(attempt, req)
}

func (s *stubClient) ChatStream(context.Context, llm.ChatRequest) (*llm.Stream, error) {
	panic("ChatStream not used by Complete")
}
func (s *stubClient) Embed(context.Context, string) ([]float64, error) {
	panic("Embed not used")
}
func (s *stubClient) EmbedBatch(context.Context, []string) ([][]float64, error) {
	panic("EmbedBatch not used")
}
func (s *stubClient) ListModels(context.Context) ([]string, error) { panic("ListModels not used") }
func (s *stubClient) ListModelsWithMetadata(context.Context) ([]llm.ModelInfo, error) {
	panic("ListModelsWithMetadata not used")
}
func (s *stubClient) Ping(context.Context) error { panic("Ping not used") }
func (s *stubClient) Provider() string           { return "stub" }
func (s *stubClient) Model() string              { return "stub-model" }
func (s *stubClient) Usage() llm.TokenUsage      { return llm.TokenUsage{} }
func (s *stubClient) ResetUsage()                {}

func textResp(s string) *llm.ChatResponse {
	return &llm.ChatResponse{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: s}}}
}

func TestNewRealLLM_UnknownProvider(t *testing.T) {
	// An unregistered provider name surfaces as a wrapped error rather than a
	// usable client — guards the constructor's error path.
	if _, err := NewRealLLM("definitely-not-a-provider", "m", "fake-key"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewRealLLM_KnownProvider(t *testing.T) {
	// anthropic is registered via the all-providers import; constructing a client
	// with a fake key must succeed (no network happens until a request is sent).
	c, err := NewRealLLM("anthropic", "claude-3-5-sonnet", "fake-key-not-real")
	if err != nil {
		t.Fatalf("NewRealLLM: %v", err)
	}
	if c == nil || c.Client == nil {
		t.Fatal("expected a non-nil client")
	}
}

func TestRealLLM_Complete_TextResponse(t *testing.T) {
	r := &RealLLM{Client: &stubClient{chat: func(_ int, req llm.ChatRequest) (*llm.ChatResponse, error) {
		// JSON response format must be requested.
		if req.ResponseFormat == nil || req.ResponseFormat.Type != llm.ResponseFormatJSON {
			t.Errorf("ResponseFormat = %+v, want JSON", req.ResponseFormat)
		}
		return textResp(`{"ok":true}`), nil
	}}}
	got, err := r.Complete(context.Background(), "prompt", nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("got %q", got)
	}
}

func TestRealLLM_Complete_SendsDocuments(t *testing.T) {
	docs := []Document{{Data: []byte("%PDF-1.4 synthetic"), MediaType: "application/pdf", Filename: "ticket.pdf"}}
	r := &RealLLM{Client: &stubClient{chat: func(_ int, req llm.ChatRequest) (*llm.ChatResponse, error) {
		blocks := req.Messages[0].Content
		// prompt text + one document block.
		if len(blocks) != 2 {
			t.Fatalf("blocks = %d, want 2 (text + doc)", len(blocks))
		}
		if blocks[1].Type != llm.BlockDocument || blocks[1].Document == nil {
			t.Fatalf("second block is not a document: %+v", blocks[1])
		}
		if blocks[1].Document.Filename != "ticket.pdf" {
			t.Errorf("doc filename = %q", blocks[1].Document.Filename)
		}
		return textResp("{}"), nil
	}}}
	if _, err := r.Complete(context.Background(), "prompt", docs); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestRealLLM_Complete_DocsRejectedRetriesTextOnly(t *testing.T) {
	docs := []Document{{Data: []byte("x"), MediaType: "application/pdf", Filename: "t.pdf"}}
	r := &RealLLM{Client: &stubClient{chat: func(attempt int, req llm.ChatRequest) (*llm.ChatResponse, error) {
		blocks := req.Messages[0].Content
		if attempt == 0 {
			// First call carries the document; reject it so Complete retries.
			if len(blocks) != 2 {
				t.Fatalf("first call blocks = %d, want 2", len(blocks))
			}
			return nil, llm.ErrNotSupported
		}
		// Retry must be text-only (no doc block).
		if len(blocks) != 1 {
			t.Fatalf("retry blocks = %d, want 1 (text-only)", len(blocks))
		}
		return textResp("{}"), nil
	}}}
	if _, err := r.Complete(context.Background(), "prompt", docs); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestRealLLM_Complete_TransientThenSuccess(t *testing.T) {
	r := &RealLLM{Client: &stubClient{chat: func(attempt int, _ llm.ChatRequest) (*llm.ChatResponse, error) {
		if attempt == 0 {
			return nil, errors.New("transient blip")
		}
		return textResp("{}"), nil
	}}}
	// The 1s backoff between attempts is real; give it room.
	if _, err := r.Complete(context.Background(), "prompt", nil); err != nil {
		t.Fatalf("Complete should recover after a transient error: %v", err)
	}
}

func TestRealLLM_Complete_AllAttemptsFail(t *testing.T) {
	r := &RealLLM{Client: &stubClient{chat: func(int, llm.ChatRequest) (*llm.ChatResponse, error) {
		return nil, errors.New("persistent boom")
	}}}
	_, err := r.Complete(context.Background(), "prompt", nil)
	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if !strings.Contains(err.Error(), "persistent boom") {
		t.Errorf("error should wrap the last failure: %v", err)
	}
}

func TestRealLLM_Complete_ContextCancelledNoRetry(t *testing.T) {
	r := &RealLLM{Client: &stubClient{chat: func(int, llm.ChatRequest) (*llm.ChatResponse, error) {
		return nil, context.Canceled
	}}}
	_, err := r.Complete(context.Background(), "prompt", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled (no retry)", err)
	}
}

func TestRealLLM_Complete_DeadlineDuringBackoff(t *testing.T) {
	// The shared deadline expires before the second attempt's backoff completes,
	// so Complete returns the context error from the backoff select.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r := &RealLLM{Client: &stubClient{chat: func(int, llm.ChatRequest) (*llm.ChatResponse, error) {
		return nil, errors.New("retryable")
	}}}
	_, err := r.Complete(ctx, "prompt", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestRealLLM_Complete_NoTextBlock(t *testing.T) {
	// A response with only non-text content yields a descriptive error.
	r := &RealLLM{Client: &stubClient{chat: func(int, llm.ChatRequest) (*llm.ChatResponse, error) {
		return &llm.ChatResponse{Content: []llm.ContentBlock{{Type: llm.BlockToolUse}}}, nil
	}}}
	_, err := r.Complete(context.Background(), "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "no text block") {
		t.Errorf("err = %v, want 'no text block'", err)
	}
}
