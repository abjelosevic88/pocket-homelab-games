module github.com/abjelosevic88/pocket-homelab-games

go 1.26.5

require (
	github.com/abjelosevic88/pocket-homelab-core v0.18.5
	github.com/go-chi/chi/v5 v5.5.0
	golang.org/x/crypto v0.58.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/PuerkitoBio/goquery v1.13.0 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327 // indirect
	github.com/chromedp/chromedp v0.14.2 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260213210345-44df1a37e875 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Reviewed SDK snapshot; see third_party/pocket-core/sdk.lock.json.
replace github.com/abjelosevic88/pocket-homelab-core => ./third_party/pocket-core
