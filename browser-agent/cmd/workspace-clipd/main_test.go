package main

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseRequestLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOp   string
		wantMIME string
		wantLen  int64
		wantCode string
		wantBody bool
	}{
		{name: "copy", line: `{"op":"copy"}` + "\n", wantOp: "copy"},
		{name: "paste text", line: `{"op":"paste","mime":"text/plain","length":3}` + "\n", wantOp: "paste", wantMIME: "text/plain", wantLen: 3, wantBody: true},
		{name: "paste zero length", line: `{"op":"paste","mime":"image/webp","length":0}` + "\n", wantOp: "paste", wantMIME: "image/webp", wantLen: 0, wantBody: true},
		{name: "unknown field", line: `{"op":"copy","extra":true}` + "\n", wantCode: errProtocol},
		{name: "unknown op", line: `{"op":"cut"}` + "\n", wantCode: errProtocol},
		{name: "copy with length", line: `{"op":"copy","length":1}` + "\n", wantCode: errProtocol},
		{name: "paste missing length", line: `{"op":"paste","mime":"text/plain"}` + "\n", wantCode: errProtocol},
		{name: "paste unsupported mime", line: `{"op":"paste","mime":"application/octet-stream","length":1}` + "\n", wantCode: errMIMEUnsupported},
		{name: "paste negative length", line: `{"op":"paste","mime":"text/plain","length":-1}` + "\n", wantCode: errProtocol},
		{name: "paste too large", line: `{"op":"paste","mime":"text/plain","length":8388609}` + "\n", wantCode: errPayloadTooLarge},
		{name: "malformed", line: `{"op":` + "\n", wantCode: errProtocol},
		{name: "trailing json", line: `{"op":"copy"}{"op":"copy"}` + "\n", wantCode: errProtocol},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, clipErr := parseRequestLine([]byte(test.line))
			if test.wantCode != "" {
				if clipErr == nil || clipErr.code != test.wantCode {
					t.Fatalf("parseRequestLine() error = %v, want %s", clipErr, test.wantCode)
				}
				return
			}
			if clipErr != nil {
				t.Fatalf("parseRequestLine() unexpected error = %v", clipErr)
			}
			if req.Op != test.wantOp || req.MIME != test.wantMIME {
				t.Fatalf("parseRequestLine() = op %q mime %q, want op %q mime %q", req.Op, req.MIME, test.wantOp, test.wantMIME)
			}
			if test.wantBody {
				if req.Length == nil || *req.Length != test.wantLen {
					t.Fatalf("parseRequestLine() length = %v, want %d", req.Length, test.wantLen)
				}
			} else if req.Length != nil {
				t.Fatalf("parseRequestLine() length = %v, want nil", req.Length)
			}
		})
	}
}

func TestCopyFromClipboardEmptySelection(t *testing.T) {
	readCalls := 0
	read := func(string) ([]byte, error) {
		readCalls++
		return nil, errClipXclipFailed
	}

	mime, payload, clipErr := copyFromClipboard(nil, false, read)
	if clipErr != nil {
		t.Fatalf("copyFromClipboard() error = %v, want nil", clipErr)
	}
	if mime != "text/plain" || len(payload) != 0 {
		t.Fatalf("copyFromClipboard() = %q with %d bytes, want empty text/plain", mime, len(payload))
	}
	if readCalls != 0 {
		t.Fatalf("copyFromClipboard() read the selection %d times, want 0 when no owner exists", readCalls)
	}
}

func TestCopyFromClipboardOwnedSelection(t *testing.T) {
	targets := []string{"TARGETS", "UTF8_STRING"}

	mime, payload, clipErr := copyFromClipboard(targets, true, func(string) ([]byte, error) {
		return []byte("hi"), nil
	})
	if clipErr != nil {
		t.Fatalf("copyFromClipboard() error = %v, want nil", clipErr)
	}
	if mime != "text/plain" || string(payload) != "hi" {
		t.Fatalf("copyFromClipboard() = %q %q, want text/plain hi", mime, string(payload))
	}

	if _, _, clipErr := copyFromClipboard(targets, true, func(string) ([]byte, error) {
		return nil, errClipXclipFailed
	}); clipErr != errClipXclipFailed {
		t.Fatalf("copyFromClipboard() error = %v, want %s", clipErr, errClipXclipFailed)
	}
}

func TestCopyCandidatesForTargets(t *testing.T) {
	tests := []struct {
		name    string
		targets []string
		want    []string
	}{
		{name: "text selection", targets: []string{"TARGETS", "UTF8_STRING", "TEXT", "STRING"}, want: []string{"text/plain"}},
		{name: "image and text selection", targets: []string{"image/png", "UTF8_STRING"}, want: []string{"image/png", "text/plain"}},
		{name: "unknown targets fall back", targets: []string{"TARGETS", "TIMESTAMP"}, want: copyMIMEs},
		{name: "missing targets fall back", want: copyMIMEs},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := copyCandidatesForTargets(test.targets)
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("copyCandidatesForTargets() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSelectCopyMIME(t *testing.T) {
	t.Run("first non-empty target wins", func(t *testing.T) {
		var seen []string
		mime, payload, clipErr := selectCopyMIME(func(mime string) ([]byte, error) {
			seen = append(seen, mime)
			switch mime {
			case "image/png":
				return nil, errors.New("not available")
			case "image/jpeg":
				return []byte{}, nil
			case "image/webp":
				return []byte("webp-data"), nil
			default:
				return []byte("text-data"), nil
			}
		})
		if clipErr != nil {
			t.Fatalf("selectCopyMIME() unexpected error = %v", clipErr)
		}
		if mime != "image/webp" || string(payload) != "webp-data" {
			t.Fatalf("selectCopyMIME() = %q %q, want image/webp webp-data", mime, payload)
		}
		wantSeen := []string{"image/png", "image/jpeg", "image/webp"}
		if strings.Join(seen, ",") != strings.Join(wantSeen, ",") {
			t.Fatalf("selectCopyMIME() tried %v, want %v", seen, wantSeen)
		}
	})

	t.Run("empty targets fail as xclip", func(t *testing.T) {
		_, _, clipErr := selectCopyMIME(func(string) ([]byte, error) {
			return []byte{}, nil
		})
		if clipErr == nil || clipErr.code != errXclipFailed {
			t.Fatalf("selectCopyMIME() error = %v, want %s", clipErr, errXclipFailed)
		}
	})

	t.Run("all command errors fail as xclip", func(t *testing.T) {
		_, _, clipErr := selectCopyMIME(func(string) ([]byte, error) {
			return nil, errors.New("xclip failed")
		})
		if clipErr == nil || clipErr.code != errXclipFailed {
			t.Fatalf("selectCopyMIME() error = %v, want %s", clipErr, errXclipFailed)
		}
	})

	t.Run("payload limit is fatal immediately", func(t *testing.T) {
		_, _, clipErr := selectCopyMIME(func(string) ([]byte, error) {
			return nil, errClipPayloadTooLarge
		})
		if clipErr == nil || clipErr.code != errPayloadTooLarge {
			t.Fatalf("selectCopyMIME() error = %v, want %s", clipErr, errPayloadTooLarge)
		}
	})
}

func TestPasteTarget(t *testing.T) {
	tests := map[string]string{
		"text/plain": "UTF8_STRING",
		"image/png":  "image/png",
		"image/jpeg": "image/jpeg",
		"image/webp": "image/webp",
	}
	for mime, want := range tests {
		got, ok := pasteTarget(mime)
		if !ok || got != want {
			t.Fatalf("pasteTarget(%q) = %q, %v; want %q, true", mime, got, ok, want)
		}
	}
	if _, ok := pasteTarget("application/octet-stream"); ok {
		t.Fatal("pasteTarget() accepted an unsupported mime")
	}
}

func TestReadPayload(t *testing.T) {
	payload, err := readPayload(strings.NewReader("abc"), 3)
	if err != nil || string(payload) != "abc" {
		t.Fatalf("readPayload() = %q, %v; want abc, nil", payload, err)
	}

	if _, err := readPayload(strings.NewReader("ab"), 3); err == nil || err.Error() != errProtocol {
		t.Fatalf("readPayload() short body error = %v, want %s", err, errProtocol)
	}

	if _, err := readPayload(strings.NewReader(""), maxPayloadBytes+1); err == nil || err.Error() != errPayloadTooLarge {
		t.Fatalf("readPayload() oversized error = %v, want %s", err, errPayloadTooLarge)
	}
}

func TestReadRequestLineLimit(t *testing.T) {
	line := strings.Repeat("x", int(maxPayloadBytes)+1) + "\n"
	_, err := readRequestLine(bufio.NewReader(strings.NewReader(line)))
	if err == nil || err.Error() != errPayloadTooLarge {
		t.Fatalf("readRequestLine() error = %v, want %s", err, errPayloadTooLarge)
	}
}

func TestWriteJSONLine(t *testing.T) {
	var output bytes.Buffer
	if err := writeJSONLine(&output, copyResponse{OK: true, MIME: "text/plain", Length: 0}); err != nil {
		t.Fatalf("writeJSONLine() error = %v", err)
	}
	want := "{\"ok\":true,\"mime\":\"text/plain\",\"length\":0}\n"
	if output.String() != want {
		t.Fatalf("writeJSONLine() = %q, want %q", output.String(), want)
	}
}
