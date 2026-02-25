# docpipe

Responsabilite: Pipeline d'extraction de documents multi-format (DOCX, ODT, PDF, Markdown, texte, HTML) en sections structurees, pure Go.
Depend de: `golang.org/x/net/html`, `github.com/hazyhaar/pkg/kit`, `github.com/hazyhaar/pkg/connectivity`, `github.com/modelcontextprotocol/go-sdk/mcp`
Dependants: `veille/internal/pipeline/handler_document`, `e2e/` (tests integration)
Point d'entree: `docpipe.go`
Types cles: `Pipeline`, `Config` (MaxFileSize, Logger), `Document` (Path, Format, Title, Sections, RawText), `Section` (Title, Level, Text, Type, Metadata), `Format` (constantes: docx, odt, pdf, md, txt, html)
Invariants:
- Tous les parsers sont pure Go, CGO_ENABLED=0 compatible, zero dependance externe binaire
- MaxFileSize par defaut = 100 MB
- PDF : decode FlateDecode streams et parse les operateurs Tj/TJ — pas de lib PDF externe
- DOCX : parse word/document.xml dans l'archive ZIP
- ODT : parse content.xml dans l'archive ZIP
- RegisterMCP expose 3 tools : `docpipe_extract`, `docpipe_detect`, `docpipe_formats`
- RegisterConnectivity expose 2 handlers : `docpipe_extract`, `docpipe_detect`
NE PAS:
- Utiliser une lib PDF C/CGO — le parseur PDF interne est volontairement minimal
- Oublier qu'un format non supporte retourne une erreur, pas un Document vide
- Confondre `extractHTMLFile` (docpipe, fichier local) avec `extract.Extract` (package extract, raw bytes)
