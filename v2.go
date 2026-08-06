package enconvert

// V2 API namespace, reached as client.V2.
//
// One method per V2 endpoint (20 total across six groups: perceive,
// discover, lookup, distill, ingest, watch). Options are idiomatic Go
// structs and are serialized to the API's snake_case wire format;
// responses are mapped back onto Go structs. User-data payloads (schemas,
// extracted data, tracked fields, diff changes) pass through untouched as
// map[string]any.
//
// All V2 endpoints require a private API key (public keys are rejected)
// and are plan-gated: a disabled feature or exhausted monthly quota
// returns a QuotaError (HTTP 402, see IsQuotaError).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
)

// V2 is the Enconvert V2 API namespace. Obtain it via Client.V2.
type V2 struct {
	client *Client
}

func newV2(c *Client) *V2 {
	return &V2{client: c}
}

// ---------------------------------------------------------------------------
// Perceive
// ---------------------------------------------------------------------------

// PerceiveOutputName is an artifact perceive/perceiveBatch can produce.
type PerceiveOutputName string

const (
	PerceiveOutputMarkdown           PerceiveOutputName = "markdown"
	PerceiveOutputHTMLCleaned        PerceiveOutputName = "html_cleaned"
	PerceiveOutputHTMLRaw            PerceiveOutputName = "html_raw"
	PerceiveOutputScreenshot         PerceiveOutputName = "screenshot"
	PerceiveOutputScreenshotFullPage PerceiveOutputName = "screenshot_full_page"
	PerceiveOutputPDF                PerceiveOutputName = "pdf"
	PerceiveOutputLinks              PerceiveOutputName = "links"
	PerceiveOutputImages             PerceiveOutputName = "images"
	PerceiveOutputStructured         PerceiveOutputName = "structured"
)

// perceiveArtifactOutputs are the artifact-producing outputs accepted by
// PerceiveDirect ("structured" is inline-only and produces no artifact).
var perceiveArtifactOutputs = []PerceiveOutputName{
	PerceiveOutputMarkdown,
	PerceiveOutputHTMLCleaned,
	PerceiveOutputHTMLRaw,
	PerceiveOutputScreenshot,
	PerceiveOutputScreenshotFullPage,
	PerceiveOutputPDF,
	PerceiveOutputLinks,
	PerceiveOutputImages,
}

// PerceiveExtractName is a heuristic extraction target.
type PerceiveExtractName string

const (
	PerceiveExtractTables         PerceiveExtractName = "tables"
	PerceiveExtractPrices         PerceiveExtractName = "prices"
	PerceiveExtractContacts       PerceiveExtractName = "contacts"
	PerceiveExtractMetadata       PerceiveExtractName = "metadata"
	PerceiveExtractMainContent    PerceiveExtractName = "main_content"
	PerceiveExtractHeadings       PerceiveExtractName = "headings"
	PerceiveExtractStructuredData PerceiveExtractName = "structured_data"
	PerceiveExtractTechnologies   PerceiveExtractName = "technologies"
	PerceiveExtractAll            PerceiveExtractName = "all"
)

// PerceiveResourceType is a browser resource type that can be blocked.
type PerceiveResourceType string

const (
	PerceiveResourceImage      PerceiveResourceType = "image"
	PerceiveResourceMedia      PerceiveResourceType = "media"
	PerceiveResourceFont       PerceiveResourceType = "font"
	PerceiveResourceStylesheet PerceiveResourceType = "stylesheet"
	PerceiveResourceScript     PerceiveResourceType = "script"
	PerceiveResourceXHR        PerceiveResourceType = "xhr"
	PerceiveResourceFetch      PerceiveResourceType = "fetch"
	PerceiveResourceWebSocket  PerceiveResourceType = "websocket"
	PerceiveResourceManifest   PerceiveResourceType = "manifest"
	PerceiveResourceOther      PerceiveResourceType = "other"
)

// PerceiveCacheMode controls perceive's render cache.
type PerceiveCacheMode string

const (
	PerceiveCacheEnabled PerceiveCacheMode = "enabled"
	PerceiveCacheBypass  PerceiveCacheMode = "bypass"
	PerceiveCacheRefresh PerceiveCacheMode = "refresh"
)

// PerceiveStatus is the lifecycle state of a perceive operation.
type PerceiveStatus string

const (
	PerceiveStatusQueued     PerceiveStatus = "queued"
	PerceiveStatusProcessing PerceiveStatus = "processing"
	PerceiveStatusCompleted  PerceiveStatus = "completed"
	PerceiveStatusFailed     PerceiveStatus = "failed"
)

// PerceiveExtractionTier records which tier answered structured extraction.
type PerceiveExtractionTier string

const (
	PerceiveExtractionHeuristic PerceiveExtractionTier = "heuristic"
	PerceiveExtractionCSS       PerceiveExtractionTier = "css"
	PerceiveExtractionLLM       PerceiveExtractionTier = "llm"
)

// PerceiveViewport sets the browser viewport for rendering.
type PerceiveViewport struct {
	// Width is 320-3840, default 1920.
	Width *int `json:"width,omitempty"`
	// Height is 240-2160, default 1080.
	Height *int `json:"height,omitempty"`
}

// PerceiveOptions are the per-render options shared by Perceive and
// PerceiveBatch.
type PerceiveOptions struct {
	// Outputs are the artifacts to produce. Default: [markdown, structured].
	Outputs []PerceiveOutputName
	// Extract lists heuristic extraction targets. Unsupported members yield
	// warnings.
	Extract []PerceiveExtractName
	// Schema is a JSON schema for structured extraction (LLM tier,
	// plan-gated).
	Schema map[string]any
	// WaitFor is a CSS selector (optionally "css:...") or "js:<expr>" to
	// await.
	WaitFor string
	// WaitTimeoutMs is 0-60000, default 30000.
	WaitTimeoutMs *int
	// JSCode is JavaScript executed after navigation. Max 20000 chars.
	JSCode   string
	Viewport *PerceiveViewport
	Headers  map[string]string
	Cookies  []BrowserCookie
	// Auth is HTTP Basic Auth (plan-gated).
	Auth *HTTPBasicAuth
	// ProxyURL is not yet available server-side — currently rejected with
	// 422.
	ProxyURL string
	// Geolocation is not yet available server-side — currently rejected
	// with 422.
	Geolocation map[string]any
	// ActionChain is not yet available server-side — currently rejected
	// with 422.
	ActionChain []map[string]any
	// CacheMode defaults to "enabled" (1h cache). "bypass" skips, "refresh"
	// re-renders.
	CacheMode PerceiveCacheMode
	// PDFOptions is only meaningful when Outputs includes "pdf".
	PDFOptions *PDFOptions
	// BlockResources lists resource types the browser should not load.
	BlockResources []PerceiveResourceType
	RespectRobots  *bool
	Mobile         *bool
	// OnlyMainContent strips site chrome (nav, header, footer, cookie
	// banners) from the markdown artifact and main_content extract.
	// Defaults to true server-side; set to false for the full page.
	OnlyMainContent *bool
	// DirectDownload makes the HTTP response body the artifact bytes
	// (Perceive only — PerceiveBatch rejects it with 422). Requires exactly
	// one artifact-producing output.
	DirectDownload *bool
}

// PerceiveBatchOutputMode controls how PerceiveBatch packages results.
type PerceiveBatchOutputMode string

const (
	PerceiveBatchOutputManifest PerceiveBatchOutputMode = "manifest"
	PerceiveBatchOutputZip      PerceiveBatchOutputMode = "zip"
)

// PerceiveBatchOptions configures PerceiveBatch: shared render options plus
// the output mode.
type PerceiveBatchOptions struct {
	PerceiveOptions
	// OutputMode defaults to "manifest"; "zip" bundles all artifacts once
	// complete.
	OutputMode PerceiveBatchOutputMode
}

// V2OutputArtifact is a rendered output stored server-side, addressed by
// signed URL.
type V2OutputArtifact struct {
	// URL is a pre-signed download URL (15 minutes). Re-signed on every
	// status GET.
	URL         string
	ObjectKey   string
	SizeBytes   int64
	ContentType string
	ExpiresIn   int
}

// V2Tokens is the LLM token usage for an operation.
type V2Tokens struct {
	Input  int
	Output int
}

// PerceiveResult is the outcome of Perceive or one item of PerceiveBatch.
type PerceiveResult struct {
	OperationID string
	Status      PerceiveStatus
	URL         string
	URLFinal    string
	ContentHash string
	// RenderQuality is a 0.0-1.0 render quality score.
	RenderQuality *float64
	// StatusCode is the HTTP status of the final main-document response.
	StatusCode *int
	// Deductions maps named render-quality deductions that fired to their
	// weight, e.g. {"http_error": 0.7}. Empty on a clean render.
	Deductions map[string]float64
	CacheHit   bool
	// Outputs is keyed by output name (e.g. "markdown",
	// "screenshot_full_page").
	Outputs map[string]V2OutputArtifact
	// Structured is present when extract/schema was requested. Shape is
	// caller-defined.
	Structured     map[string]any
	ExtractionTier PerceiveExtractionTier
	Tokens         V2Tokens
	CostCents      int
	DurationMs     *int
	Error          string
	Warnings       []string
	// OptionsEcho echoes the request options the server honoured (secrets
	// redacted to booleans). Nil when the server omits it.
	OptionsEcho map[string]any
}

// PerceiveDirectResult is the outcome of PerceiveDirect and
// DownloadPerceiveArtifact: the raw artifact bytes plus the metadata the
// server carries in response headers.
type PerceiveDirectResult struct {
	// Content is the raw artifact bytes.
	Content []byte
	// ContentType is the artifact media type, e.g.
	// "text/markdown; charset=utf-8".
	ContentType string
	// Filename is parsed from the Content-Disposition header. Empty when
	// the header is absent.
	Filename    string
	OperationID string
	ObjectKey   string
	CacheHit    bool
	// RenderQuality is a 0.0-1.0 render quality score; nil when the header
	// is absent.
	RenderQuality *float64
	// SourceStatusCode is the HTTP status of the upstream main-document
	// response; nil when the header is absent.
	SourceStatusCode *int
	// ContentHash is the artifact sha256; empty when the header is absent.
	ContentHash string
	// WarningsCount is the operation's warning count (0 when the header is
	// absent).
	WarningsCount int
}

// PerceiveBatchStatus is the lifecycle state of a perceive batch job.
type PerceiveBatchStatus string

const (
	PerceiveBatchStatusQueued     PerceiveBatchStatus = "queued"
	PerceiveBatchStatusProcessing PerceiveBatchStatus = "processing"
	PerceiveBatchStatusCompleted  PerceiveBatchStatus = "completed"
	PerceiveBatchStatusFailed     PerceiveBatchStatus = "failed"
	PerceiveBatchStatusPartial    PerceiveBatchStatus = "partial"
)

// PerceiveBatchResult is the outcome of PerceiveBatch / GetPerceiveBatch.
type PerceiveBatchResult struct {
	JobID      string
	Status     PerceiveBatchStatus
	OutputMode PerceiveBatchOutputMode
	Total      int
	Completed  int
	Failed     int
	Pending    int
	// Zip is the bundle of every successful artifact (OutputMode "zip",
	// once done).
	Zip *V2OutputArtifact
	// Items has one entry per URL. Empty on the initial 202 — poll
	// GetPerceiveBatch.
	Items    []PerceiveResult
	Warnings []string
}

// ---------------------------------------------------------------------------
// Discover
// ---------------------------------------------------------------------------

// DiscoverMode is a URL discovery strategy.
type DiscoverMode string

const (
	DiscoverModeSitemap DiscoverMode = "sitemap"
	DiscoverModeCrawl   DiscoverMode = "crawl"
	DiscoverModeHybrid  DiscoverMode = "hybrid"
)

// DiscoverOptions configures Discover.
type DiscoverOptions struct {
	// Mode defaults to "hybrid" (sitemap + HTTP crawl).
	Mode DiscoverMode
	// MaxURLs is 1-1000, default 100.
	MaxURLs *int
	// MaxDepth is 1-5, default 2.
	MaxDepth *int
	// IncludePatterns is a regex allowlist (re.search semantics), max 50.
	IncludePatterns []string
	// ExcludePatterns is a regex denylist, applied after IncludePatterns,
	// max 50.
	ExcludePatterns []string
	// SameDomainOnly defaults to true.
	SameDomainOnly *bool
	RespectRobots  *bool
}

// DiscoverResult is the outcome of Discover.
type DiscoverResult struct {
	URL          string
	Mode         DiscoverMode
	Total        int
	URLs         []string
	PagesCrawled int
	// Truncated is true when more URLs were found than MaxURLs allowed.
	Truncated       bool
	RobotsRespected bool
	// Sources holds raw counts per source before dedup, e.g.
	// {"sitemap": 42, "crawl": 30}.
	Sources  map[string]int
	Warnings []string
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

// LookupCategory selects the search vertical.
type LookupCategory string

const (
	LookupCategoryWeb     LookupCategory = "web"
	LookupCategoryNews    LookupCategory = "news"
	LookupCategoryImages  LookupCategory = "images"
	LookupCategoryScholar LookupCategory = "scholar"
	LookupCategoryPatents LookupCategory = "patents"
	LookupCategoryMaps    LookupCategory = "maps"
)

// LookupTimeFilter restricts results by recency.
type LookupTimeFilter string

const (
	LookupTimeHour  LookupTimeFilter = "hour"
	LookupTimeDay   LookupTimeFilter = "day"
	LookupTimeWeek  LookupTimeFilter = "week"
	LookupTimeMonth LookupTimeFilter = "month"
	LookupTimeYear  LookupTimeFilter = "year"
)

// LookupOptions configures Lookup.
type LookupOptions struct {
	// Category defaults to "web".
	Category LookupCategory
	// Country is a Google "gl" country code, e.g. "us", "in".
	Country string
	// Locale is a Google "hl" interface language, e.g. "en".
	Locale     string
	TimeFilter LookupTimeFilter
	// NumResults is 1-100, default 10.
	NumResults *int
	// Page is 1-10, default 1.
	Page *int
	// Location is a free-text location, e.g. "Austin, Texas".
	Location string
	// Autocorrect defaults to true.
	Autocorrect *bool
	// PerceiveTop auto-perceives the top-N result URLs (0-10, default 0).
	// Each consumes one perceive-quota unit and runs a full browser render.
	PerceiveTop *int
}

// LookupItem is one search hit, optionally carrying its full perceive
// result.
type LookupItem struct {
	Title        string
	URL          string
	Snippet      string
	Position     *int
	Source       string
	Date         string
	ImageURL     string
	ThumbnailURL string
	// Extra holds provider-specific passthrough fields.
	Extra map[string]any
	// Perceive is present for the top-N results when PerceiveTop > 0 and it
	// succeeded.
	Perceive *PerceiveResult
}

// LookupResult is the outcome of Lookup.
type LookupResult struct {
	// LookupID is the audit row id; nil when the audit write failed
	// (results are still valid).
	LookupID   *int
	Query      string
	Category   LookupCategory
	Country    string
	Locale     string
	TimeFilter LookupTimeFilter
	Total      int
	Results    []LookupItem
	// PerceiveTop is how many results were actually perceived (may be below
	// requested).
	PerceiveTop          int
	PerceiveOperationIDs []string
	AnswerBox            map[string]any
	KnowledgeGraph       map[string]any
	// Credits is search-provider credits consumed.
	Credits   *int
	CostCents int
	Warnings  []string
}

// ---------------------------------------------------------------------------
// Distill
// ---------------------------------------------------------------------------

// CSSFieldType is the extraction kind for one CSSField.
type CSSFieldType string

const (
	CSSFieldText       CSSFieldType = "text"
	CSSFieldAttribute  CSSFieldType = "attribute"
	CSSFieldHTML       CSSFieldType = "html"
	CSSFieldRegex      CSSFieldType = "regex"
	CSSFieldNested     CSSFieldType = "nested"
	CSSFieldList       CSSFieldType = "list"
	CSSFieldNestedList CSSFieldType = "nested_list"
)

// CSSField is one field of a CSS extraction schema (recursive for nested
// types).
type CSSField struct {
	Name     string
	Type     CSSFieldType
	Selector string
	// Attribute is required when Type is CSSFieldAttribute.
	Attribute string
	// Pattern is required when Type is CSSFieldRegex. Compiled
	// server-side; ReDoS-screened.
	Pattern string
	Default any
	// Transform is "lowercase", "uppercase", or "strip".
	Transform string
	// Fields is required (non-empty) for "nested" / "list" / "nested_list".
	// Max depth 5.
	Fields []CSSField
}

// CSSSchema is a free CSS extraction pass run before any LLM escalation.
type CSSSchema struct {
	// BaseSelector matches the repeating container; one extracted record
	// per match.
	BaseSelector string
	Fields       []CSSField
	Name         string
	// TargetField is the top-level output-schema property the CSS records
	// fill. An array property receives the full list; scalar/object gets
	// the first record. Inferred when empty and the schema has exactly one
	// array property.
	TargetField string
}

// DistillDiscoverFrom discovers a site's URLs before distilling them.
type DistillDiscoverFrom struct {
	URL string
	// Mode defaults to "hybrid".
	Mode DiscoverMode
	// MaxPages is 1-50, default 10. Cap on URLs discovered AND distilled.
	MaxPages *int
}

// DistillOptions configures Distill. Exactly one of URLs or DiscoverFrom
// must be set.
type DistillOptions struct {
	// URLs are explicit URLs to distill (max 50).
	URLs []string
	// DiscoverFrom discovers a site's URLs first, then distills each.
	DiscoverFrom *DistillDiscoverFrom
	// Schema is the required output shape: a JSON-Schema object
	// ({type: "object", properties: {...}}) or a flat {field: description}
	// map. The response data matches this shape.
	Schema map[string]any
	// CSSSchema is an optional free CSS pass; missing fields escalate to
	// the LLM tier.
	CSSSchema *CSSSchema
	WaitFor   string
	// WaitTimeoutMs is 0-60000, default 30000.
	WaitTimeoutMs *int
	Headers       map[string]string
	Cookies       []BrowserCookie
	RespectRobots *bool
}

// DistillExtractionTier records which tier(s) answered a distill item.
type DistillExtractionTier string

const (
	DistillExtractionCSS   DistillExtractionTier = "css"
	DistillExtractionLLM   DistillExtractionTier = "llm"
	DistillExtractionMixed DistillExtractionTier = "mixed"
	DistillExtractionNone  DistillExtractionTier = "none"
)

// DistillItemStatus is the per-URL outcome of a distill operation.
type DistillItemStatus string

const (
	DistillItemCompleted DistillItemStatus = "completed"
	DistillItemFailed    DistillItemStatus = "failed"
)

// DistillItem is one URL's result within DistillResult.
type DistillItem struct {
	URL      string
	URLFinal string
	Status   DistillItemStatus
	// Data is the extracted data matching the requested schema.
	Data           map[string]any
	ExtractionTier DistillExtractionTier
	FieldsFromCSS  int
	FieldsFromLLM  int
	RenderQuality  *float64
	Tokens         V2Tokens
	CostCents      int
	Error          string
	Warnings       []string
}

// DistillResult is the outcome of Distill.
type DistillResult struct {
	OperationID    string
	Total          int
	Completed      int
	Failed         int
	Results        []DistillItem
	TotalCostCents int
	Warnings       []string
}

// ---------------------------------------------------------------------------
// Ingest
// ---------------------------------------------------------------------------

// IngestMode selects how Ingest discovers pages.
type IngestMode string

const (
	IngestModeURLs    IngestMode = "urls"
	IngestModeSitemap IngestMode = "sitemap"
	IngestModeCrawl   IngestMode = "crawl"
	IngestModeFiles   IngestMode = "files"
)

// IngestStatus is the lifecycle state of an ingest job.
type IngestStatus string

const (
	IngestStatusQueued      IngestStatus = "queued"
	IngestStatusDiscovering IngestStatus = "discovering"
	IngestStatusProcessing  IngestStatus = "processing"
	IngestStatusCompleted   IngestStatus = "completed"
	IngestStatusFailed      IngestStatus = "failed"
	IngestStatusCanceled    IngestStatus = "canceled"
)

// IngestChunkOptions configures how ingested text is split into chunks.
type IngestChunkOptions struct {
	// MaxWords is 32-4000, default 512.
	MaxWords *int
	// SentenceOverlap is sentences repeated between consecutive chunks,
	// 0-10, default 1.
	SentenceOverlap *int
}

// IngestOptions configures Ingest.
type IngestOptions struct {
	// Mode defaults to "urls".
	Mode IngestMode
	// URL is the seed URL — required for "sitemap"/"crawl", forbidden for
	// "urls".
	URL string
	// URLs are explicit URLs (max 1000) — required for "urls", forbidden
	// otherwise.
	URLs []string
	// MaxPages is the discovery cap for sitemap/crawl, 1-1000, default 50.
	MaxPages *int
	// MaxDepth is 1-5, default 2.
	MaxDepth *int
	// SameDomainOnly defaults to true.
	SameDomainOnly  *bool
	IncludePatterns []string
	ExcludePatterns []string
	RespectRobots   *bool
	WaitFor         string
	WaitTimeoutMs   *int
	Chunk           *IngestChunkOptions
	// WebhookURL is the completion webhook, HMAC-signed (see
	// GetWebhookSecret).
	WebhookURL string
}

// IngestFilesOptions configures IngestFiles. Uploaded documents are
// converted to Markdown and chunked through the same pipeline as Ingest.
type IngestFilesOptions struct {
	// Chunk configures the heading-aware chunker parameters.
	Chunk *IngestChunkOptions
	// WebhookURL is the completion webhook, HMAC-signed (see
	// GetWebhookSecret).
	WebhookURL string
}

// IngestJob is the outcome of Ingest, GetIngestJob, CancelIngestJob.
type IngestJob struct {
	JobID           string
	Status          IngestStatus
	Mode            IngestMode
	PagesDiscovered int
	PagesProcessed  int
	PagesFailed     int
	TotalChunks     int
	// OutputURL is the signed URL to the final JSONL, once completed.
	OutputURL        string
	ErrorMessage     string
	WebhookURL       string
	WebhookDelivered bool
	CreatedAt        string
	CompletedAt      string
	Warnings         []string
}

// IngestJobSummary is the compact job row from ListIngestJobs (WebhookURL
// replaced by a flag).
type IngestJobSummary struct {
	JobID             string
	Status            IngestStatus
	Mode              IngestMode
	PagesDiscovered   int
	PagesProcessed    int
	PagesFailed       int
	TotalChunks       int
	OutputURL         string
	ErrorMessage      string
	WebhookConfigured bool
	WebhookDelivered  bool
	CreatedAt         string
	CompletedAt       string
}

// IngestJobList is the response from ListIngestJobs.
type IngestJobList struct {
	Jobs    []IngestJobSummary
	Skip    int
	Limit   int
	HasMore bool
}

// WebhookSecret is the project's webhook signing secret and verification
// metadata.
type WebhookSecret struct {
	Secret string
	// SignatureHeader carries the HMAC signature, e.g.
	// "X-Enconvert-Signature".
	SignatureHeader        string
	TimestampHeader        string
	SignatureScheme        string
	ReplayToleranceSeconds int
	// Rotated is true when this response just replaced the previous
	// secret.
	Rotated bool
}

// WebhookRetryResult is the outcome of RetryIngestWebhook.
type WebhookRetryResult struct {
	JobID     string
	Delivered bool
	Attempts  int
	// StatusCode is the HTTP status of the last attempt; nil on network
	// error.
	StatusCode *int
	Detail     string
}

// ---------------------------------------------------------------------------
// Watch
// ---------------------------------------------------------------------------

// WatchDiffMode selects how a watcher diffs successive snapshots.
type WatchDiffMode string

const (
	WatchDiffAuto       WatchDiffMode = "auto"
	WatchDiffText       WatchDiffMode = "text"
	WatchDiffStructured WatchDiffMode = "structured"
	WatchDiffTables     WatchDiffMode = "tables"
	WatchDiffMetadata   WatchDiffMode = "metadata"
)

// WatcherStatus is the lifecycle state of a watcher.
type WatcherStatus string

const (
	WatcherStatusActive  WatcherStatus = "active"
	WatcherStatusPaused  WatcherStatus = "paused"
	WatcherStatusDeleted WatcherStatus = "deleted"
)

// WatchCreateOptions configures CreateWatcher.
type WatchCreateOptions struct {
	// FrequencyMinutes is 60-43200 (hourly floor is hard). Default 60.
	FrequencyMinutes *int
	// DiffMode defaults to "auto" (diff engine picks by content type).
	DiffMode WatchDiffMode
	// TrackFields is an optional field/selector subset for the diff
	// engine.
	TrackFields map[string]any
	// WebhookURL is the change-notification webhook, HMAC-signed.
	WebhookURL string
	// NotifyEmail emails the project owner on changes. Default true.
	NotifyEmail *bool
}

// WatcherUpdate configures UpdateWatcher. At least one field must be set.
type WatcherUpdate struct {
	// FrequencyMinutes is 60-43200.
	FrequencyMinutes *int
	DiffMode         WatchDiffMode
	TrackFields      map[string]any
	// WebhookURL: nil means "leave unchanged"; a pointer to "" explicitly
	// clears the webhook.
	WebhookURL  *string
	NotifyEmail *bool
	// Status must be WatcherStatusActive or WatcherStatusPaused. Deleting
	// goes through DeleteWatcher.
	Status WatcherStatus
}

// Watcher is the outcome of CreateWatcher, GetWatcher, UpdateWatcher,
// DeleteWatcher.
type Watcher struct {
	WatcherID         string
	URL               string
	Status            WatcherStatus
	FrequencyMinutes  int
	DiffMode          WatchDiffMode
	TrackFields       map[string]any
	WebhookURL        string
	NotifyEmail       bool
	ConsecutiveErrors int
	ChecksCount       int
	LastCheckAt       string
	NextCheckAt       string
	LastChangeAt      string
	CreatedAt         string
	UpdatedAt         string
}

// WatcherSummary is the compact watcher row from ListWatchers.
type WatcherSummary struct {
	WatcherID         string
	URL               string
	Status            WatcherStatus
	FrequencyMinutes  int
	ChecksCount       int
	ConsecutiveErrors int
	LastCheckAt       string
	NextCheckAt       string
	LastChangeAt      string
	CreatedAt         string
}

// WatcherList is the response from ListWatchers.
type WatcherList struct {
	Watchers []WatcherSummary
	Skip     int
	Limit    int
	HasMore  bool
}

// WatcherSnapshot is one checked-at entry in a watcher's history.
type WatcherSnapshot struct {
	CheckedAt  string
	HasChanges bool
	// Similarity is 0.0-1.0 similarity to the previous capture.
	Similarity    *float64
	RenderQuality *float64
	ChangeCount   int
	// Changes holds diff entries. Values are untrusted page content —
	// escape before rendering.
	Changes []map[string]any
}

// WatcherSnapshotList is the response from GetWatcherSnapshots.
type WatcherSnapshotList struct {
	WatcherID string
	Snapshots []WatcherSnapshot
	Limit     int
}

// ---------------------------------------------------------------------------
// Shared list options
// ---------------------------------------------------------------------------

// V2ListOptions paginates ListIngestJobs and ListWatchers.
type V2ListOptions struct {
	// Skip is rows to skip (default 0).
	Skip *int
	// Limit is the page size, 1-100 (default 20).
	Limit *int
}

// SnapshotListOptions paginates GetWatcherSnapshots.
type SnapshotListOptions struct {
	// Limit is the page size, 1-100 (default 20).
	Limit *int
}

// ---------------------------------------------------------------------------
// Methods — Perceive
// ---------------------------------------------------------------------------

// Perceive renders one URL into the requested outputs (markdown,
// screenshots, PDF, links, structured data, ...). Synchronous: returns the
// completed operation with 15-minute signed artifact URLs.
func (v *V2) Perceive(ctx context.Context, pageURL string, opts PerceiveOptions) (PerceiveResult, error) {
	body := serializePerceiveOptions(opts)
	body["url"] = pageURL
	data, err := v.postJSON(ctx, "/v2/perceive", body)
	if err != nil {
		return PerceiveResult{}, err
	}
	return toPerceiveResult(data), nil
}

// GetPerceiveOperation re-fetches a perceive operation by id (per_...).
// Artifact URLs are freshly re-signed on every call.
func (v *V2) GetPerceiveOperation(ctx context.Context, operationID string) (PerceiveResult, error) {
	data, err := v.getJSON(ctx, "/v2/perceive/"+url.PathEscape(operationID))
	if err != nil {
		return PerceiveResult{}, err
	}
	return toPerceiveResult(data), nil
}

// PerceiveDirect renders pageURL and streams the single requested artifact
// back as raw bytes (direct_download) instead of a JSON envelope with
// signed URLs. Exactly one artifact-producing output must be requested in
// opts.Outputs (markdown, html_cleaned, html_raw, screenshot,
// screenshot_full_page, pdf, links, images); the operation metadata
// arrives via response headers.
func (v *V2) PerceiveDirect(ctx context.Context, pageURL string, opts PerceiveOptions) (PerceiveDirectResult, error) {
	if err := validatePerceiveDirectOutputs(opts.Outputs); err != nil {
		return PerceiveDirectResult{}, err
	}
	body := serializePerceiveOptions(opts)
	body["url"] = pageURL
	body["direct_download"] = true
	payload, err := json.Marshal(body)
	if err != nil {
		return PerceiveDirectResult{}, err
	}
	return v.doBytes(ctx, http.MethodPost, "/v2/perceive", "application/json", bytes.NewReader(payload))
}

// DownloadPerceiveArtifact streams one stored artifact of an earlier
// perceive operation as raw bytes. output may be "" when the operation
// produced exactly one artifact (else the API answers 400 listing the
// available outputs); a 410 APIError means the artifact passed the plan's
// retention window.
func (v *V2) DownloadPerceiveArtifact(ctx context.Context, operationID string, output PerceiveOutputName) (PerceiveDirectResult, error) {
	requestPath := "/v2/perceive/" + url.PathEscape(operationID) + "?direct_download=true"
	if output != "" {
		requestPath += "&output=" + url.QueryEscape(string(output))
	}
	return v.doBytes(ctx, http.MethodGet, requestPath, "", nil)
}

// validatePerceiveDirectOutputs enforces PerceiveDirect's client-side
// contract: exactly one artifact-producing output requested.
func validatePerceiveDirectOutputs(outputs []PerceiveOutputName) error {
	count := 0
	for _, output := range outputs {
		for _, artifact := range perceiveArtifactOutputs {
			if output == artifact {
				count++
				break
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("perceiveDirect: request exactly one artifact-producing output (markdown, html_cleaned, html_raw, screenshot, screenshot_full_page, pdf, links, images); got %d", count)
	}
	return nil
}

// PerceiveBatch perceives up to 1000 URLs with one shared options block.
// Small batches run inline (completed result); larger ones return status
// "queued" — poll GetPerceiveBatch with the JobID.
func (v *V2) PerceiveBatch(ctx context.Context, urls []string, opts PerceiveBatchOptions) (PerceiveBatchResult, error) {
	body := map[string]any{
		"urls":    urls,
		"options": serializePerceiveOptions(opts.PerceiveOptions),
	}
	if opts.OutputMode != "" {
		body["output_mode"] = opts.OutputMode
	}
	data, err := v.postJSON(ctx, "/v2/perceive/batch", body)
	if err != nil {
		return PerceiveBatchResult{}, err
	}
	return toPerceiveBatchResult(data), nil
}

// GetPerceiveBatch polls a perceive batch by jobID. Items fill in as URLs
// complete.
func (v *V2) GetPerceiveBatch(ctx context.Context, jobID string) (PerceiveBatchResult, error) {
	data, err := v.getJSON(ctx, "/v2/perceive/batch/"+url.PathEscape(jobID))
	if err != nil {
		return PerceiveBatchResult{}, err
	}
	return toPerceiveBatchResult(data), nil
}

// ---------------------------------------------------------------------------
// Methods — Discover
// ---------------------------------------------------------------------------

// Discover lists a site's URLs via sitemap, HTTP crawl, or both. No browser
// rendering — fast, and does not consume perceive quota.
func (v *V2) Discover(ctx context.Context, pageURL string, opts DiscoverOptions) (DiscoverResult, error) {
	body := map[string]any{"url": pageURL}
	if opts.Mode != "" {
		body["mode"] = opts.Mode
	}
	if opts.MaxURLs != nil {
		body["max_urls"] = *opts.MaxURLs
	}
	if opts.MaxDepth != nil {
		body["max_depth"] = *opts.MaxDepth
	}
	if opts.IncludePatterns != nil {
		body["include_patterns"] = opts.IncludePatterns
	}
	if opts.ExcludePatterns != nil {
		body["exclude_patterns"] = opts.ExcludePatterns
	}
	if opts.SameDomainOnly != nil {
		body["same_domain_only"] = *opts.SameDomainOnly
	}
	if opts.RespectRobots != nil {
		body["respect_robots"] = *opts.RespectRobots
	}
	data, err := v.postJSON(ctx, "/v2/discover", body)
	if err != nil {
		return DiscoverResult{}, err
	}
	return toDiscoverResult(data), nil
}

// ---------------------------------------------------------------------------
// Methods — Lookup
// ---------------------------------------------------------------------------

// Lookup runs a categorized web search. With PerceiveTop > 0, the top-N
// result URLs are auto-perceived (each consumes one perceive-quota unit)
// and carry their full PerceiveResult inline.
func (v *V2) Lookup(ctx context.Context, query string, opts LookupOptions) (LookupResult, error) {
	body := map[string]any{"query": query}
	if opts.Category != "" {
		body["category"] = opts.Category
	}
	if opts.Country != "" {
		body["country"] = opts.Country
	}
	if opts.Locale != "" {
		body["locale"] = opts.Locale
	}
	if opts.TimeFilter != "" {
		body["time_filter"] = opts.TimeFilter
	}
	if opts.NumResults != nil {
		body["num_results"] = *opts.NumResults
	}
	if opts.Page != nil {
		body["page"] = *opts.Page
	}
	if opts.Location != "" {
		body["location"] = opts.Location
	}
	if opts.Autocorrect != nil {
		body["autocorrect"] = *opts.Autocorrect
	}
	if opts.PerceiveTop != nil {
		body["perceive_top"] = *opts.PerceiveTop
	}
	data, err := v.postJSON(ctx, "/v2/lookup", body)
	if err != nil {
		return LookupResult{}, err
	}
	return toLookupResult(data), nil
}

// ---------------------------------------------------------------------------
// Methods — Distill
// ---------------------------------------------------------------------------

// Distill extracts structured data matching opts.Schema from explicit URLs
// or from a discovered site. An optional CSSSchema answers fields for
// free; anything it misses escalates to the LLM tier (plan-gated).
func (v *V2) Distill(ctx context.Context, opts DistillOptions) (DistillResult, error) {
	hasURLs := len(opts.URLs) > 0
	hasDiscover := opts.DiscoverFrom != nil
	if hasURLs == hasDiscover {
		return DistillResult{}, errors.New("distill: provide exactly one of URLs or DiscoverFrom")
	}
	if opts.Schema == nil {
		return DistillResult{}, errors.New("distill: Schema is required and must be a non-nil map")
	}

	body := map[string]any{"schema": opts.Schema}
	if hasURLs {
		body["urls"] = opts.URLs
	}
	if opts.DiscoverFrom != nil {
		df := map[string]any{"url": opts.DiscoverFrom.URL}
		if opts.DiscoverFrom.Mode != "" {
			df["mode"] = opts.DiscoverFrom.Mode
		}
		if opts.DiscoverFrom.MaxPages != nil {
			df["max_pages"] = *opts.DiscoverFrom.MaxPages
		}
		body["discover_from"] = df
	}
	if opts.CSSSchema != nil {
		body["css_schema"] = serializeCSSSchema(*opts.CSSSchema)
	}
	if opts.WaitFor != "" {
		body["wait_for"] = opts.WaitFor
	}
	if opts.WaitTimeoutMs != nil {
		body["wait_timeout_ms"] = *opts.WaitTimeoutMs
	}
	if opts.Headers != nil {
		body["headers"] = opts.Headers
	}
	if opts.Cookies != nil {
		body["cookies"] = opts.Cookies
	}
	if opts.RespectRobots != nil {
		body["respect_robots"] = *opts.RespectRobots
	}

	data, err := v.postJSON(ctx, "/v2/distill", body)
	if err != nil {
		return DistillResult{}, err
	}
	return toDistillResult(data), nil
}

// ---------------------------------------------------------------------------
// Methods — Ingest
// ---------------------------------------------------------------------------

// Ingest starts an ingest job: turn explicit URLs or a discovered site into
// chunked, RAG-ready JSONL. Always asynchronous — returns the queued job;
// poll GetIngestJob or configure WebhookURL for completion.
func (v *V2) Ingest(ctx context.Context, opts IngestOptions) (IngestJob, error) {
	mode := opts.Mode
	if mode == "" {
		mode = IngestModeURLs
	}
	if mode == IngestModeURLs {
		if len(opts.URLs) == 0 {
			return IngestJob{}, errors.New("ingest: mode 'urls' requires a non-empty URLs list")
		}
		if opts.URL != "" {
			return IngestJob{}, errors.New("ingest: mode 'urls' does not accept URL")
		}
	} else {
		if opts.URL == "" {
			return IngestJob{}, fmt.Errorf("ingest: mode '%s' requires a seed URL", mode)
		}
		if opts.URLs != nil {
			return IngestJob{}, fmt.Errorf("ingest: mode '%s' does not accept URLs", mode)
		}
	}

	body := map[string]any{}
	if opts.Mode != "" {
		body["mode"] = opts.Mode
	}
	if opts.URL != "" {
		body["url"] = opts.URL
	}
	if opts.URLs != nil {
		body["urls"] = opts.URLs
	}
	if opts.MaxPages != nil {
		body["max_pages"] = *opts.MaxPages
	}
	if opts.MaxDepth != nil {
		body["max_depth"] = *opts.MaxDepth
	}
	if opts.SameDomainOnly != nil {
		body["same_domain_only"] = *opts.SameDomainOnly
	}
	if opts.IncludePatterns != nil {
		body["include_patterns"] = opts.IncludePatterns
	}
	if opts.ExcludePatterns != nil {
		body["exclude_patterns"] = opts.ExcludePatterns
	}
	if opts.RespectRobots != nil {
		body["respect_robots"] = *opts.RespectRobots
	}
	if opts.WaitFor != "" {
		body["wait_for"] = opts.WaitFor
	}
	if opts.WaitTimeoutMs != nil {
		body["wait_timeout_ms"] = *opts.WaitTimeoutMs
	}
	if opts.Chunk != nil {
		chunk := map[string]any{}
		if opts.Chunk.MaxWords != nil {
			chunk["max_words"] = *opts.Chunk.MaxWords
		}
		if opts.Chunk.SentenceOverlap != nil {
			chunk["sentence_overlap"] = *opts.Chunk.SentenceOverlap
		}
		body["chunk"] = chunk
	}
	if opts.WebhookURL != "" {
		body["webhook_url"] = opts.WebhookURL
	}

	data, err := v.postJSON(ctx, "/v2/ingest", body)
	if err != nil {
		return IngestJob{}, err
	}
	return toIngestJob(data), nil
}

// IngestFiles ingests one or more uploaded files into RAG-ready JSONL
// chunks — the file counterpart of Ingest, sharing the same job lifecycle
// (mode "files"). PDF, DOCX, PPTX, XLSX, CSV, HTML, EPUB, TXT/MD and
// legacy/ODF office are accepted. Always asynchronous; poll GetIngestJob or
// configure a webhook. files may be FilePath, FileBytes, or FileInput; at
// least one is required.
func (v *V2) IngestFiles(ctx context.Context, files []FileSource, opts IngestFilesOptions) (IngestJob, error) {
	if len(files) == 0 {
		return IngestJob{}, errors.New("ingestFiles: provide at least one file")
	}

	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	for _, file := range files {
		part, err := file.resolve()
		if err != nil {
			return IngestJob{}, err
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, escapeQuotes(part.filename)))
		contentType := part.contentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header.Set("Content-Type", contentType)
		fw, err := writer.CreatePart(header)
		if err != nil {
			return IngestJob{}, err
		}
		if _, err := fw.Write(part.data); err != nil {
			return IngestJob{}, err
		}
	}
	if opts.Chunk != nil {
		if opts.Chunk.MaxWords != nil {
			if err := writer.WriteField("max_words", strconv.Itoa(*opts.Chunk.MaxWords)); err != nil {
				return IngestJob{}, err
			}
		}
		if opts.Chunk.SentenceOverlap != nil {
			if err := writer.WriteField("sentence_overlap", strconv.Itoa(*opts.Chunk.SentenceOverlap)); err != nil {
				return IngestJob{}, err
			}
		}
	}
	if opts.WebhookURL != "" {
		if err := writer.WriteField("webhook_url", opts.WebhookURL); err != nil {
			return IngestJob{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return IngestJob{}, err
	}

	data, err := v.client.doJSON(ctx, http.MethodPost, "/v2/ingest/files", writer.FormDataContentType(), buf)
	if err != nil {
		return IngestJob{}, err
	}
	return toIngestJob(data), nil
}

// ListIngestJobs lists ingest jobs, newest first.
func (v *V2) ListIngestJobs(ctx context.Context, opts V2ListOptions) (IngestJobList, error) {
	data, err := v.getJSON(ctx, "/v2/ingest"+listQuery(opts))
	if err != nil {
		return IngestJobList{}, err
	}
	rawJobs, _ := data["jobs"].([]any)
	jobs := make([]IngestJobSummary, 0, len(rawJobs))
	for _, rj := range rawJobs {
		if jm, ok := rj.(map[string]any); ok {
			jobs = append(jobs, toIngestJobSummary(jm))
		}
	}
	return IngestJobList{
		Jobs:    jobs,
		Skip:    mapInt(data, "skip", 0),
		Limit:   mapInt(data, "limit", 20),
		HasMore: mapBool(data, "has_more"),
	}, nil
}

// GetIngestJob gets one ingest job by id (ing_...).
func (v *V2) GetIngestJob(ctx context.Context, jobID string) (IngestJob, error) {
	data, err := v.getJSON(ctx, "/v2/ingest/"+url.PathEscape(jobID))
	if err != nil {
		return IngestJob{}, err
	}
	return toIngestJob(data), nil
}

// CancelIngestJob cancels a queued/processing ingest job. Idempotent:
// canceling an already-terminal job returns it unchanged.
func (v *V2) CancelIngestJob(ctx context.Context, jobID string) (IngestJob, error) {
	data, err := v.deleteJSON(ctx, "/v2/ingest/"+url.PathEscape(jobID))
	if err != nil {
		return IngestJob{}, err
	}
	return toIngestJob(data), nil
}

// RetryIngestWebhook re-delivers the completion webhook of a completed job
// (409 if the job is not completed, 400 if it has no webhook configured).
func (v *V2) RetryIngestWebhook(ctx context.Context, jobID string) (WebhookRetryResult, error) {
	data, err := v.postJSON(ctx, "/v2/ingest/"+url.PathEscape(jobID)+"/retry-webhook", nil)
	if err != nil {
		return WebhookRetryResult{}, err
	}
	return WebhookRetryResult{
		JobID:      mapString(data, "job_id"),
		Delivered:  mapBool(data, "delivered"),
		Attempts:   mapInt(data, "attempts", 0),
		StatusCode: mapOptInt(data, "status_code"),
		Detail:     mapString(data, "detail"),
	}, nil
}

// GetWebhookSecret gets (creating on first call) the project's webhook
// signing secret and the header/scheme details needed to verify
// deliveries.
func (v *V2) GetWebhookSecret(ctx context.Context) (WebhookSecret, error) {
	data, err := v.getJSON(ctx, "/v2/ingest/webhook-secret")
	if err != nil {
		return WebhookSecret{}, err
	}
	return toWebhookSecret(data), nil
}

// RotateWebhookSecret rotates the webhook signing secret. Signatures made
// with the previous secret stop verifying immediately.
func (v *V2) RotateWebhookSecret(ctx context.Context) (WebhookSecret, error) {
	data, err := v.postJSON(ctx, "/v2/ingest/webhook-secret/rotate", nil)
	if err != nil {
		return WebhookSecret{}, err
	}
	return toWebhookSecret(data), nil
}

// ---------------------------------------------------------------------------
// Methods — Watch
// ---------------------------------------------------------------------------

// CreateWatcher creates a watcher that re-renders pageURL on a fixed
// cadence (hourly floor) and notifies on changes via email and/or webhook.
func (v *V2) CreateWatcher(ctx context.Context, pageURL string, opts WatchCreateOptions) (Watcher, error) {
	body := map[string]any{"url": pageURL}
	if opts.FrequencyMinutes != nil {
		body["frequency_minutes"] = *opts.FrequencyMinutes
	}
	if opts.DiffMode != "" {
		body["diff_mode"] = opts.DiffMode
	}
	if opts.TrackFields != nil {
		body["track_fields"] = opts.TrackFields
	}
	if opts.WebhookURL != "" {
		body["webhook_url"] = opts.WebhookURL
	}
	if opts.NotifyEmail != nil {
		body["notify_email"] = *opts.NotifyEmail
	}
	data, err := v.postJSON(ctx, "/v2/watch", body)
	if err != nil {
		return Watcher{}, err
	}
	return toWatcher(data), nil
}

// ListWatchers lists watchers, newest first.
func (v *V2) ListWatchers(ctx context.Context, opts V2ListOptions) (WatcherList, error) {
	data, err := v.getJSON(ctx, "/v2/watch"+listQuery(opts))
	if err != nil {
		return WatcherList{}, err
	}
	rawWatchers, _ := data["watchers"].([]any)
	watchers := make([]WatcherSummary, 0, len(rawWatchers))
	for _, rw := range rawWatchers {
		if wm, ok := rw.(map[string]any); ok {
			watchers = append(watchers, toWatcherSummary(wm))
		}
	}
	return WatcherList{
		Watchers: watchers,
		Skip:     mapInt(data, "skip", 0),
		Limit:    mapInt(data, "limit", 20),
		HasMore:  mapBool(data, "has_more"),
	}, nil
}

// GetWatcher gets one watcher by id (wat_...). Deleted watchers read as
// 404.
func (v *V2) GetWatcher(ctx context.Context, watcherID string) (Watcher, error) {
	data, err := v.getJSON(ctx, "/v2/watch/"+url.PathEscape(watcherID))
	if err != nil {
		return Watcher{}, err
	}
	return toWatcher(data), nil
}

// GetWatcherSnapshots pages through a watcher's check history, newest
// first.
func (v *V2) GetWatcherSnapshots(ctx context.Context, watcherID string, opts SnapshotListOptions) (WatcherSnapshotList, error) {
	query := ""
	if opts.Limit != nil {
		query = fmt.Sprintf("?limit=%d", *opts.Limit)
	}
	data, err := v.getJSON(ctx, "/v2/watch/"+url.PathEscape(watcherID)+"/snapshots"+query)
	if err != nil {
		return WatcherSnapshotList{}, err
	}
	rawSnapshots, _ := data["snapshots"].([]any)
	snapshots := make([]WatcherSnapshot, 0, len(rawSnapshots))
	for _, rs := range rawSnapshots {
		if sm, ok := rs.(map[string]any); ok {
			snapshots = append(snapshots, toWatcherSnapshot(sm))
		}
	}
	return WatcherSnapshotList{
		WatcherID: mapString(data, "watcher_id"),
		Snapshots: snapshots,
		Limit:     mapInt(data, "limit", 20),
	}, nil
}

// UpdateWatcher updates a watcher. At least one field is required. Set
// WebhookURL to a pointer to "" to clear the webhook; resuming a paused
// watcher re-checks the plan's watcher cap.
func (v *V2) UpdateWatcher(ctx context.Context, watcherID string, updates WatcherUpdate) (Watcher, error) {
	body := map[string]any{}
	if updates.FrequencyMinutes != nil {
		body["frequency_minutes"] = *updates.FrequencyMinutes
	}
	if updates.DiffMode != "" {
		body["diff_mode"] = updates.DiffMode
	}
	if updates.TrackFields != nil {
		body["track_fields"] = updates.TrackFields
	}
	if updates.WebhookURL != nil {
		body["webhook_url"] = *updates.WebhookURL
	}
	if updates.NotifyEmail != nil {
		body["notify_email"] = *updates.NotifyEmail
	}
	if updates.Status != "" {
		body["status"] = updates.Status
	}
	if len(body) == 0 {
		return Watcher{}, errors.New("updateWatcher: provide at least one field to update")
	}
	data, err := v.patchJSON(ctx, "/v2/watch/"+url.PathEscape(watcherID), body)
	if err != nil {
		return Watcher{}, err
	}
	return toWatcher(data), nil
}

// DeleteWatcher soft-deletes a watcher (idempotent). Returns the
// tombstoned watcher with status "deleted".
func (v *V2) DeleteWatcher(ctx context.Context, watcherID string) (Watcher, error) {
	data, err := v.deleteJSON(ctx, "/v2/watch/"+url.PathEscape(watcherID))
	if err != nil {
		return Watcher{}, err
	}
	return toWatcher(data), nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (v *V2) getJSON(ctx context.Context, requestPath string) (map[string]any, error) {
	return v.client.doJSON(ctx, http.MethodGet, requestPath, "", nil)
}

// postJSON always sends a Content-Type: application/json header, even with
// a nil body (matching the Node SDK, whose retryIngestWebhook and
// rotateWebhookSecret calls send that header with no body).
func (v *V2) postJSON(ctx context.Context, requestPath string, body map[string]any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	return v.client.doJSON(ctx, http.MethodPost, requestPath, "application/json", reader)
}

func (v *V2) patchJSON(ctx context.Context, requestPath string, body map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return v.client.doJSON(ctx, http.MethodPatch, requestPath, "application/json", bytes.NewReader(payload))
}

func (v *V2) deleteJSON(ctx context.Context, requestPath string) (map[string]any, error) {
	return v.client.doJSON(ctx, http.MethodDelete, requestPath, "", nil)
}

// doBytes issues an authenticated request expecting raw artifact bytes
// (direct_download). Non-2xx responses carry a normal JSON error body and
// go through raiseForStatus exactly like the JSON paths.
func (v *V2) doBytes(ctx context.Context, method, requestPath, contentType string, body io.Reader) (PerceiveDirectResult, error) {
	resp, err := v.client.request(ctx, method, requestPath, contentType, body)
	if err != nil {
		return PerceiveDirectResult{}, err
	}
	if err := raiseForStatus(resp); err != nil {
		return PerceiveDirectResult{}, err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return PerceiveDirectResult{}, err
	}
	return toPerceiveDirectResult(content, resp.Header), nil
}

func listQuery(opts V2ListOptions) string {
	params := url.Values{}
	if opts.Skip != nil {
		params.Set("skip", strconv.Itoa(*opts.Skip))
	}
	if opts.Limit != nil {
		params.Set("limit", strconv.Itoa(*opts.Limit))
	}
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}

// ---------------------------------------------------------------------------
// Request serializers
// ---------------------------------------------------------------------------

func serializePerceiveOptions(o PerceiveOptions) map[string]any {
	out := map[string]any{}
	if o.Outputs != nil {
		out["outputs"] = o.Outputs
	}
	if o.Extract != nil {
		out["extract"] = o.Extract
	}
	if o.Schema != nil {
		out["schema"] = o.Schema
	}
	if o.WaitFor != "" {
		out["wait_for"] = o.WaitFor
	}
	if o.WaitTimeoutMs != nil {
		out["wait_timeout_ms"] = *o.WaitTimeoutMs
	}
	if o.JSCode != "" {
		out["js_code"] = o.JSCode
	}
	if o.Viewport != nil {
		out["viewport"] = o.Viewport
	}
	if o.Headers != nil {
		out["headers"] = o.Headers
	}
	if o.Cookies != nil {
		out["cookies"] = o.Cookies
	}
	if o.Auth != nil {
		out["auth"] = o.Auth
	}
	if o.ProxyURL != "" {
		out["proxy_url"] = o.ProxyURL
	}
	if o.Geolocation != nil {
		out["geolocation"] = o.Geolocation
	}
	if o.ActionChain != nil {
		out["action_chain"] = o.ActionChain
	}
	if o.CacheMode != "" {
		out["cache_mode"] = o.CacheMode
	}
	if o.PDFOptions != nil {
		out["pdf_options"] = serializePDFOptions(o.PDFOptions)
	}
	if o.BlockResources != nil {
		out["block_resources"] = o.BlockResources
	}
	if o.RespectRobots != nil {
		out["respect_robots"] = *o.RespectRobots
	}
	if o.Mobile != nil {
		out["mobile"] = *o.Mobile
	}
	if o.OnlyMainContent != nil {
		out["only_main_content"] = *o.OnlyMainContent
	}
	if o.DirectDownload != nil {
		out["direct_download"] = *o.DirectDownload
	}
	return out
}

func serializeCSSField(f CSSField) map[string]any {
	out := map[string]any{"name": f.Name, "type": f.Type}
	if f.Selector != "" {
		out["selector"] = f.Selector
	}
	if f.Attribute != "" {
		out["attribute"] = f.Attribute
	}
	if f.Pattern != "" {
		out["pattern"] = f.Pattern
	}
	if f.Default != nil {
		out["default"] = f.Default
	}
	if f.Transform != "" {
		out["transform"] = f.Transform
	}
	if f.Fields != nil {
		fields := make([]map[string]any, 0, len(f.Fields))
		for _, sub := range f.Fields {
			fields = append(fields, serializeCSSField(sub))
		}
		out["fields"] = fields
	}
	return out
}

func serializeCSSSchema(s CSSSchema) map[string]any {
	fields := make([]map[string]any, 0, len(s.Fields))
	for _, f := range s.Fields {
		fields = append(fields, serializeCSSField(f))
	}
	// NOTE: baseSelector stays camelCase on the wire (matches the Node SDK
	// and the API's schema for this nested object) while targetField is
	// snake_case — this asymmetry is intentional, not a typo.
	out := map[string]any{
		"baseSelector": s.BaseSelector,
		"fields":       fields,
	}
	if s.Name != "" {
		out["name"] = s.Name
	}
	if s.TargetField != "" {
		out["target_field"] = s.TargetField
	}
	return out
}

// ---------------------------------------------------------------------------
// Response mappers. Optional fields may be absent entirely
// (response_model_exclude_none) — every access is guarded.
// ---------------------------------------------------------------------------

func mapStringDefault(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func toTokens(v any) V2Tokens {
	obj, _ := v.(map[string]any)
	return V2Tokens{Input: mapInt(obj, "input", 0), Output: mapInt(obj, "output", 0)}
}

func toOutputArtifact(v any) V2OutputArtifact {
	obj, _ := v.(map[string]any)
	return V2OutputArtifact{
		URL:         mapString(obj, "url"),
		ObjectKey:   mapString(obj, "object_key"),
		SizeBytes:   int64(mapFloat64(obj, "size_bytes", 0)),
		ContentType: mapStringDefault(obj, "content_type", "application/octet-stream"),
		ExpiresIn:   mapInt(obj, "expires_in", 900),
	}
}

func toPerceiveResult(d map[string]any) PerceiveResult {
	rawOutputs := mapObject(d, "outputs")
	outputs := map[string]V2OutputArtifact{}
	for name, artifact := range rawOutputs {
		outputs[name] = toOutputArtifact(artifact)
	}
	deductions := map[string]float64{}
	for name, weight := range mapObject(d, "deductions") {
		if w, ok := weight.(float64); ok {
			deductions[name] = w
		}
	}
	return PerceiveResult{
		OperationID:    mapString(d, "operation_id"),
		Status:         PerceiveStatus(mapString(d, "status")),
		URL:            mapString(d, "url"),
		URLFinal:       mapString(d, "url_final"),
		ContentHash:    mapString(d, "content_hash"),
		RenderQuality:  mapOptFloat64(d, "render_quality"),
		StatusCode:     mapOptInt(d, "status_code"),
		Deductions:     deductions,
		CacheHit:       mapBool(d, "cache_hit"),
		Outputs:        outputs,
		Structured:     mapObject(d, "structured"),
		ExtractionTier: PerceiveExtractionTier(mapString(d, "extraction_tier")),
		Tokens:         toTokens(d["tokens"]),
		CostCents:      mapInt(d, "cost_cents", 0),
		DurationMs:     mapOptInt(d, "duration_ms"),
		Error:          mapString(d, "error"),
		Warnings:       mapStringSlice(d, "warnings"),
		OptionsEcho:    mapObject(d, "options_echo"),
	}
}

// toPerceiveDirectResult maps a direct-download response's headers onto
// the result struct. Optional headers may be absent — every parse is
// guarded, mirroring the JSON mappers.
func toPerceiveDirectResult(content []byte, header http.Header) PerceiveDirectResult {
	result := PerceiveDirectResult{
		Content:     content,
		ContentType: header.Get("Content-Type"),
		Filename:    filenameFromDisposition(header.Get("Content-Disposition")),
		OperationID: header.Get("X-Operation-Id"),
		ObjectKey:   header.Get("X-Object-Key"),
		CacheHit:    header.Get("X-Cache-Hit") == "true",
		ContentHash: header.Get("X-Content-Hash"),
	}
	if quality, err := strconv.ParseFloat(header.Get("X-Render-Quality"), 64); err == nil {
		result.RenderQuality = &quality
	}
	if status, err := strconv.Atoi(header.Get("X-Source-Status-Code")); err == nil {
		result.SourceStatusCode = &status
	}
	if warnings, err := strconv.Atoi(header.Get("X-Warnings-Count")); err == nil {
		result.WarningsCount = warnings
	}
	return result
}

// filenameFromDisposition extracts the filename parameter from a
// Content-Disposition header, e.g. `attachment; filename="per_x_markdown.md"`.
func filenameFromDisposition(disposition string) string {
	if disposition == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		return params["filename"]
	}
	return ""
}

func toPerceiveBatchResult(d map[string]any) PerceiveBatchResult {
	rawItems, _ := d["items"].([]any)
	items := make([]PerceiveResult, 0, len(rawItems))
	for _, ri := range rawItems {
		if im, ok := ri.(map[string]any); ok {
			items = append(items, toPerceiveResult(im))
		}
	}
	var zip *V2OutputArtifact
	if zv, ok := d["zip"]; ok && zv != nil {
		z := toOutputArtifact(zv)
		zip = &z
	}
	return PerceiveBatchResult{
		JobID:      mapString(d, "job_id"),
		Status:     PerceiveBatchStatus(mapString(d, "status")),
		OutputMode: PerceiveBatchOutputMode(mapStringDefault(d, "output_mode", "manifest")),
		Total:      mapInt(d, "total", 0),
		Completed:  mapInt(d, "completed", 0),
		Failed:     mapInt(d, "failed", 0),
		Pending:    mapInt(d, "pending", 0),
		Zip:        zip,
		Items:      items,
		Warnings:   mapStringSlice(d, "warnings"),
	}
}

func toDiscoverResult(d map[string]any) DiscoverResult {
	sources := map[string]int{}
	if rawSources := mapObject(d, "sources"); rawSources != nil {
		for k, v := range rawSources {
			if f, ok := v.(float64); ok {
				sources[k] = int(f)
			}
		}
	}
	return DiscoverResult{
		URL:             mapString(d, "url"),
		Mode:            DiscoverMode(mapString(d, "mode")),
		Total:           mapInt(d, "total", 0),
		URLs:            mapStringSlice(d, "urls"),
		PagesCrawled:    mapInt(d, "pages_crawled", 0),
		Truncated:       mapBool(d, "truncated"),
		RobotsRespected: mapBool(d, "robots_respected"),
		Sources:         sources,
		Warnings:        mapStringSlice(d, "warnings"),
	}
}

func toLookupItem(d map[string]any) LookupItem {
	var perceive *PerceiveResult
	if p := mapObject(d, "perceive"); p != nil {
		pr := toPerceiveResult(p)
		perceive = &pr
	}
	extra := mapObject(d, "extra")
	if extra == nil {
		extra = map[string]any{}
	}
	return LookupItem{
		Title:        mapString(d, "title"),
		URL:          mapString(d, "url"),
		Snippet:      mapString(d, "snippet"),
		Position:     mapOptInt(d, "position"),
		Source:       mapString(d, "source"),
		Date:         mapString(d, "date"),
		ImageURL:     mapString(d, "image_url"),
		ThumbnailURL: mapString(d, "thumbnail_url"),
		Extra:        extra,
		Perceive:     perceive,
	}
}

func toLookupResult(d map[string]any) LookupResult {
	rawResults, _ := d["results"].([]any)
	results := make([]LookupItem, 0, len(rawResults))
	for _, rr := range rawResults {
		if rm, ok := rr.(map[string]any); ok {
			results = append(results, toLookupItem(rm))
		}
	}
	return LookupResult{
		LookupID:             mapOptInt(d, "lookup_id"),
		Query:                mapString(d, "query"),
		Category:             LookupCategory(mapString(d, "category")),
		Country:              mapString(d, "country"),
		Locale:               mapString(d, "locale"),
		TimeFilter:           LookupTimeFilter(mapString(d, "time_filter")),
		Total:                mapInt(d, "total", 0),
		Results:              results,
		PerceiveTop:          mapInt(d, "perceive_top", 0),
		PerceiveOperationIDs: mapStringSlice(d, "perceive_operation_ids"),
		AnswerBox:            mapObject(d, "answer_box"),
		KnowledgeGraph:       mapObject(d, "knowledge_graph"),
		Credits:              mapOptInt(d, "credits"),
		CostCents:            mapInt(d, "cost_cents", 0),
		Warnings:             mapStringSlice(d, "warnings"),
	}
}

func toDistillItem(d map[string]any) DistillItem {
	return DistillItem{
		URL:            mapString(d, "url"),
		URLFinal:       mapString(d, "url_final"),
		Status:         DistillItemStatus(mapStringDefault(d, "status", "completed")),
		Data:           mapObject(d, "data"),
		ExtractionTier: DistillExtractionTier(mapStringDefault(d, "extraction_tier", "none")),
		FieldsFromCSS:  mapInt(d, "fields_from_css", 0),
		FieldsFromLLM:  mapInt(d, "fields_from_llm", 0),
		RenderQuality:  mapOptFloat64(d, "render_quality"),
		Tokens:         toTokens(d["tokens"]),
		CostCents:      mapInt(d, "cost_cents", 0),
		Error:          mapString(d, "error"),
		Warnings:       mapStringSlice(d, "warnings"),
	}
}

func toDistillResult(d map[string]any) DistillResult {
	rawResults, _ := d["results"].([]any)
	results := make([]DistillItem, 0, len(rawResults))
	for _, rr := range rawResults {
		if rm, ok := rr.(map[string]any); ok {
			results = append(results, toDistillItem(rm))
		}
	}
	return DistillResult{
		OperationID:    mapString(d, "operation_id"),
		Total:          mapInt(d, "total", 0),
		Completed:      mapInt(d, "completed", 0),
		Failed:         mapInt(d, "failed", 0),
		Results:        results,
		TotalCostCents: mapInt(d, "total_cost_cents", 0),
		Warnings:       mapStringSlice(d, "warnings"),
	}
}

func toIngestJob(d map[string]any) IngestJob {
	return IngestJob{
		JobID:            mapString(d, "job_id"),
		Status:           IngestStatus(mapString(d, "status")),
		Mode:             IngestMode(mapString(d, "mode")),
		PagesDiscovered:  mapInt(d, "pages_discovered", 0),
		PagesProcessed:   mapInt(d, "pages_processed", 0),
		PagesFailed:      mapInt(d, "pages_failed", 0),
		TotalChunks:      mapInt(d, "total_chunks", 0),
		OutputURL:        mapString(d, "output_url"),
		ErrorMessage:     mapString(d, "error_message"),
		WebhookURL:       mapString(d, "webhook_url"),
		WebhookDelivered: mapBool(d, "webhook_delivered"),
		CreatedAt:        mapString(d, "created_at"),
		CompletedAt:      mapString(d, "completed_at"),
		Warnings:         mapStringSlice(d, "warnings"),
	}
}

func toIngestJobSummary(d map[string]any) IngestJobSummary {
	return IngestJobSummary{
		JobID:             mapString(d, "job_id"),
		Status:            IngestStatus(mapString(d, "status")),
		Mode:              IngestMode(mapString(d, "mode")),
		PagesDiscovered:   mapInt(d, "pages_discovered", 0),
		PagesProcessed:    mapInt(d, "pages_processed", 0),
		PagesFailed:       mapInt(d, "pages_failed", 0),
		TotalChunks:       mapInt(d, "total_chunks", 0),
		OutputURL:         mapString(d, "output_url"),
		ErrorMessage:      mapString(d, "error_message"),
		WebhookConfigured: mapBool(d, "webhook_configured"),
		WebhookDelivered:  mapBool(d, "webhook_delivered"),
		CreatedAt:         mapString(d, "created_at"),
		CompletedAt:       mapString(d, "completed_at"),
	}
}

func toWebhookSecret(d map[string]any) WebhookSecret {
	return WebhookSecret{
		Secret:                 mapString(d, "secret"),
		SignatureHeader:        mapString(d, "signature_header"),
		TimestampHeader:        mapString(d, "timestamp_header"),
		SignatureScheme:        mapString(d, "signature_scheme"),
		ReplayToleranceSeconds: mapInt(d, "replay_tolerance_seconds", 0),
		Rotated:                mapBool(d, "rotated"),
	}
}

func toWatcher(d map[string]any) Watcher {
	return Watcher{
		WatcherID:         mapString(d, "watcher_id"),
		URL:               mapString(d, "url"),
		Status:            WatcherStatus(mapString(d, "status")),
		FrequencyMinutes:  mapInt(d, "frequency_minutes", 0),
		DiffMode:          WatchDiffMode(mapString(d, "diff_mode")),
		TrackFields:       mapObject(d, "track_fields"),
		WebhookURL:        mapString(d, "webhook_url"),
		NotifyEmail:       notifyEmailDefault(d),
		ConsecutiveErrors: mapInt(d, "consecutive_errors", 0),
		ChecksCount:       mapInt(d, "checks_count", 0),
		LastCheckAt:       mapString(d, "last_check_at"),
		NextCheckAt:       mapString(d, "next_check_at"),
		LastChangeAt:      mapString(d, "last_change_at"),
		CreatedAt:         mapString(d, "created_at"),
		UpdatedAt:         mapString(d, "updated_at"),
	}
}

// notifyEmailDefault mirrors the Node SDK's `notify_email !== false` —
// true unless the field is explicitly false.
func notifyEmailDefault(d map[string]any) bool {
	v, ok := d["notify_email"]
	return !(ok && v == false)
}

func toWatcherSummary(d map[string]any) WatcherSummary {
	return WatcherSummary{
		WatcherID:         mapString(d, "watcher_id"),
		URL:               mapString(d, "url"),
		Status:            WatcherStatus(mapString(d, "status")),
		FrequencyMinutes:  mapInt(d, "frequency_minutes", 0),
		ChecksCount:       mapInt(d, "checks_count", 0),
		ConsecutiveErrors: mapInt(d, "consecutive_errors", 0),
		LastCheckAt:       mapString(d, "last_check_at"),
		NextCheckAt:       mapString(d, "next_check_at"),
		LastChangeAt:      mapString(d, "last_change_at"),
		CreatedAt:         mapString(d, "created_at"),
	}
}

func toWatcherSnapshot(d map[string]any) WatcherSnapshot {
	var changes []map[string]any
	if rawChanges, ok := d["changes"].([]any); ok {
		for _, rc := range rawChanges {
			if cm, ok := rc.(map[string]any); ok {
				changes = append(changes, cm)
			}
		}
	}
	if changes == nil {
		changes = []map[string]any{}
	}
	return WatcherSnapshot{
		CheckedAt:     mapString(d, "checked_at"),
		HasChanges:    mapBool(d, "has_changes"),
		Similarity:    mapOptFloat64(d, "similarity"),
		RenderQuality: mapOptFloat64(d, "render_quality"),
		ChangeCount:   mapInt(d, "change_count", 0),
		Changes:       changes,
	}
}
