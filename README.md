# enconvert

Honest eyes for your AI agent — the Go SDK for [Enconvert](https://enconvert.com). Requires Go 1.21+. No third-party dependencies — built entirely on the standard library (`net/http`, `encoding/json`, `mime/multipart`).

Read any web page or file into clean Markdown, JSON, or screenshots, and get a `RenderQuality` score (0.0–1.0) on **every** read — so a blocked, challenge, or empty-SPA page comes back flagged with a low score and warnings, never mistaken for real content. Perceive, discover, look up, distill, ingest, and watch the web; convert 43 file and document formats through the same key.

> Wiring an agent (Claude, Cursor, Windsurf, n8n, …)? The [MCP server](https://enconvert.com/mcp) is the native path — `npx @enconvert/mcp setup`. This SDK is the programmatic REST path for everything else.

## Install

```bash
go get github.com/conversionapi/go-sdk
```

## Quick Start

```go
import (
	"context"
	"fmt"

	enconvert "github.com/conversionapi/go-sdk"
)

client, err := enconvert.New("sk_...")
if err != nil {
	log.Fatal(err)
}

// Read a page the way your agent should — with a quality score attached.
op, err := client.V2.Perceive(ctx, "https://example.com", enconvert.PerceiveOptions{
	Outputs: []enconvert.PerceiveOutputName{enconvert.PerceiveOutputMarkdown, enconvert.PerceiveOutputStructured},
})
fmt.Println(op.Outputs["markdown"].URL, *op.RenderQuality) // e.g. 0.93
```

`New` accepts functional options to override defaults:

```go
client, err := enconvert.New("sk_...",
	enconvert.WithTimeout(60*time.Second),
	enconvert.WithBaseURL("https://api.enconvert.com"),
)
```

Every method that talks to the API takes a `context.Context` as its first argument, so callers control cancellation and deadlines on top of the client's own timeout.

---

# V2 — agent-ready data (`client.V2`)

The V2 namespace turns web pages into agent-ready data: render, search, extract, ingest, and monitor. All V2 endpoints require a **private API key** and are plan-gated — a disabled feature or exhausted monthly quota returns an error where `IsQuotaError(err)` is true (HTTP 402).

Every render carries `RenderQuality` (0.0–1.0). A low score means the page didn't render cleanly (challenge page, cookie wall, empty shell); the content is still returned, flagged, so a bad read never quietly enters your agent's context.

### Perceive — render a URL into artifacts

```go
op, err := client.V2.Perceive(ctx, "https://example.com", enconvert.PerceiveOptions{
	Outputs: []enconvert.PerceiveOutputName{
		enconvert.PerceiveOutputMarkdown,
		enconvert.PerceiveOutputScreenshot,
		enconvert.PerceiveOutputStructured,
	},
	Extract: []enconvert.PerceiveExtractName{enconvert.PerceiveExtractTables, enconvert.PerceiveExtractMetadata},
})
fmt.Println(*op.RenderQuality)           // honesty score, 0.0–1.0
fmt.Println(op.Outputs["markdown"].URL)  // 15-min signed URL
fmt.Println(op.Structured)

// Re-sign artifact URLs later:
again, err := client.V2.GetPerceiveOperation(ctx, op.OperationID)

// Batch (<=1000 URLs; small batches run inline, larger return "queued" — poll):
batch, err := client.V2.PerceiveBatch(ctx, []string{"https://a.com", "https://b.com"}, enconvert.PerceiveBatchOptions{
	PerceiveOptions: enconvert.PerceiveOptions{Outputs: []enconvert.PerceiveOutputName{enconvert.PerceiveOutputMarkdown}},
	OutputMode:      enconvert.PerceiveBatchOutputZip,
})
done, err := client.V2.GetPerceiveBatch(ctx, batch.JobID)

// Direct download — stream one artifact's raw bytes instead of the JSON envelope
// (exactly one artifact-producing output; metadata arrives via headers):
direct, err := client.V2.PerceiveDirect(ctx, "https://example.com", enconvert.PerceiveOptions{
	Outputs: []enconvert.PerceiveOutputName{enconvert.PerceiveOutputPDF},
})
os.WriteFile(direct.Filename, direct.Content, 0o644)

// Re-download a stored artifact later (output "" when the operation has only one):
saved, err := client.V2.DownloadPerceiveArtifact(ctx, direct.OperationID, enconvert.PerceiveOutputPDF)
```

### Discover — enumerate a site's URLs (no rendering)

```go
found, err := client.V2.Discover(ctx, "https://example.com", enconvert.DiscoverOptions{
	Mode:            enconvert.DiscoverModeHybrid, // Sitemap | Crawl | Hybrid
	MaxURLs:         enconvert.Int(200),
	ExcludePatterns: []string{"/tag/"},
})
fmt.Println(found.Total, found.URLs)
```

### Lookup — web search with optional auto-perceive

```go
search, err := client.V2.Lookup(ctx, "best static site generators", enconvert.LookupOptions{
	Category:    enconvert.LookupCategoryWeb, // web | news | images | scholar | patents | maps
	NumResults:  enconvert.Int(10),
	PerceiveTop: enconvert.Int(3), // auto-render top 3 results (uses perceive quota)
})
for _, hit := range search.Results {
	fmt.Println(hit.Title, hit.URL)
	if hit.Perceive != nil {
		fmt.Println(hit.Perceive.RenderQuality)
	}
}
```

### Distill — schema-driven structured extraction

```go
extraction, err := client.V2.Distill(ctx, enconvert.DistillOptions{
	URLs:   []string{"https://example.com/pricing"},
	Schema: map[string]any{"plans": "list of plan names with monthly prices"},
	CSSSchema: &enconvert.CSSSchema{ // optional free CSS pass before the LLM tier
		BaseSelector: ".plan-card",
		Fields: []enconvert.CSSField{
			{Name: "name", Type: enconvert.CSSFieldText, Selector: "h3"},
			{Name: "price", Type: enconvert.CSSFieldText, Selector: ".price"},
		},
	},
})
fmt.Println(extraction.Results[0].Data, extraction.Results[0].ExtractionTier)

// Or discover-then-distill:
client.V2.Distill(ctx, enconvert.DistillOptions{
	DiscoverFrom: &enconvert.DistillDiscoverFrom{URL: "https://example.com", Mode: enconvert.DiscoverModeSitemap, MaxPages: enconvert.Int(10)},
	Schema:       map[string]any{"title": "page title", "summary": "one-line summary"},
})
```

### Ingest — site or files to RAG-ready JSONL (always async)

Turn a whole site — or a set of uploaded documents — into chunked, RAG-ready JSONL through one pipeline.

```go
// From a site:
job, err := client.V2.Ingest(ctx, enconvert.IngestOptions{
	Mode:       enconvert.IngestModeSitemap,
	URL:        "https://docs.example.com",
	MaxPages:   enconvert.Int(100),
	Chunk:      &enconvert.IngestChunkOptions{MaxWords: enconvert.Int(512), SentenceOverlap: enconvert.Int(1)},
	WebhookURL: "https://my.app/hooks/enconvert",
})

// Or from uploaded files (PDF, DOCX, PPTX, XLSX, CSV, HTML, EPUB, TXT/MD, legacy/ODF office):
fileJob, err := client.V2.IngestFiles(ctx, []enconvert.FileSource{
	enconvert.FilePath("handbook.pdf"),
	enconvert.FilePath("notes.docx"),
}, enconvert.IngestFilesOptions{
	Chunk: &enconvert.IngestChunkOptions{MaxWords: enconvert.Int(512), SentenceOverlap: enconvert.Int(1)},
})

status, err := client.V2.GetIngestJob(ctx, job.JobID) // poll
if status.Status == enconvert.IngestStatusCompleted {
	fmt.Println(status.OutputURL) // JSONL
}

client.V2.ListIngestJobs(ctx, enconvert.V2ListOptions{Limit: enconvert.Int(20)})
client.V2.CancelIngestJob(ctx, job.JobID) // idempotent

// Webhook signing (HMAC):
secret, err := client.V2.GetWebhookSecret(ctx)
fmt.Println(secret.Secret, secret.SignatureHeader)
client.V2.RotateWebhookSecret(ctx)          // invalidates old secret
client.V2.RetryIngestWebhook(ctx, job.JobID) // re-deliver
```

### Watch — recurring change monitoring

```go
watcher, err := client.V2.CreateWatcher(ctx, "https://example.com/pricing", enconvert.WatchCreateOptions{
	FrequencyMinutes: enconvert.Int(60),       // hourly floor
	DiffMode:         enconvert.WatchDiffAuto, // auto | text | structured | tables | metadata
	WebhookURL:       "https://my.app/hooks/changes",
	NotifyEmail:      enconvert.Bool(true),
})

client.V2.ListWatchers(ctx, enconvert.V2ListOptions{})
client.V2.GetWatcher(ctx, watcher.WatcherID)
client.V2.GetWatcherSnapshots(ctx, watcher.WatcherID, enconvert.SnapshotListOptions{Limit: enconvert.Int(10)})
client.V2.UpdateWatcher(ctx, watcher.WatcherID, enconvert.WatcherUpdate{Status: enconvert.WatcherStatusPaused})
client.V2.UpdateWatcher(ctx, watcher.WatcherID, enconvert.WatcherUpdate{WebhookURL: enconvert.String("")}) // clears webhook
client.V2.DeleteWatcher(ctx, watcher.WatcherID) // soft-delete, idempotent
```

### V2 error handling

```go
_, err := client.V2.Ingest(ctx, enconvert.IngestOptions{URLs: []string{"https://example.com"}})
if err != nil {
	if enconvert.IsQuotaError(err) {
		log.Println("upgrade plan or wait for quota reset")
	} else {
		log.Fatal(err)
	}
}
```

---

# File conversion

The same key also converts 43+ formats. Two "anything → X" endpoints auto-detect the input; the format-specific methods below give you a validated, typed path.

### Anything to Markdown / PDF

```go
// Any document → clean Markdown (a RAG-ingestion building block):
client.ConvertToMarkdown(ctx, enconvert.FilePath("report.docx"), enconvert.ConvertToMarkdownOptions{SaveTo: "report.md"})
// PDF, DOCX, PPTX, XLSX, CSV, HTML, EPUB, TXT/MD, and legacy/ODF office. (Images not supported.)

// Almost anything → PDF:
client.ConvertToPDF(ctx, enconvert.FilePath("slides.pptx"), enconvert.ConvertToPDFOptions{SaveTo: "slides.pdf"})
// office/ODF/Pages/Numbers/RTF/CSV, HTML, Markdown, text, images, SVG, EPUB, or a PDF passthrough.
// Only PDFOptions.Grayscale is honored on this endpoint:
client.ConvertToPDF(ctx, enconvert.FilePath("scan.pdf"), enconvert.ConvertToPDFOptions{
	PDFOptions: &enconvert.PDFOptions{Grayscale: enconvert.Bool(true)},
	SaveTo:     "gray.pdf",
})
```

### Image Conversion

`ConvertImage` and `ConvertDocument` accept any `FileSource`: a `FilePath` (filesystem path), `FileBytes` (raw bytes, uploaded as `upload.bin`), or `FileInput` (raw bytes with an explicit filename/content type).

```go
result, err := client.ConvertImage(ctx, enconvert.FilePath("photo.heic"), enconvert.ConvertImageOptions{
	OutputFormat: "webp",
	SaveTo:       "photo.webp",
})
```

Any pair among `jpeg`, `png`, `svg`, `heic`, `webp` — plus PDF rasterization (`pdf` → `jpeg`). Unsupported pairs return an error before any request is made:

```go
enconvert.ValidOutputsFor("json") // ["csv", "toml", "xml", "yaml"]
enconvert.ValidOutputsFor("pdf")  // ["jpeg"]
```

### Document Conversion

```go
client.ConvertDocument(ctx, enconvert.FilePath("report.docx"), enconvert.ConvertDocumentOptions{SaveTo: "report.pdf"})
client.ConvertDocument(ctx, enconvert.FilePath("data.json"), enconvert.ConvertDocumentOptions{OutputFormat: "yaml", SaveTo: "data.yaml"})
client.ConvertDocument(ctx, enconvert.FilePath("notes.md"), enconvert.ConvertDocumentOptions{OutputFormat: "html", SaveTo: "notes.html"})
```

Supported inputs: `doc`/`docx`, `xls`/`xlsx`, `ppt`/`pptx`, `odt`, `ods`, `odp`, `ots`, `pages`, `numbers`, `html`, `markdown`, `csv`, `json`, `xml`, `yaml`, `toml`. (EPUB → use `ConvertToPDF` / `ConvertToMarkdown`.)

| Input | Outputs |
|-------|---------|
| json | csv, toml, xml, yaml |
| xml | csv, json |
| yaml | json |
| csv | json, xml |
| toml | json |
| markdown | html, pdf |
| html | pdf |
| doc, excel, ppt, odt, ods, odp, ots, pages, numbers | pdf |
| jpeg, png, svg, heic, webp | each other (all 20 pairs) |
| pdf | jpeg |

### URL to PDF / Screenshot / Markdown

```go
client.ConvertURLToPDF(ctx, "https://example.com", enconvert.URLToPDFOptions{SaveTo: "page.pdf"})
client.ConvertURLToScreenshot(ctx, "https://example.com", enconvert.URLToScreenshotOptions{SaveTo: "shot.png"})
client.ConvertURLToMarkdown(ctx, "https://example.com/article", enconvert.URLToMarkdownOptions{SaveTo: "article.md"})
```

### Website to PDF / Screenshot (whole-site batch)

Discover every page of a website (sitemap, or full crawl on higher plans), convert each in the background, and receive a single ZIP. Requires a private API key with crawl access.

```go
batch, err := client.ConvertWebsiteToPDF(ctx, "https://example.com", enconvert.WebsiteToPDFOptions{
	WebsiteConversionOptions: enconvert.WebsiteConversionOptions{CrawlMode: enconvert.CrawlModeSitemap},
})
status, err := client.WaitForBatch(ctx, batch.BatchID, enconvert.WaitForBatchOptions{SaveTo: "site.zip"})
fmt.Println(status.Completed, "of", status.Total, "pages converted")
```

`ConvertWebsiteToScreenshot` works the same way and produces a ZIP of PNGs.

### PDF Options & Authenticated Pages

```go
client.ConvertURLToPDF(ctx, "https://internal.example.com/report", enconvert.URLToPDFOptions{
	PDFOptions: &enconvert.PDFOptions{
		PageSize:    "A4",
		Orientation: "landscape",
		Margins:     &enconvert.PDFMargins{Top: enconvert.Float64(10), Bottom: enconvert.Float64(10)},
	},
	URLRenderOptions: enconvert.URLRenderOptions{
		Auth: &enconvert.HTTPBasicAuth{Username: "user", Password: "pass"}, // or Cookies / Headers, plan-gated
	},
	SaveTo: "report.pdf",
})
```

Do not combine `Auth` with an `Authorization` header — the API rejects the conflict.

### Job Status (async polling)

```go
status, err := client.GetJobStatus(ctx, "job_abc123")
if status.Status == enconvert.JobStatusSuccess {
	fmt.Println(status.PresignedURL)
}
```

---

## Error Handling

Failed requests return `*enconvert.APIError`, whose `Error()` string is `"[<status>] <message>"`. Use the `IsXxxError` helpers to check for the conditions the API signals with dedicated status codes:

```go
result, err := client.ConvertURLToPDF(ctx, "https://example.com", enconvert.URLToPDFOptions{})
if err != nil {
	switch {
	case enconvert.IsAuthenticationError(err):
		log.Println("invalid API key")
	case enconvert.IsRateLimitError(err):
		log.Println("too many requests — slow down")
	case enconvert.IsQuotaError(err):
		log.Println("plan feature not enabled or quota exhausted")
	default:
		var apiErr *enconvert.APIError
		if errors.As(err, &apiErr) {
			log.Printf("API error [%d]: %s", apiErr.StatusCode, apiErr.Message)
		} else {
			log.Println(err) // network error, context cancellation, etc.
		}
	}
}
```

## Configuration

```go
client, err := enconvert.New("sk_...",
	enconvert.WithTimeout(300*time.Second),             // default
	enconvert.WithBaseURL("https://api.enconvert.com"), // default
)
```

## Get an API Key

Sign up at [enconvert.com](https://enconvert.com). Free tier: 100 ops/month, no credit card.

## License

MIT
