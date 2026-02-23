# HOROS Context - Plan de Développement

## Concept

**horos_context** = Définition nommée d'un workflow de production de contexte enrichi, réutilisable et composable.

C'est une "recette" qui transforme une requête simple en contexte structuré et augmenté, prêt pour consommation par Think ou autre handler.

---

## Exemples Cas d'Usage

### Contexte Simple : RAG Direct
```json
{
  "id": "context_rag_simple",
  "name": "Recherche Vectorielle Basique",
  "steps": [
    {"type": "rag_query", "params": {"top_k": 5, "min_score": 0.7}}
  ]
}
```
**Output** : 5 chunks pertinents.

---

### Contexte Enrichi : RAG + Synthèse
```json
{
  "id": "context_rag_synthesized",
  "steps": [
    {"type": "rag_query", "params": {"top_k": 20}},
    {"type": "think_summarize", "params": {"max_tokens": 500}}
  ]
}
```
**Output** : Synthèse condensée de 20 chunks en 500 tokens.

---

### Contexte Dynamique : Update Data + RAG
```json
{
  "id": "context_live_weather",
  "steps": [
    {"type": "api_call", "endpoint": "https://api.weather.com", "params": {"city": "{query}"}},
    {"type": "index_to_rag", "ttl": 3600},
    {"type": "rag_query", "params": {"keywords": "temperature forecast"}}
  ]
}
```
**Output** : Contexte météo actualisé depuis API, indexé temporairement, puis recherché.

---

### Contexte Itératif : Re-RAG avec Expansion
```json
{
  "id": "context_deep_research",
  "steps": [
    {"type": "rag_query", "params": {"top_k": 3}},
    {"type": "think_evaluate_relevance"},
    {"type": "conditional_branch", "if": "relevance < 0.8", "then": [
      {"type": "think_expand_query"},
      {"type": "rag_query", "params": {"top_k": 5}},
      {"type": "iterate_until", "max_iterations": 3}
    ]},
    {"type": "think_synthesize_all"}
  ]
}
```
**Output** : Contexte multi-passes avec raffinement itératif.

---

### Contexte Hybride : Web + Local + Synthesis
```json
{
  "id": "context_hybrid_research",
  "steps": [
    {"type": "parallel", "branches": [
      {"type": "rag_query", "source": "local", "top_k": 5},
      {"type": "api_call", "endpoint": "https://api.perplexity.ai/search"},
      {"type": "api_call", "endpoint": "https://en.wikipedia.org/api/rest_v1/"}
    ]},
    {"type": "think_merge_sources", "strategy": "weighted"},
    {"type": "think_deduplicate"},
    {"type": "think_format_context", "template": "markdown"}
  ]
}
```
**Output** : Contexte fusionné local + web avec déduplication.

---

## Architecture Technique

### Table `context_definitions`

```sql
CREATE TABLE context_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    steps TEXT NOT NULL,              -- JSON array des étapes
    output_format TEXT DEFAULT 'json', -- 'json', 'markdown', 'plain'
    cache_ttl INTEGER DEFAULT 0,      -- 0=pas de cache, N=cache Ns
    created_at INTEGER DEFAULT (unixepoch())
);
```

### Table `context_cache` (Optionnel)

```sql
CREATE TABLE context_cache (
    context_id TEXT,
    query_hash TEXT,                  -- SHA256 de la requête
    result TEXT,                      -- Contexte produit
    created_at INTEGER,
    expires_at INTEGER,
    PRIMARY KEY (context_id, query_hash)
);
```

---

## Handlers Spécialisés

### 1. `handler_context_builder.go`

Point d'entrée générique qui exécute une définition de contexte.

```go
func (s *Service) HandlerContextBuilder(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
    contextID := payload["context_id"].(string)
    query := payload["query"].(string)

    // Charger définition
    definition := loadContextDefinition(s.db, contextID)

    // Check cache
    if definition.CacheTTL > 0 {
        if cached := checkCache(contextID, query); cached != nil {
            return cached, nil
        }
    }

    // Exécuter steps
    contextData := map[string]interface{}{"query": query, "results": []interface{}{}}

    for _, step := range definition.Steps {
        result := executeContextStep(ctx, step, contextData)
        contextData = mergeResults(contextData, result)
    }

    // Format output
    formattedContext := formatContext(contextData, definition.OutputFormat)

    // Cache si TTL
    if definition.CacheTTL > 0 {
        cacheContext(contextID, query, formattedContext, definition.CacheTTL)
    }

    return map[string]interface{}{
        "context": formattedContext,
        "metadata": map[string]interface{}{
            "context_id": contextID,
            "steps_executed": len(definition.Steps),
            "cached": false,
        },
    }, nil
}
```

---

### 2. `handler_context_rag_query.go`

Step type "rag_query" pour recherche vectorielle.

```go
func (s *Service) HandlerContextRAGQuery(ctx context.Context, stepParams map[string]interface{}, contextData map[string]interface{}) (map[string]interface{}, error) {
    query := contextData["query"].(string)
    topK := stepParams["top_k"].(int)
    minScore := stepParams["min_score"].(float64)

    // Recherche vectorielle
    chunks := s.ragService.VectorSearch(query, topK, minScore)

    return map[string]interface{}{
        "rag_results": chunks,
    }, nil
}
```

---

### 3. `handler_context_api_call.go`

Step type "api_call" pour requêtes API externes.

```go
func (s *Service) HandlerContextAPICall(ctx context.Context, stepParams map[string]interface{}, contextData map[string]interface{}) (map[string]interface{}, error) {
    endpoint := stepParams["endpoint"].(string)
    params := stepParams["params"].(map[string]interface{})

    // Interpoler variables depuis contextData
    endpoint = interpolateVars(endpoint, contextData)

    // HTTP call
    resp := httpClient.Get(endpoint, params)

    return map[string]interface{}{
        "api_response": resp.Data,
    }, nil
}
```

---

### 4. `handler_context_think_synthesize.go`

Step type "think_synthesize" pour condensation via LLM.

```go
func (s *Service) HandlerContextThinkSynthesize(ctx context.Context, stepParams map[string]interface{}, contextData map[string]interface{}) (map[string]interface{}, error) {
    maxTokens := stepParams["max_tokens"].(int)

    // Construire prompt de synthèse
    sources := gatherSources(contextData) // RAG results, API responses, etc.
    prompt := fmt.Sprintf("Synthesize the following information in %d tokens:\n\n%s", maxTokens, sources)

    // Appel Think
    summary := s.thinkService.Generate(prompt, maxTokens)

    return map[string]interface{}{
        "synthesis": summary,
    }, nil
}
```

---

### 5. `handler_context_index_to_rag.go`

Step type "index_to_rag" pour indexation temporaire de données fraîches.

```go
func (s *Service) HandlerContextIndexToRAG(ctx context.Context, stepParams map[string]interface{}, contextData map[string]interface{}) (map[string]interface{}, error) {
    ttl := stepParams["ttl"].(int) // Secondes

    // Extraire données à indexer (API response, etc.)
    dataToIndex := contextData["api_response"]

    // Créer chunk temporaire avec TTL
    chunkID := createTemporaryChunk(s.db, dataToIndex, ttl)

    // Générer embedding
    embedding := s.ragService.GenerateEmbedding(dataToIndex)

    // Stocker avec expiration
    storeTemporaryEmbedding(s.db, chunkID, embedding, time.Now().Unix() + int64(ttl))

    return map[string]interface{}{
        "indexed_chunk_id": chunkID,
    }, nil
}
```

---

### 6. `handler_context_parallel.go`

Step type "parallel" pour exécution parallèle de branches.

```go
func (s *Service) HandlerContextParallel(ctx context.Context, stepParams map[string]interface{}, contextData map[string]interface{}) (map[string]interface{}, error) {
    branches := stepParams["branches"].([]interface{})

    results := make(chan map[string]interface{}, len(branches))
    var wg sync.WaitGroup

    for _, branch := range branches {
        wg.Add(1)
        go func(b interface{}) {
            defer wg.Done()
            result := executeContextStep(ctx, b, contextData)
            results <- result
        }(branch)
    }

    wg.Wait()
    close(results)

    // Merge résultats
    merged := map[string]interface{}{}
    for result := range results {
        merged = deepMerge(merged, result)
    }

    return merged, nil
}
```

---

## Formats de Output

### JSON (Défaut)
```json
{
  "context": {
    "rag_results": [...],
    "api_response": {...},
    "synthesis": "..."
  },
  "metadata": {
    "context_id": "context_hybrid",
    "steps_executed": 5,
    "cached": false
  }
}
```

### Markdown
```markdown
# Context: Hybrid Research

## RAG Results
- Chunk 1: ...
- Chunk 2: ...

## External Data
Source: Wikipedia
...

## Synthesis
Based on local and external sources...
```

### Plain Text
```
RAG: [5 chunks found]
API: [Weather data retrieved]
Synthesis: Current temperature is 22°C...
```

---

## Enregistrement et Utilisation

### Enregistrement Handlers (main.go)

```go
// Context system
contextSvc := context.New(db, ragSvc, thinkSvc, logger)

worker.RegisterHandler("context_builder", contextSvc.HandlerContextBuilder)
worker.RegisterHandler("context_rag_query", contextSvc.HandlerContextRAGQuery)
worker.RegisterHandler("context_api_call", contextSvc.HandlerContextAPICall)
worker.RegisterHandler("context_think_synthesize", contextSvc.HandlerContextThinkSynthesize)
worker.RegisterHandler("context_index_to_rag", contextSvc.HandlerContextIndexToRAG)
worker.RegisterHandler("context_parallel", contextSvc.HandlerContextParallel)
```

### Appel depuis Think Workflow

```json
{
  "id": "workflow_think_with_context",
  "steps_chain": [
    "build_context",     // Construit contexte enrichi
    "generate_response"  // Utilise contexte pour génération
  ]
}
```

Job initial :
```json
{
  "type": "build_context",
  "payload": {
    "context_id": "context_hybrid_research",
    "query": "latest climate change data",
    "_workflow": {
      "chain": ["generate_response"]
    }
  }
}
```

---

## API HTTP/MCP

### Endpoint de Test Immédiat

```go
// POST /contexts/build
func (s *Service) HandleBuildContext(w http.ResponseWriter, r *http.Request) {
    var req struct {
        ContextID string `json:"context_id"`
        Query     string `json:"query"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    // Exécution synchrone pour test
    context := s.BuildContextSync(req.ContextID, req.Query)

    json.NewEncoder(w).Encode(context)
}
```

### Tool MCP

```go
tool := &mcp.Tool{
    Name: "build_context",
    Description: "Build enriched context using named context definition",
    InputSchema: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "context_id": map[string]string{"type": "string"},
            "query": map[string]string{"type": "string"},
        },
    },
}
```

---

## Extensions Avancées

### 1. Context Composition (Nested)

Un contexte peut appeler un autre contexte :

```json
{
  "id": "context_super",
  "steps": [
    {"type": "context_builder", "context_id": "context_rag_simple"},
    {"type": "context_builder", "context_id": "context_live_weather"},
    {"type": "think_merge"}
  ]
}
```

### 2. Conditional Steps

```json
{
  "type": "conditional",
  "condition": "rag_results.length < 3",
  "then": [
    {"type": "web_search"},
    {"type": "index_to_rag"}
  ],
  "else": [
    {"type": "think_synthesize"}
  ]
}
```

### 3. Variables et Templating

```json
{
  "steps": [
    {"type": "set_var", "name": "city", "value": "{query}"},
    {"type": "api_call", "endpoint": "https://api.weather.com/{city}"}
  ]
}
```

### 4. Context Chaining

Output d'un contexte devient input d'un autre :

```json
{
  "id": "context_chain",
  "steps": [
    {"type": "context_builder", "context_id": "context_extract_entities", "output_var": "entities"},
    {"type": "for_each", "var": "entities", "do": [
      {"type": "rag_query", "query": "{entity}"}
    ]}
  ]
}
```

---

## Fichiers à Créer

```
services/context/
├── service.go               # Service principal
├── builder.go               # HandlerContextBuilder
├── step_rag.go              # Step RAG query
├── step_api.go              # Step API call
├── step_think.go            # Step Think operations
├── step_index.go            # Step index to RAG
├── step_parallel.go         # Step parallel execution
├── step_conditional.go      # Step conditional branching
├── format.go                # Output formatters (JSON/MD/TXT)
├── cache.go                 # Context caching
└── interpolate.go           # Variable interpolation

migrations/
└── 008_context_tables.sql   # Tables context_definitions, context_cache

scripts/
└── seed_contexts.sql        # Contexts prédéfinis
```

---

## Livrables

1. **Service context complet** avec handlers step types
2. **Tables SQLite** (definitions + cache)
3. **API HTTP/MCP** pour build context
4. **5 contextes prédéfinis** (simple RAG, hybrid, iterative, etc.)
5. **Documentation usage** avec exemples
6. **Tests unitaires** par step type
7. **Benchmarks** cache vs rebuild

---

## Bénéfices Architecture

### Réutilisabilité
Définitions nommées réutilisables dans N workflows.

### Composabilité
Steps atomiques combinables infiniment.

### Extensibilité
Nouveaux step types ajoutables sans modifier core.

### Performance
Cache transparent avec TTL configurable.

### Observabilité
Chaque step tracé, durée mesurée, output inspecté.

### Testabilité
Chaque step type testable isolément.

---

## Cas d'Usage Métier

### Support Client
Context "customer_history" :
- RAG sur tickets précédents
- API CRM pour infos client
- Synthèse dernières interactions

### Recherche Juridique
Context "legal_research" :
- RAG local sur jurisprudence
- API Legifrance
- Think extraction citations
- Re-RAG sur citations trouvées

### Veille Technologique
Context "tech_watch" :
- RAG local documentation
- API GitHub trending
- API Hacker News
- Web scraping blogs spécialisés
- Synthèse tendances

### Analyse Financière
Context "financial_analysis" :
- RAG rapports annuels
- API Yahoo Finance données temps réel
- Think calculs ratios
- Synthèse recommandation

---

## Migration depuis Approche Actuelle

Actuellement, contexte = résultat direct d'une requête RAG.

Avec horos_context :
1. Créer définition `context_rag_current` équivalente
2. Remplacer appels directs RAG par `context_builder`
3. Enrichir progressivement avec steps additionnels

Rétrocompatibilité totale via contexte simple équivalent.

---

## Priorité

**MOYENNE-HAUTE** : Fondation pour contextes enrichis, requis pour agents avancés et RAG itératif.

Dépend de : reasoning_profiles, coordination_handlers.
Permet : Agents complexes, recherche multi-sources, contextes dynamiques.
