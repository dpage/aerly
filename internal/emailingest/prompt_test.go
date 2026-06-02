package emailingest

import (
	"strings"
	"testing"
)

func TestBuildPrompt_TextAndHTML(t *testing.T) {
	p := &Parsed{TextBody: "hello world", HTMLBody: "<p>hi</p>"}
	body, docs := buildPrompt(p, 0)
	if !strings.Contains(body, "hello world") || !strings.Contains(body, "<p>hi</p>") {
		t.Errorf("body missing parts: %q", body)
	}
	if !strings.Contains(body, "text/plain") || !strings.Contains(body, "text/html") {
		t.Errorf("body missing section dividers: %q", body)
	}
	if len(docs) != 0 {
		t.Errorf("expected no docs, got %d", len(docs))
	}
}

func TestBuildPrompt_TextTruncated(t *testing.T) {
	p := &Parsed{TextBody: strings.Repeat("a", 1024)}
	body, _ := buildPrompt(p, 64)
	if len(body) != 64 {
		t.Errorf("body len = %d, want 64", len(body))
	}
}

func TestBuildPrompt_PDFsToDocs(t *testing.T) {
	p := &Parsed{PDFs: [][]byte{[]byte("%PDF-1.4 one"), []byte("%PDF-1.4 two")}}
	_, docs := buildPrompt(p, 0)
	if len(docs) != 2 {
		t.Fatalf("len(docs) = %d, want 2", len(docs))
	}
	for i, d := range docs {
		if d.MediaType != "application/pdf" {
			t.Errorf("doc %d mediaType = %q", i, d.MediaType)
		}
		if d.Filename == "" {
			t.Errorf("doc %d filename empty", i)
		}
	}
}

func TestBuildPrompt_DropsOversizedPDFs(t *testing.T) {
	big := make([]byte, maxDocBytes+1)
	small := []byte("%PDF-1.4 small")
	p := &Parsed{PDFs: [][]byte{big, small, big}}
	_, docs := buildPrompt(p, 0)
	if len(docs) != 1 {
		t.Fatalf("len(docs) = %d, want 1 (only the small PDF should survive)", len(docs))
	}
	if string(docs[0].Data) != "%PDF-1.4 small" {
		t.Errorf("kept the wrong doc: %q", docs[0].Data)
	}
}

func TestSanitizeHTML_StripsNoiseKeepsContent(t *testing.T) {
	in := `<head><style>.x{color:red}</style></head>` +
		`<body><!-- tracking --><img src="data:image/png;base64,QUJDREVGR0g=">` +
		`<p>Check-in 12 Jun 2026 — Marriott Tysons</p><script>track()</script></body>`
	out := sanitizeHTML(in)
	for _, noise := range []string{"color:red", "track()", "QUJDREVGR0g", "<!--"} {
		if strings.Contains(out, noise) {
			t.Errorf("expected %q to be stripped, got: %s", noise, out)
		}
	}
	if !strings.Contains(out, "Check-in 12 Jun 2026 — Marriott Tysons") {
		t.Errorf("reservation content lost: %s", out)
	}
}

func TestBuildPrompt_StyleNoiseDoesntTruncateReservation(t *testing.T) {
	// A big <style> block before the reservation would push it past a small
	// cap if not stripped; sanitizing keeps the reservation text in the prompt.
	big := "<style>" + strings.Repeat("x", 8000) + "</style>"
	p := &Parsed{HTMLBody: big + "<p>RESERVATION DETAIL HERE</p>"}
	body, _ := buildPrompt(p, 2000)
	if !strings.Contains(body, "RESERVATION DETAIL HERE") {
		t.Errorf("reservation truncated away by style noise: %q", body)
	}
}
