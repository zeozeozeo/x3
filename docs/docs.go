package docs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

const (
	// MaxDownloadBytes caps attachment downloads (Discord and Gotenberg responses).
	MaxDownloadBytes = 10 * 1024 * 1024
	// MaxExtractChars caps extracted text per file before truncation.
	MaxExtractChars = 30000
	// MaxDocsPerMessage caps how many document attachments are parsed per message.
	MaxDocsPerMessage = 3
	// maxPDFPages caps pages read during text extraction.
	maxPDFPages = 100
	// convertTimeout bounds the Gotenberg round-trip.
	convertTimeout = 60 * time.Second
)

var officeExts = map[string]struct{}{
	".doc": {}, ".docx": {}, ".odt": {}, ".rtf": {},
	".xls": {}, ".xlsx": {}, ".ods": {}, ".csv": {}, ".tsv": {},
	".ppt": {}, ".pptx": {}, ".odp": {},
	".html": {}, ".htm": {}, ".xml": {}, ".md": {}, ".txt": {},
}

func Supported(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext == ".pdf" {
		return true
	}
	if _, ok := officeExts[ext]; ok {
		return true
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(ct, "application/pdf"):
		return true
	case strings.Contains(ct, "officedocument"), strings.Contains(ct, "msword"),
		strings.Contains(ct, "ms-excel"), strings.Contains(ct, "ms-powerpoint"),
		strings.Contains(ct, "opendocument"), strings.Contains(ct, "text/csv"):
		return true
	}
	return false
}

// IsPDF reports whether the attachment is already a PDF (no conversion needed).
func IsPDF(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext == ".pdf" {
		return true
	}
	return strings.Contains(strings.ToLower(contentType), "application/pdf")
}

// GotenbergURL returns the configured Gotenberg base URL (no trailing slash).
func GotenbergURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("X3_GOTENBERG_URL")), "/")
}

// ExtractText converts raw attachment bytes to LLM-ready text.
// It enforces the Gotenberg hard requirement, converts office docs to PDF,
// extracts PDF text, and truncates to MaxExtractChars.
func ExtractText(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	if strings.TrimSpace(filename) == "" {
		filename = "attachment"
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty file")
	}
	if len(data) > MaxDownloadBytes {
		return "", fmt.Errorf("file exceeds %d byte limit", MaxDownloadBytes)
	}
	baseURL := GotenbergURL()
	if baseURL == "" {
		return "", fmt.Errorf("document parsing requires Gotenberg (X3_GOTENBERG_URL is not configured)")
	}

	pdfData := data
	if !IsPDF(filename, contentType) {
		if !Supported(filename, contentType) {
			return "", fmt.Errorf("unsupported document type %q", filename)
		}
		converted, err := ConvertToPDF(ctx, baseURL, filename, data)
		if err != nil {
			return "", err
		}
		pdfData = converted
	}

	text, err := ExtractPDFText(pdfData)
	if err != nil {
		return "", err
	}
	text = normalizeText(text)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no extractable text in %q (scanned/image-only PDFs are not supported)", filename)
	}
	if len(text) > MaxExtractChars {
		text = text[:MaxExtractChars] + fmt.Sprintf("\n[…truncated, %d chars total]", len(text))
	}
	return text, nil
}

// ConvertToPDF sends an office document to Gotenberg LibreOffice and returns PDF bytes.
func ConvertToPDF(ctx context.Context, baseURL, filename string, data []byte) ([]byte, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("document parsing requires Gotenberg (X3_GOTENBERG_URL is not configured)")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	if len(data) > MaxDownloadBytes {
		return nil, fmt.Errorf("file exceeds %d byte limit", MaxDownloadBytes)
	}

	ctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, escapeFilename(filename)))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/forms/libreoffice/convert", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/pdf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg convert failed: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxDownloadBytes)+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gotenberg convert failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	if len(out) > MaxDownloadBytes {
		return nil, fmt.Errorf("converted PDF exceeds %d byte limit", MaxDownloadBytes)
	}
	if len(out) == 0 || !bytes.HasPrefix(bytes.TrimSpace(out), []byte("%PDF")) {
		slog.Warn("gotenberg convert returned non-PDF bytes", "size", len(out), "filename", filename)
	}
	return out, nil
}

// ExtractPDFText reads plain text from PDF bytes (up to maxPDFPages pages).
func ExtractPDFText(pdfData []byte) (string, error) {
	if len(pdfData) == 0 {
		return "", fmt.Errorf("empty PDF")
	}
	reader, err := pdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("failed to parse PDF: %w", err)
	}
	numPages := reader.NumPage()
	if numPages > maxPDFPages {
		numPages = maxPDFPages
	}
	var sb strings.Builder
	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		rows, err := page.GetTextByRow()
		if err != nil {
			slog.Debug("pdf page text extraction failed", "page", i, "err", err)
			continue
		}
		for _, row := range rows {
			var line strings.Builder
			for _, word := range row.Content {
				line.WriteString(word.S)
			}
			sb.WriteString(strings.TrimRight(line.String(), " \t"))
			sb.WriteByte('\n')
		}
	}
	return sb.String(), nil
}

// FormatBlock renders extracted text as a context block for the LLM.
func FormatBlock(filename, text string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "attachment"
	}
	return fmt.Sprintf("[attachment %s]\n%s", filename, strings.TrimSpace(text))
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func escapeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, `"`, "_")
	if name == "" || name == "." {
		return "document"
	}
	return name
}
