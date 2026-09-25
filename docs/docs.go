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
	"sort"
	"strings"
	"time"
	"unicode"

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
func ExtractPDFText(pdfData []byte) (text string, err error) {
	if len(pdfData) == 0 {
		return "", fmt.Errorf("empty PDF")
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("pdf parser panicked, recovered", "panic", r)
			text, err = "", fmt.Errorf("failed to parse PDF (malformed file)")
		}
	}()
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
		pageText, perr := extractPageLayout(page)
		if perr != nil {
			slog.Debug("pdf page text extraction failed", "page", i, "err", perr)
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(pageText)
	}
	return cleanMathPhantoms(sb.String()), nil
}

func cleanMathPhantoms(s string) string {
	s = strings.ReplaceAll(s, "−⊗", "− ")
	return strings.ReplaceAll(s, "·⊗", "· ")
}

const lineBaselineTol = 3.5

type textRun struct {
	frags []pdf.Text
	minX  float64
	endX  float64
	// spaced requests a space after this run
	spaced bool
}

type textLine struct {
	baseY float64
	runs  []textRun
}

// classifyFrag sorts fragments into content, spacing, or noise.
func classifyFrag(w pdf.Text) (keep, asSpace bool) {
	if w.S == "" {
		return false, false
	}
	if w.S == "Ω" && w.W <= 0 {
		return false, false
	}
	if r, ok := asSingleRune(w.S); ok {
		switch {
		case r == '\n' || r == '\t' || r == ' ':
			return true, true
		case unicode.IsControl(r):
			return false, false
		}
	}
	if strings.TrimSpace(w.S) == "" {
		return true, true
	}
	if w.S == "Ω" && isCMRFont(w.Font) {
		return true, true
	}
	return true, false
}

func asSingleRune(s string) (rune, bool) {
	r := []rune(s)
	if len(r) == 1 {
		return r[0], true
	}
	return 0, false
}

func isCMRFont(name string) bool {
	if i := strings.Index(name, "+"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "CMR")
}

func fragSize(a, b pdf.Text) float64 {
	if a.FontSize > 0 {
		return a.FontSize
	}
	if b.FontSize > 0 {
		return b.FontSize
	}
	return 12
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func buildLines(frags []pdf.Text) []textLine {
	var lines []textLine
	for _, w := range frags {
		keep, asSpace := classifyFrag(w)
		if !keep {
			continue
		}
		if asSpace {
			if n := len(lines); n > 0 && len(lines[n-1].runs) > 0 {
				lines[n-1].runs[len(lines[n-1].runs)-1].spaced = true
			}
			continue
		}
		if len(lines) == 0 || absf(w.Y-lines[len(lines)-1].baseY) > lineBaselineTol {
			lines = append(lines, textLine{baseY: w.Y})
		}
		L := &lines[len(lines)-1]
		if len(L.runs) == 0 {
			L.runs = append(L.runs, textRun{minX: w.X})
		}
		R := &L.runs[len(L.runs)-1]
		if len(R.frags) > 0 {
			prev := R.frags[len(R.frags)-1]
			if R.spaced || w.X-(prev.X+prev.W) >= 0.2*fragSize(prev, w) {
				L.runs = append(L.runs, textRun{minX: w.X})
				R = &L.runs[len(L.runs)-1]
			}
		}
		R.frags = append(R.frags, w)
		if w.X < R.minX {
			R.minX = w.X
		}
		if end := w.X + w.W; end > R.endX {
			R.endX = end
		}
	}
	return lines
}

// renderLines orders lines top-to-bottom and joins runs with spacing.
func renderLines(lines []textLine) string {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].baseY > lines[j].baseY })
	var sb strings.Builder
	for _, L := range lines {
		runs := append([]textRun(nil), L.runs...)
		sort.SliceStable(runs, func(i, j int) bool { return runs[i].minX < runs[j].minX })
		var lb strings.Builder
		for i, R := range runs {
			if i > 0 {
				sep := " "
				prev := runs[i-1]
				size := 12.0
				if len(prev.frags) > 0 && len(R.frags) > 0 {
					size = fragSize(prev.frags[len(prev.frags)-1], R.frags[0])
				}
				if !prev.spaced && R.minX-prev.endX >= 4*size {
					sep = "  " // column break
				}
				lb.WriteString(sep)
			}
			for _, w := range R.frags {
				lb.WriteString(w.S)
			}
		}
		if line := strings.TrimRight(lb.String(), " \t"); line != "" {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// extractPageLayout rebuilds a page's text from positioned glyph fragments,
// recovering word spaces from horizontal gaps.
func extractPageLayout(page pdf.Page) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("failed to parse PDF page: %v", r)
		}
	}()
	return renderLines(buildLines(page.Content().Text)), nil
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
