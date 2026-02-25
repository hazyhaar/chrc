# catalog

Responsabilite: Collections de sources pre-definies et moteurs de recherche pour bootstrapper rapidement un espace veille.
Depend de: standard library uniquement (context, fmt)
Dependants: `cmd/chrc/` (endpoint `/api/catalog`), `veille/` (seeding initial)
Point d'entree: catalog.go
Types cles:
- `SourceDef` — definition d'une source (Name, URL, SourceType, ConfigJSON, Interval)
- `SourceInput` — structure d'insertion compatible veille.AddSource
- `SearchEngineDef` — definition d'un moteur de recherche (ID, Name, Strategy, URLTemplate, etc.)
- `SearchEngineInput` — structure d'insertion pour moteurs de recherche
Fonctions:
- `Categories()` — liste des noms de categories disponibles
- `Sources(category)` — definitions de sources pour une categorie
- `Populate(ctx, addSource, category)` — insere toutes les sources d'une categorie (skip duplicates)
- `PopulateSearchEngines(ctx, insertFn)` — insere les moteurs de recherche par defaut
Categories: `tech` (HN, Lobsters, Ars, Verge, TechCrunch, Go Blog), `legal-fr` (Legifrance, CNIL, EUR-Lex), `opendata` (data.gouv.fr, OpenAlex), `academic` (arXiv CS.AI, CS.CL), `news-fr` (Le Monde, Next INpact)
Moteurs: `brave_api` (enabled, API), `ddg_html` (stub, generic), `github_search` (enabled, API), `scholar` (stub, generic)
Invariants:
- Interval par defaut 3600000 ms (1h) si non specifie
- Skip silencieux des duplicates (UNIQUE constraint violations)
- Les moteurs `generic` (DDG, Scholar) sont disabled par defaut — necessitent domwatch
Tests: `catalog_test.go` — Populate insert/dedup, unknown category error, Categories non-vide, Sources non-vide, PopulateSearchEngines insert/dedup
NE PAS:
- Ajouter de sources avec des intervalles < 30min sans justification (rate limiting)
- Activer les moteurs `generic` sans avoir domwatch fonctionnel
- Importer de dependances externes (ce package doit rester stdlib-only)
