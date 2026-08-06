// Package enconvert is the Go SDK for Enconvert (https://enconvert.com):
// read any page or file into agent-ready Markdown/JSON/screenshots, every
// read scored 0.0-1.0 for render quality. The V2 namespace (Client.V2)
// renders pages into agent-ready artifacts, runs web search, extracts
// structured data, ingests sites or files into RAG-ready JSONL, and watches
// pages for changes. The same client also converts URLs to PDFs, captures
// screenshots, extracts Markdown, crawls whole websites, transforms images,
// and converts 43 document/data format pairs.
//
// # Quick start
//
//	client, err := enconvert.New("sk_...")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	result, err := client.ConvertURLToPDF(ctx, "https://example.com", enconvert.URLToPDFOptions{
//		SaveTo: "page.pdf",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(result.PresignedURL)
//
// # Errors
//
// Failed requests return *enconvert.APIError. Use IsAuthenticationError,
// IsQuotaError, and IsRateLimitError to check for the conditions the API
// signals with dedicated status codes (401/403, 402, 429 respectively).
package enconvert

// Version is the SDK's release version.
const Version = "0.1.1"
