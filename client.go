package enconvert

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultBaseURL           = "https://api.enconvert.com"
	defaultTimeout           = 300 * time.Second
	defaultBatchPollInterval = 5 * time.Second
	defaultBatchTimeout      = 30 * time.Minute
	defaultPollMaxWait       = 300 * time.Second
	defaultPollInterval      = 3 * time.Second

	// defaultUserAgent identifies SDK traffic to the gateway (surface
	// attribution); the "enconvert-sdk/" prefix is what the gateway matches.
	defaultUserAgent = "enconvert-sdk/" + Version + " (go)"
)

// Client is the Enconvert file conversion client.
//
// Example:
//
//	client, err := enconvert.New("sk_...")
//	if err != nil { ... }
//	result, err := client.ConvertURLToPDF(ctx, "https://example.com", enconvert.URLToPDFOptions{})
type Client struct {
	apiKey     string
	baseURL    string
	timeout    time.Duration
	userAgent  string
	httpClient *http.Client

	// V2 is the V2 API namespace: perceive, discover, lookup, distill,
	// ingest, watch. Requires a private API key; endpoints are plan-gated
	// (QuotaError on 402).
	V2 *V2
}

// Option configures a Client constructed with New.
type Option func(*Client)

// WithTimeout overrides the default per-request timeout (300 seconds).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithBaseURL overrides the API base URL (default
// "https://api.enconvert.com"). Trailing slashes are stripped.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithUserAgent overrides the User-Agent header sent on every API request
// (default "enconvert-sdk/<Version> (go)").
func WithUserAgent(userAgent string) Option {
	return func(c *Client) { c.userAgent = userAgent }
}

// New constructs a Client. apiKey is required.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("enconvert: apiKey is required")
	}
	c := &Client{
		apiKey:    apiKey,
		baseURL:   defaultBaseURL,
		timeout:   defaultTimeout,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.httpClient = &http.Client{Timeout: c.timeout}
	c.V2 = newV2(c)
	return c, nil
}

// ------------------------------------------------------------------
// URL conversions (single page)
// ------------------------------------------------------------------

// ConvertURLToPDF converts a URL to PDF.
func (c *Client) ConvertURLToPDF(ctx context.Context, url string, opts URLToPDFOptions) (ConversionResult, error) {
	body := buildURLBody(url, opts.URLRenderOptions)
	singlePage := true
	if opts.SinglePage != nil {
		singlePage = *opts.SinglePage
	}
	body["single_page"] = singlePage
	if opts.PDFOptions != nil {
		body["pdf_options"] = serializePDFOptions(opts.PDFOptions)
	}

	data, err := c.postJSON(ctx, "/v1/convert/url-to-pdf", body, true)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ConvertURLToScreenshot converts a URL to a PNG screenshot.
func (c *Client) ConvertURLToScreenshot(ctx context.Context, url string, opts URLToScreenshotOptions) (ConversionResult, error) {
	body := buildURLBody(url, opts.URLRenderOptions)

	data, err := c.postJSON(ctx, "/v1/convert/url-to-screenshot", body, true)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ConvertURLToMarkdown converts a URL to clean GitHub-Flavored Markdown with
// YAML frontmatter (title, description, url, links, images). Strips
// nav/footer/ads/scripts and extracts the main article content.
func (c *Client) ConvertURLToMarkdown(ctx context.Context, url string, opts URLToMarkdownOptions) (ConversionResult, error) {
	body := buildURLBody(url, opts.URLRenderOptions)

	data, err := c.postJSON(ctx, "/v1/convert/url-to-markdown", body, true)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ------------------------------------------------------------------
// Website conversions (async batch, whole-site crawl)
// ------------------------------------------------------------------

// ConvertWebsiteToPDF converts every discovered page of a website to PDF.
// Async-only: pages are discovered via sitemap or full crawl
// (plan-dependent), converted in the background, and bundled into a single
// ZIP. Poll with GetBatchStatus or block with WaitForBatch. Requires a
// private API key with crawl access.
func (c *Client) ConvertWebsiteToPDF(ctx context.Context, url string, opts WebsiteToPDFOptions) (BatchSubmission, error) {
	body := buildWebsiteBody(url, opts.WebsiteConversionOptions)
	if opts.SinglePage != nil {
		body["single_page"] = *opts.SinglePage
	}
	if opts.PDFOptions != nil {
		body["pdf_options"] = serializePDFOptions(opts.PDFOptions)
	}

	// No job-polling fallback: website submissions have no per-job row, so a
	// 5xx here means the submission itself failed and must surface directly.
	data, err := c.postJSON(ctx, "/v1/convert/website-to-pdf", body, false)
	if err != nil {
		return BatchSubmission{}, err
	}
	return toBatchSubmission(data), nil
}

// ConvertWebsiteToScreenshot screenshots every discovered page of a website
// (PNG). Async-only, bundled into a single ZIP. Poll with GetBatchStatus or
// block with WaitForBatch. Requires a private API key with crawl access.
func (c *Client) ConvertWebsiteToScreenshot(ctx context.Context, url string, opts WebsiteToScreenshotOptions) (BatchSubmission, error) {
	body := buildWebsiteBody(url, opts.WebsiteConversionOptions)

	data, err := c.postJSON(ctx, "/v1/convert/website-to-screenshot", body, false)
	if err != nil {
		return BatchSubmission{}, err
	}
	return toBatchSubmission(data), nil
}

// ------------------------------------------------------------------
// File conversions
// ------------------------------------------------------------------

// ConvertImage converts an image between formats (jpeg, png, svg, heic,
// webp), or rasterizes a PDF to JPEG. Only pairs implemented by the API are
// accepted; unsupported pairs return an error before any request is made.
// file may be a FilePath, FileBytes, or FileInput.
func (c *Client) ConvertImage(ctx context.Context, file FileSource, opts ConvertImageOptions) (ConversionResult, error) {
	part, err := file.resolve()
	if err != nil {
		return ConversionResult{}, err
	}
	inputFormat, err := resolveInputFormat(part.filename, imageFormats)
	if err != nil {
		return ConversionResult{}, err
	}
	outputFmt := normalizeOutputFormat(opts.OutputFormat)
	endpoint, err := assertConversionImplemented(inputFormat, outputFmt)
	if err != nil {
		return ConversionResult{}, err
	}

	data, err := c.postFile(ctx, "/v1/convert/"+endpoint, part, opts.OutputFilename, nil)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ConvertDocument converts a document (doc, excel, ppt, odt, ods, odp, ots,
// pages, numbers, html, markdown, csv, json, xml, yaml, toml). Output
// defaults to pdf. Only pairs implemented by the API are accepted;
// unsupported pairs return an error before any request is made. file may be
// a FilePath, FileBytes, or FileInput. (EPUB has no dedicated document pair
// — use ConvertToPDF / ConvertToMarkdown.)
func (c *Client) ConvertDocument(ctx context.Context, file FileSource, opts ConvertDocumentOptions) (ConversionResult, error) {
	part, err := file.resolve()
	if err != nil {
		return ConversionResult{}, err
	}
	inputFormat, err := resolveInputFormat(part.filename, documentFormats)
	if err != nil {
		return ConversionResult{}, err
	}
	outputFormat := opts.OutputFormat
	if outputFormat == "" {
		outputFormat = "pdf"
	}
	outputFmt := normalizeOutputFormat(outputFormat)
	endpoint, err := assertConversionImplemented(inputFormat, outputFmt)
	if err != nil {
		return ConversionResult{}, err
	}

	data, err := c.postFile(ctx, "/v1/convert/"+endpoint, part, opts.OutputFilename, opts.PDFOptions)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ConvertToMarkdown converts an uploaded file of (almost) any document
// format to clean Markdown — PDF, DOCX, PPTX, XLSX, CSV, HTML, EPUB, TXT/MD,
// and legacy/ODF office. The format is auto-detected server-side; a
// RAG-ingestion building block. Images are not supported. file may be a
// FilePath, FileBytes, or FileInput.
func (c *Client) ConvertToMarkdown(ctx context.Context, file FileSource, opts ConvertToMarkdownOptions) (ConversionResult, error) {
	part, err := file.resolve()
	if err != nil {
		return ConversionResult{}, err
	}

	data, err := c.postFile(ctx, "/v1/convert/anything-to-markdown", part, opts.OutputFilename, nil)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ConvertToPDF converts an uploaded file of (almost) any format to PDF —
// office/ODF/Pages/Numbers/RTF/CSV, HTML, Markdown, text, raster images,
// SVG, EPUB, or an existing PDF (passthrough/normalise). The format is
// auto-detected server-side. Only PDFOptions.Grayscale is honored on this
// endpoint. file may be a FilePath, FileBytes, or FileInput.
func (c *Client) ConvertToPDF(ctx context.Context, file FileSource, opts ConvertToPDFOptions) (ConversionResult, error) {
	part, err := file.resolve()
	if err != nil {
		return ConversionResult{}, err
	}

	data, err := c.postFile(ctx, "/v1/convert/anything-to-pdf", part, opts.OutputFilename, opts.PDFOptions)
	if err != nil {
		return ConversionResult{}, err
	}
	result := toConversionResult(data)
	if opts.SaveTo != "" {
		if err := c.download(ctx, result.PresignedURL, opts.SaveTo); err != nil {
			return ConversionResult{}, err
		}
	}
	return result, nil
}

// ------------------------------------------------------------------
// Job + batch status
// ------------------------------------------------------------------

// GetJobStatus polls the status of an async conversion job.
func (c *Client) GetJobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	data, err := c.getJSON(ctx, "/v1/convert/status/"+jobID)
	if err != nil {
		return JobStatus{}, err
	}
	return toJobStatus(data), nil
}

// GetBatchStatus gets the status of an async batch (website conversion).
// Returns aggregate counts, per-URL statuses, and download URLs. Private
// API keys only.
func (c *Client) GetBatchStatus(ctx context.Context, batchID string) (BatchStatus, error) {
	data, err := c.getJSON(ctx, "/v1/convert/batch/"+batchID)
	if err != nil {
		return BatchStatus{}, err
	}
	return toBatchStatus(data), nil
}

// WaitForBatch polls a batch until it leaves "processing", then returns its
// final status. With opts.SaveTo, downloads the batch ZIP once available.
// Returns an *APIError with status 504 on timeout.
func (c *Client) WaitForBatch(ctx context.Context, batchID string, opts WaitForBatchOptions) (BatchStatus, error) {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultBatchPollInterval
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultBatchTimeout
	}
	deadline := time.Now().Add(timeout)

	for {
		status, err := c.GetBatchStatus(ctx, batchID)
		if err != nil {
			return BatchStatus{}, err
		}
		if status.Status != BatchStatusProcessing {
			if opts.SaveTo != "" {
				if status.ZipDownloadURL == "" {
					return BatchStatus{}, NewAPIError(500, fmt.Sprintf(
						"Batch %s finished with status '%s' but no ZIP is available to save", batchID, status.Status))
				}
				if err := c.download(ctx, status.ZipDownloadURL, opts.SaveTo); err != nil {
					return BatchStatus{}, err
				}
			}
			return status, nil
		}
		if !time.Now().Before(deadline) {
			return BatchStatus{}, NewAPIError(504, fmt.Sprintf("Batch %s did not complete within %s", batchID, timeout))
		}
		select {
		case <-ctx.Done():
			return BatchStatus{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ------------------------------------------------------------------
// File input
// ------------------------------------------------------------------

// filePart is the normalized form of any FileSource, ready for upload.
type filePart struct {
	data        []byte
	filename    string
	contentType string
}

// FileSource is the input accepted by ConvertImage and ConvertDocument. Use
// FilePath for a filesystem path, FileBytes for raw bytes (defaults to
// filename "upload.bin"), or FileInput for raw bytes with an explicit
// filename and content type.
type FileSource interface {
	resolve() (filePart, error)
}

// FilePath is a filesystem path to the file to convert. The filename
// (basename) determines the input format and, unless overridden via
// FileInput, the multipart content type.
type FilePath string

func (p FilePath) resolve() (filePart, error) {
	data, err := os.ReadFile(string(p))
	if err != nil {
		return filePart{}, err
	}
	filename := filepath.Base(string(p))
	return filePart{data: data, filename: filename, contentType: mimeFor(filename)}, nil
}

// FileBytes is raw file content with no associated filename. The upload is
// named "upload.bin" with content type "application/octet-stream" — use
// FileInput to set an explicit filename/content type.
type FileBytes []byte

func (b FileBytes) resolve() (filePart, error) {
	return filePart{data: b, filename: "upload.bin", contentType: "application/octet-stream"}, nil
}

// FileInput is raw file content with an explicit filename and optional
// content type (defaulted from the filename's extension when empty).
type FileInput struct {
	Data        []byte
	Filename    string
	ContentType string
}

func (f FileInput) resolve() (filePart, error) {
	contentType := f.ContentType
	if contentType == "" {
		contentType = mimeFor(f.Filename)
	}
	return filePart{data: f.Data, filename: f.Filename, contentType: contentType}, nil
}

// ------------------------------------------------------------------
// Internal helpers (mirror of the Node SDK's postJson / postFile / pollJob /
// download / raiseForStatus)
// ------------------------------------------------------------------

// request issues an authenticated HTTP request against the API base URL.
func (c *Client) request(ctx context.Context, method, requestPath, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+requestPath, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.httpClient.Do(req)
}

// doJSON issues an authenticated request and decodes a JSON object response,
// mapping HTTP errors via raiseForStatus.
func (c *Client) doJSON(ctx context.Context, method, requestPath, contentType string, body io.Reader) (map[string]any, error) {
	resp, err := c.request(ctx, method, requestPath, contentType, body)
	if err != nil {
		return nil, err
	}
	if err := raiseForStatus(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) getJSON(ctx context.Context, requestPath string) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodGet, requestPath, "", nil)
}

// postJSON POSTs body as JSON. When jobFallback is true, a client-generated
// job_id is added to the body and, if the request fails with a >=500
// APIError, the client falls back to polling that job instead of
// propagating the error.
func (c *Client) postJSON(ctx context.Context, endpoint string, body map[string]any, jobFallback bool) (map[string]any, error) {
	var jobID string
	if jobFallback {
		jobID = newJobID()
		body["job_id"] = jobID
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	data, err := c.doJSON(ctx, http.MethodPost, endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		var apiErr *APIError
		if jobFallback && errors.As(err, &apiErr) && apiErr.StatusCode >= 500 {
			return c.pollJob(ctx, jobID)
		}
		return nil, err
	}
	// Some success responses omit job_id (URL sync path); backfill the
	// client-generated id so callers can still poll GetJobStatus with it.
	if jobID != "" {
		if _, ok := data["job_id"]; !ok {
			data["job_id"] = jobID
		}
	}
	return data, nil
}

// postFile POSTs a multipart file upload. Always generates a job_id and
// falls back to polling that job on a >=500 APIError.
func (c *Client) postFile(ctx context.Context, endpoint string, part filePart, outputFilename string, pdfOptions *PDFOptions) (map[string]any, error) {
	jobID := newJobID()

	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(part.filename)))
	contentType := part.contentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	fw, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(part.data); err != nil {
		return nil, err
	}

	if err := writer.WriteField("direct_download", "false"); err != nil {
		return nil, err
	}
	if err := writer.WriteField("job_id", jobID); err != nil {
		return nil, err
	}
	if outputFilename != "" {
		if err := writer.WriteField("output_filename", outputFilename); err != nil {
			return nil, err
		}
	}
	if pdfOptions != nil {
		serialized, err := json.Marshal(serializePDFOptions(pdfOptions))
		if err != nil {
			return nil, err
		}
		if err := writer.WriteField("pdf_options", string(serialized)); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	data, err := c.doJSON(ctx, http.MethodPost, endpoint, writer.FormDataContentType(), buf)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 500 {
			return c.pollJob(ctx, jobID)
		}
		return nil, err
	}
	data["job_id"] = jobID
	return data, nil
}

// pollJob polls job status until success/failure. Used as a fallback when
// the initiating HTTP request fails with a server error.
func (c *Client) pollJob(ctx context.Context, jobID string) (map[string]any, error) {
	deadline := time.Now().Add(defaultPollMaxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(defaultPollInterval):
		}

		resp, err := c.request(ctx, http.MethodGet, "/v1/convert/status/"+jobID, "", nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			continue
		}
		if err := raiseForStatus(resp); err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		status := mapString(data, "status")
		if status == "success" {
			return data, nil
		}
		if status == "failed" {
			errMsg := mapString(data, "error")
			if errMsg == "" {
				errMsg = "Conversion failed"
			}
			return nil, NewAPIError(500, errMsg)
		}
	}
	return nil, NewAPIError(504, "Conversion timed out")
}

// download saves a presigned URL to a local file. The API key is
// deliberately NOT sent — presigned URLs are self-authenticating signed S3
// URLs, and forwarding the key to a third-party host would leak it.
func (c *Client) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NewAPIError(resp.StatusCode, fmt.Sprintf("Failed to download: %s", resp.Status))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// newJobID returns a UUIDv4 as a 32-character hex string (no dashes),
// matching the other Enconvert SDKs.
func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read failing is effectively unrecoverable on any
		// supported platform; fall back to a deterministic-but-unique
		// pattern rather than panicking, since job IDs need not be secret.
		for i := range b {
			b[i] = byte(i)
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return hex.EncodeToString(b[:])
}

// ------------------------------------------------------------------
// Request body builders
// ------------------------------------------------------------------

// buildURLBody is the request body shared by all single-URL conversions.
func buildURLBody(url string, opts URLRenderOptions) map[string]any {
	viewportWidth := 1920
	if opts.ViewportWidth != nil {
		viewportWidth = *opts.ViewportWidth
	}
	viewportHeight := 1080
	if opts.ViewportHeight != nil {
		viewportHeight = *opts.ViewportHeight
	}
	loadMedia := true
	if opts.LoadMedia != nil {
		loadMedia = *opts.LoadMedia
	}
	enableScroll := true
	if opts.EnableScroll != nil {
		enableScroll = *opts.EnableScroll
	}

	body := map[string]any{
		"url":             url,
		"direct_download": false,
		"viewport_width":  viewportWidth,
		"viewport_height": viewportHeight,
		"load_media":      loadMedia,
		"enable_scroll":   enableScroll,
	}
	if opts.OutputFilename != "" {
		body["output_filename"] = opts.OutputFilename
	}
	appendBrowserAccess(body, opts)
	return body
}

// buildWebsiteBody is the request body for website (whole-site)
// conversions. Render options are only sent when set — the gateway applies
// the same defaults per page.
func buildWebsiteBody(url string, opts WebsiteConversionOptions) map[string]any {
	body := map[string]any{"url": url}
	if opts.CrawlMode != "" {
		body["crawl_mode"] = opts.CrawlMode
	}
	if opts.IncludePatterns != nil {
		body["include_patterns"] = opts.IncludePatterns
	}
	if opts.ExcludePatterns != nil {
		body["exclude_patterns"] = opts.ExcludePatterns
	}
	if opts.NotificationEmail != "" {
		body["notification_email"] = opts.NotificationEmail
	}
	if opts.CallbackURL != "" {
		body["callback_url"] = opts.CallbackURL
	}
	if opts.OutputFilename != "" {
		body["output_filename"] = opts.OutputFilename
	}
	if opts.ViewportWidth != nil {
		body["viewport_width"] = *opts.ViewportWidth
	}
	if opts.ViewportHeight != nil {
		body["viewport_height"] = *opts.ViewportHeight
	}
	if opts.LoadMedia != nil {
		body["load_media"] = *opts.LoadMedia
	}
	if opts.EnableScroll != nil {
		body["enable_scroll"] = *opts.EnableScroll
	}
	appendBrowserAccess(body, opts.URLRenderOptions)
	return body
}

// appendBrowserAccess attaches the plan-gated auth/cookies/headers fields
// when provided.
func appendBrowserAccess(body map[string]any, opts URLRenderOptions) {
	if opts.Auth != nil {
		body["auth"] = opts.Auth
	}
	if opts.Cookies != nil {
		body["cookies"] = opts.Cookies
	}
	if opts.Headers != nil {
		body["headers"] = opts.Headers
	}
}

// serializePDFOptions converts PDFOptions into its snake_case wire form,
// including only fields explicitly set.
func serializePDFOptions(o *PDFOptions) map[string]any {
	out := map[string]any{}
	if o == nil {
		return out
	}
	if o.PageSize != "" {
		out["page_size"] = o.PageSize
	}
	if o.PageWidth != nil {
		out["page_width"] = *o.PageWidth
	}
	if o.PageHeight != nil {
		out["page_height"] = *o.PageHeight
	}
	if o.Orientation != "" {
		out["orientation"] = o.Orientation
	}
	if o.Margins != nil {
		out["margins"] = o.Margins
	}
	if o.Scale != nil {
		out["scale"] = *o.Scale
	}
	if o.Grayscale != nil {
		out["grayscale"] = *o.Grayscale
	}
	if o.Header != nil {
		out["header"] = o.Header
	}
	if o.Footer != nil {
		out["footer"] = o.Footer
	}
	return out
}

// ------------------------------------------------------------------
// Response mappers
// ------------------------------------------------------------------

func toConversionResult(data map[string]any) ConversionResult {
	objectKey := mapString(data, "object_key")
	filename := mapString(data, "filename")
	// Job-status fallback responses omit `filename`; recover it from the
	// object key (an S3-style key, always forward-slash separated) so
	// callers never see an empty name for a completed conversion.
	if filename == "" && objectKey != "" {
		filename = path.Base(objectKey)
	}
	return ConversionResult{
		PresignedURL:          mapString(data, "presigned_url"),
		ObjectKey:             objectKey,
		Filename:              filename,
		FileSize:              mapOptInt64(data, "file_size"),
		ConversionTimeSeconds: mapOptFloat64(data, "conversion_time_seconds"),
		JobID:                 mapString(data, "job_id"),
	}
}

func toJobStatus(data map[string]any) JobStatus {
	return JobStatus{
		Status:       JobStatusValue(mapString(data, "status")),
		PresignedURL: mapString(data, "presigned_url"),
		ObjectKey:    mapString(data, "object_key"),
		Error:        mapString(data, "error"),
	}
}

func toBatchSubmission(data map[string]any) BatchSubmission {
	status := mapString(data, "status")
	if status == "" {
		status = "processing"
	}
	return BatchSubmission{
		BatchID:         mapString(data, "batch_id"),
		Status:          status,
		URLCount:        mapInt(data, "url_count", 0),
		TotalDiscovered: mapOptInt(data, "total_discovered"),
		DiscoveryMethod: mapString(data, "discovery_method"),
		OutputFormat:    mapString(data, "output_format"),
	}
}

func toBatchStatus(data map[string]any) BatchStatus {
	items := []BatchItem{}
	if rawItems, ok := data["items"].([]any); ok {
		for _, ri := range rawItems {
			itemMap, ok := ri.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, BatchItem{
				SourceURL:      mapString(itemMap, "source_url"),
				Status:         mapString(itemMap, "status"),
				DownloadURL:    mapString(itemMap, "download_url"),
				OutputFileSize: mapOptInt64(itemMap, "output_file_size"),
				Duration:       mapString(itemMap, "duration"),
			})
		}
	}
	return BatchStatus{
		BatchID:        mapString(data, "batch_id"),
		Status:         BatchStatusValue(mapString(data, "status")),
		Total:          mapInt(data, "total", 0),
		Completed:      mapInt(data, "completed", 0),
		Failed:         mapInt(data, "failed", 0),
		InProgress:     mapInt(data, "in_progress", 0),
		OutputMode:     mapString(data, "output_mode"),
		ZipDownloadURL: mapString(data, "zip_download_url"),
		Items:          items,
	}
}

// ------------------------------------------------------------------
// raiseForStatus (error mapping) — shared by V1 and V2
// ------------------------------------------------------------------

func raiseForStatus(resp *http.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	message := extractErrorMessage(raw, resp.StatusCode)
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return newAuthenticationError(message)
	case resp.StatusCode == 402:
		return newQuotaError(message)
	case resp.StatusCode == 429:
		return newRateLimitError(message)
	default:
		return NewAPIError(resp.StatusCode, message)
	}
}

func extractErrorMessage(body []byte, status int) string {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if v, ok := parsed["detail"]; ok && v != nil {
			if s, ok := v.(string); ok {
				if s != "" {
					return s
				}
			} else if b, err := json.Marshal(v); err == nil {
				return string(b)
			}
		}
		if v, ok := parsed["error"]; ok && v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
		return string(body)
	}
	text := strings.TrimSpace(string(body))
	if text != "" {
		return text
	}
	return fmt.Sprintf("HTTP %d", status)
}

// ------------------------------------------------------------------
// map[string]any read helpers, shared with v2.go. Every accessor is
// guarded since the API omits unset fields (response_model_exclude_none).
// ------------------------------------------------------------------

func mapString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func mapBool(m map[string]any, key string) bool {
	v, ok := m[key]
	return ok && v == true
}

func mapFloat64(m map[string]any, key string, fallback float64) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return fallback
}

func mapInt(m map[string]any, key string, fallback int) int {
	return int(mapFloat64(m, key, float64(fallback)))
}

func mapOptFloat64(m map[string]any, key string) *float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return &f
		}
	}
	return nil
}

func mapOptInt64(m map[string]any, key string) *int64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			i := int64(f)
			return &i
		}
	}
	return nil
}

func mapOptInt(m map[string]any, key string) *int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			i := int(f)
			return &i
		}
	}
	return nil
}

func mapStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return []string{}
	}
	arr, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func mapObject(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return obj
}
