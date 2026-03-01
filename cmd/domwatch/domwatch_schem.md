# domwatch (cmd/domwatch) -- Binary Technical Schema

> DOM observation daemon -- Chrome headless, CDP mutations, multi-stealth, YAML/CLI config

## Usage Modes

```
╔══════════════════════════════════════════════════════════════════╗
║  domwatch -config domwatch.yaml          # Multi-page from YAML ║
║  domwatch -url https://example.com       # Single-page observe  ║
║  domwatch -profile https://example.com   # Profile page + exit  ║
╚══════════════════════════════════════════════════════════════════╝
```

## CLI Flags

```
╔═══════════════╦══════════╦═══════════════════════════════════════╗
║ Flag          ║ Default  ║ Purpose                               ║
╠═══════════════╬══════════╬═══════════════════════════════════════╣
║ -config       ║ ""       ║ Path to YAML config file              ║
║ -url          ║ ""       ║ Single URL to observe (stdout sink)   ║
║ -profile      ║ ""       ║ Profile URL and exit                  ║
║ -log-level    ║ info     ║ debug/info/warn/error                 ║
╚═══════════════╩══════════╩═══════════════════════════════════════╝
```

## Startup Sequence

```
╔══════════════════════════════════════════════════════════════════╗
║  Profile Mode:                                                  ║
║    1. defaultConfig() → headless, 1GB mem, 4h recycle           ║
║    2. NewStdoutSink()                                           ║
║    3. domwatch.New(cfg, logger, sink)                           ║
║    4. w.Start(ctx) → launch Chrome headless                     ║
║    5. w.ProfilePage(url) → analyse DOM → emit JSON profile      ║
║    6. w.Stop() → close browser                                  ║
║                                                                  ║
║  Single URL Mode:                                                ║
║    1. defaultConfig() + set Pages[0]={url, stealth=auto}        ║
║    2. NewStdoutSink()                                           ║
║    3. domwatch.New(cfg, logger, sink)                           ║
║    4. w.Start(ctx) → launch browser → observe page              ║
║    5. <-ctx.Done() → w.Stop()                                   ║
║                                                                  ║
║  Config Mode:                                                    ║
║    1. LoadConfigFile(path) → parse YAML with defaults            ║
║    2. Build sinks from config (stdout/webhook)                   ║
║    3. domwatch.New(cfg, logger, sinks...)                        ║
║    4. w.Start(ctx) → launch browser → observe all pages          ║
║    5. <-ctx.Done() → w.Stop()                                   ║
╚══════════════════════════════════════════════════════════════════╝
```

## Architecture

```
╔══════════════════════════════════════════════════════════════════════════╗
║                         domwatch.Watcher                                ║
║                                                                         ║
║  ┌────────────────────────────┐                                         ║
║  │  browser.Manager           │  Chrome lifecycle manager               ║
║  │  ├── Start(ctx) → *Browser │  Launch Chrome (headless/headful)       ║
║  │  ├── Recycle callback      │  Kill + restart every RecycleInterval   ║
║  │  ├── MemoryLimit: 1 GB     │  Monitor RSS, kill if exceeded          ║
║  │  └── ResourceBlocking      │  Block images/fonts/media               ║
║  └────────────┬───────────────┘                                         ║
║               │                                                          ║
║  ┌────────────▼───────────────┐                                         ║
║  │  Per-Page Observer          │  One per configured page                ║
║  │  ├── browser.Tab            │  CDP connection to page                 ║
║  │  ├── CDP DOM.enable         │  childNodeInserted/Removed/etc.        ║
║  │  ├── Shadow DOM tracking    │  Open shadow roots observed            ║
║  │  ├── SPA detection          │  popstate + pushState hooks            ║
║  │  ├── Debouncer              │  Window=250ms, MaxBuffer=1000          ║
║  │  └── XPath generator        │  Stable paths for mutation Records     ║
║  └────────────┬───────────────┘                                         ║
║               │ Batch / Snapshot / Profile                               ║
║  ┌────────────▼───────────────┐                                         ║
║  │  sink.Router                │  Fan-out to all registered sinks        ║
║  │  ├── StdoutSink             │  JSON-lines to stdout                   ║
║  │  ├── WebhookSink            │  HTTP POST with retry                   ║
║  │  └── CallbackSink           │  In-process (→ domkeeper)               ║
║  └────────────────────────────┘                                         ║
║                                                                         ║
║  ┌────────────────────────────┐                                         ║
║  │  fetcher.Fetcher            │  HTTP-only path (stealth=0/auto)       ║
║  │  ├── HEAD + GET             │  Content sufficiency check             ║
║  │  └── detect.IsSufficient    │  Text density heuristic                ║
║  └────────────────────────────┘                                         ║
║                                                                         ║
║  ┌────────────────────────────┐                                         ║
║  │  profiler.Profile()         │  Structural DOM analysis               ║
║  │  ├── Landmarks (main,      │  HTML5 landmark elements                ║
║  │  │   article, section...)   │                                        ║
║  │  ├── Dynamic zones          │  High mutation rate regions            ║
║  │  ├── Static zones           │  Zero mutation regions                 ║
║  │  ├── Content selectors      │  Auto-detected CSS selectors           ║
║  │  ├── Fingerprint (hash)     │  Structural DOM hash                   ║
║  │  └── Text density map       │  XPath → text/markup ratio             ║
║  └────────────────────────────┘                                         ║
╚══════════════════════════════════════════════════════════════════════════╝
```

## Stealth Levels

```
╔═══════════╦══════════════════════════════════════════════════════╗
║ Level     ║ Description                                         ║
╠═══════════╬══════════════════════════════════════════════════════╣
║ 0 (HTTP)  ║ Pure HTTP GET -- no browser, fast, limited          ║
║ 1 (headless)║ Chrome headless -- CDP, full JS, no display       ║
║ 2 (headful) ║ Chrome headful -- Xvfb, stealth plugins, slow     ║
║ auto      ║ Try HTTP first → check sufficiency → escalate       ║
╚═══════════╩══════════════════════════════════════════════════════╝
```

## YAML Config Format

```yaml
browser:
  remote: ""                    # Remote Chrome CDP endpoint
  memory_limit: 1073741824      # 1 GB
  recycle_interval: 4h
  resource_blocking: [images, fonts, media]
  stealth: headless             # headless | headful
  xvfb_display: ":99"

pages:
  - id: "page-1"
    url: "https://example.com"
    stealth_level: "auto"       # 0|1|2|auto
    selectors: ["main", "article"]
    filters: []
    snapshot_interval: 4h
    profile: true               # Run profiler on first visit

debounce:
  window: 250ms
  max_buffer: 1000

sinks:
  - type: stdout
  - type: webhook
    url: "https://hooks.example.com/domwatch"
```

## Output Types (mutation package)

```
Batch:
  ├── id:           UUIDv7
  ├── page_url:     observed URL
  ├── page_id:      stable caller-provided ID
  ├── seq:          monotonic per-page (gap detection)
  ├── records[]:    Record{op, xpath, node_type, tag, value, html}
  ├── timestamp:    epoch ms at flush
  └── snapshot_ref: last snapshot ID

Snapshot:
  ├── id:           UUIDv7
  ├── page_url / page_id
  ├── html:         full serialized DOM ([]byte)
  ├── html_hash:    SHA-256 hex
  └── timestamp

Profile:
  ├── page_url
  ├── landmarks[]:       {tag, xpath, role}
  ├── dynamic_zones[]:   {xpath, selector, mutation_rate}
  ├── static_zones[]:    {xpath, selector, mutation_rate=0}
  ├── content_selectors: auto-detected CSS selectors
  ├── fingerprint:       structural hash
  └── text_density_map:  {xpath → float64}
```

## Connectivity Services (2 services)

```
domwatch_observe  -- Start observing a page (page_id, url, stealth_level)
domwatch_profile  -- Profile a page (url, page_id)
```

## Dependencies

```
domwatch → go-rod/rod, go-rod/stealth (Chrome automation)
domwatch → pkg/connectivity, pkg/idgen
domwatch → mutation (public types for consumers)
```
