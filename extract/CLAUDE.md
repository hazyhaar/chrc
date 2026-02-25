# extract

Responsabilite: Extraction de contenu HTML avec dispatch multi-mode (CSS selectors, XPath, density analysis, auto).
Depend de: `golang.org/x/net/html`
Dependants: `domkeeper/internal/ingest`, `veille/internal/pipeline` (handlers web, rss, api, document, connectivity, question)
Point d'entree: `extract.go`
Types cles: `Result` (Text, HTML, Title, Hash), `Options` (Selectors, Mode, MinTextLen, TrustLevel)
Invariants:
- Mode "auto" essaie CSS/XPath d'abord, puis fallback density — jamais l'inverse
- `Hash` est toujours un SHA-256 hex du texte extrait
- Les noeuds boilerplate (nav, footer, sidebar, cookie, ads) sont toujours exclus en mode density
- Le MinTextLen par defaut est 50 caracteres
- `findContentByLandmarks` cherche d'abord `<main>` puis `<article>` dans cet ordre
NE PAS:
- Ajouter de dependance C (pure Go obligatoire, `golang.org/x/net/html` seulement)
- Modifier `boilerplatePatterns` sans valider sur des pages reelles
- Confondre `collectText` (texte visible) avec `collectCleanText` (texte sans boilerplate) — la seconde filtre les zones de navigation
