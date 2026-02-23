# Reasoning Profiles - Plan de Développement

## Objectif
Système de profils paramétrables pour comportements RAG/Think (température LLM, TopK, seuils similarité, etc.).

## Architecture
### Table SQLite `reasoning_profiles`
```sql
CREATE TABLE reasoning_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    config TEXT NOT NULL,  -- JSON des paramètres
    created_at INTEGER DEFAULT (unixepoch())
);
```

### Structure Config JSON
```json
{
  "rag": {
    "top_k": 5,
    "min_similarity": 0.7,
    "enable_reranking": false
  },
  "llm": {
    "temperature": 0.3,
    "max_tokens": 1024,
    "top_p": 0.9
  },
  "reasoning": {
    "enable_cot": false,
    "max_iterations": 1
  }
}
```

## Profils Prédéfinis
### `factual_lookup`
- TopK élevé (10), similarité stricte (0.8)
- Température basse (0.1) pour précision
- Pas de Chain-of-Thought

### `deep_reasoning`
- TopK faible (3), similarité lâche (0.6)
- Température moyenne (0.7)
- CoT activé, 3 itérations max

### `creative_synthesis`
- TopK faible (5), similarité lâche (0.5)
- Température haute (0.9)
- Encourager divergence

## Utilisation
### Dans workflow_definitions
```sql
INSERT INTO workflow_definitions (..., reasoning_profile_id) VALUES (
    'rag_query_factual',
    'Mon workflow',
    '["query_to_embed", "search_context", "generate_response"]',
    'factual_lookup'  -- Référence au profil
);
```

### Dans handlers
```go
profileID := payload["_workflow"].(map[string]interface{})["reasoning_profile"]
profile := loadReasoningProfile(s.db, profileID)
topK := profile.RAG.TopK
temperature := profile.LLM.Temperature
```

## Implémentation
### Fichiers
- `core/profiles/reasoning.go` : Load/validate profils
- `scripts/seed_reasoning_profiles.sql` : Profils prédéfinis
- `services/*/handler_*.go` : Lecture profils dans handlers

## Override au Niveau Job
Possibilité de surcharger paramètres individuels :
```json
{
  "_workflow": {
    "chain": [...],
    "reasoning_profile": "factual_lookup",
    "overrides": {
      "llm.temperature": 0.5
    }
  }
}
```

## Livrables
- Table + migrations
- 3 profils prédéfinis
- Code Go load/validate
- Documentation paramètres disponibles
