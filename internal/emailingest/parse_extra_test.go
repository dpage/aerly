package emailingest

import (
	"errors"
	"strings"
	"testing"
)

// errReader fails on the first Read so the readEncoded/walkBody error paths can
// be exercised without a real malformed stream.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom read") }

func TestReadEncoded_QuotedPrintable(t *testing.T) {
	out, err := readEncoded(strings.NewReader("a=3Db"), "quoted-printable")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a=b" {
		t.Errorf("got %q, want a=b", out)
	}
}

func TestReadEncoded_Base64UnpaddedFallback(t *testing.T) {
	// "hi" base64-encodes to "aGk=" with padding; strip the padding so the
	// standard (padded) decode fails and the raw (unpadded) fallback kicks in.
	out, err := readEncoded(strings.NewReader("aGk"), "base64")
	if err != nil {
		t.Fatalf("unpadded base64 should decode via the raw fallback: %v", err)
	}
	if string(out) != "hi" {
		t.Errorf("got %q, want hi", out)
	}
}

func TestReadEncoded_Base64ReadError(t *testing.T) {
	if _, err := readEncoded(errReader{}, "base64"); err == nil {
		t.Error("expected a read error to propagate from base64 decode")
	}
}

func TestWalkBody_TextReadError(t *testing.T) {
	out := &Parsed{}
	if err := walkBody(errReader{}, "text/plain", nil, "base64", out); err == nil {
		t.Error("expected read error from a text/plain part")
	}
}

func TestWalkBody_HTMLReadError(t *testing.T) {
	out := &Parsed{}
	if err := walkBody(errReader{}, "text/html", nil, "base64", out); err == nil {
		t.Error("expected read error from a text/html part")
	}
}

func TestWalkBody_PDFReadError(t *testing.T) {
	out := &Parsed{}
	if err := walkBody(errReader{}, "application/pdf", nil, "base64", out); err == nil {
		t.Error("expected read error from an application/pdf part")
	}
}

func TestWalkBody_MultipartNextPartError(t *testing.T) {
	// A multipart stream whose body abruptly errors mid-part surfaces the
	// NextPart error rather than treating it as a clean EOF.
	out := &Parsed{}
	err := walkBody(errReader{}, "multipart/mixed", map[string]string{"boundary": "B"}, "", out)
	if err == nil {
		t.Error("expected a NextPart error from a broken multipart body")
	}
}

func TestParse_PDFViaWalk(t *testing.T) {
	// A multipart message with a base64 PDF part exercises the application/pdf
	// branch of walkBody through the public Parse entry point.
	raw := "From: a@example.com\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n" +
		"--B\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		"JVBERi0xLjQK\r\n" + // "%PDF-1.4\n"
		"--B--\r\n"
	p, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.PDFs) != 1 || !strings.HasPrefix(string(p.PDFs[0]), "%PDF-1.4") {
		t.Errorf("PDF not decoded: %q", p.PDFs)
	}
}

func TestDecodeRFC2047_InvalidReturnedRaw(t *testing.T) {
	// An unknown charset makes DecodeHeader error; decodeRFC2047 swallows the
	// error and hands back the original header value untouched.
	in := "=?bogus-charset?B?aGk=?="
	if got := decodeRFC2047(in); got != in {
		t.Errorf("decodeRFC2047(%q) = %q, want the input back unchanged", in, got)
	}
}

func TestQuotedPrintable_EncodesSpecials(t *testing.T) {
	// Thin alias over the mailer helper; encode a non-ASCII byte and confirm the
	// QP soft-wrap/escape form comes back rather than the raw rune.
	got := quotedPrintable("café")
	if got == "café" {
		t.Errorf("expected quoted-printable encoding, got the raw string %q", got)
	}
	if !strings.Contains(got, "=") {
		t.Errorf("quoted-printable output missing escape: %q", got)
	}
}
