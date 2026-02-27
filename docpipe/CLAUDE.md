# docpipe

Responsabilite: Pipeline d'extraction de documents multi-format (DOCX, ODT, PDF, Markdown, texte, HTML) en sections structurees, pure Go.
Depend de: `golang.org/x/net/html`, `github.com/pdfcpu/pdfcpu`, `github.com/hazyhaar/pkg/kit`, `github.com/hazyhaar/pkg/connectivity`, `github.com/modelcontextprotocol/go-sdk/mcp`
Dependants: `veille/internal/pipeline/handler_document`, `e2e/` (tests integration)
Point d'entree: `docpipe.go`
Types cles: `Pipeline`, `Config` (MaxFileSize, Logger), `Document` (Path, Format, Title, Sections, RawText, Quality), `Section` (Title, Level, Text, Type, Metadata), `Format` (constantes: docx, odt, pdf, md, txt, html), `ExtractionQuality` (PageCount, CharsPerPage, PrintableRatio, WordlikeRatio, HasImageStreams, VisualRefCount)
Invariants:
- Tous les parsers sont pure Go, CGO_ENABLED=0 compatible, zero dependance externe binaire
- MaxFileSize par defaut = 100 MB
- PDF : pdfcpu (page-aware, CIDFont, ToUnicode, encryption), quality gate (`ExtractionQuality`, `NeedsOCR()`, `HasVisualGap()`)
- DOCX : parse word/document.xml dans l'archive ZIP, limite profondeur XML 256 (anti XML-bomb)
- ODT : parse content.xml dans l'archive ZIP, limite profondeur XML 256 (anti XML-bomb)
- HTML : filtrage CSS hidden text (display:none, visibility:hidden, font-size:0, opacity:0)
- RegisterMCP expose 3 tools : `docpipe_extract`, `docpipe_detect`, `docpipe_formats`
- RegisterConnectivity expose 2 handlers : `docpipe_extract`, `docpipe_detect`
NE PAS:
- Utiliser une lib PDF C/CGO — pdfcpu est pure Go, pas de CGO
- Oublier qu'un format non supporte retourne une erreur, pas un Document vide
- Confondre `extractHTMLFile` (docpipe, fichier local) avec `extract.Extract` (package extract, raw bytes)
