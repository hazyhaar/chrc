# Handlers de Coordination - Plan de Développement

## Objectif
Handlers génériques réutilisables pour patterns avancés (fan-in, branches conditionnelles, boucles).

## Handlers à Créer

### 1. `handler_fan_in.go`
Attend complétion de N jobs parents avant continuer.

```go
func HandlerFanIn(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
    batchID := payload["batch_id"].(string)
    expectedCount := payload["expected_count"].(int)

    // Count completed siblings
    var doneCount int
    db.QueryRow(`
        SELECT COUNT(*) FROM jobs
        WHERE parent_sha256 = ? AND status = 'done'
    `, batchID).Scan(&doneCount)

    if doneCount < expectedCount {
        return nil, fmt.Errorf("waiting for %d/%d jobs", doneCount, expectedCount)
    }

    // Aggregate results
    results := aggregateSiblingResults(db, batchID)

    // Continue workflow
    return results, nil
}
```

### 2. `handler_conditional_branch.go`
Lit condition dans payload et choisit prochaine étape.

```go
func HandlerConditionalBranch(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
    condition := payload["condition"].(string)
    branchesMap := payload["branches"].(map[string]interface{})

    nextChain := branchesMap[condition].([]string)

    // Override workflow chain
    payload["_workflow"].(map[string]interface{})["chain"] = nextChain

    // Create next job
    createNextJob(payload)
    return payload, nil
}
```

### 3. `handler_iterate_until.go`
Boucle avec condition d'arrêt.

```go
func HandlerIterateUntil(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
    iteration := payload["iteration"].(int)
    maxIterations := payload["max_iterations"].(int)

    result := doWork(payload)

    // Check stop condition
    if conditionMet(result) || iteration >= maxIterations {
        // Continue to next step in chain
        return result, nil
    }

    // Loop: create same job type again
    payload["iteration"] = iteration + 1
    createJobSameType(payload)

    return result, nil
}
```

### 4. `handler_batch_aggregator.go`
Collecte résultats de jobs parallèles et produit résultat consolidé.

```go
func HandlerBatchAggregator(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
    batchID := payload["batch_id"].(string)

    var results []map[string]interface{}
    rows := db.Query(`
        SELECT result FROM jobs
        WHERE parent_sha256 = ? AND status = 'done'
        ORDER BY created_at
    `, batchID)

    for rows.Next() {
        var resultJSON string
        rows.Scan(&resultJSON)
        var result map[string]interface{}
        json.Unmarshal([]byte(resultJSON), &result)
        results = append(results, result)
    }

    consolidated := consolidate(results)
    return consolidated, nil
}
```

## Fichiers
```
core/handlers/
├── fan_in.go
├── conditional_branch.go
├── iterate_until.go
└── batch_aggregator.go
```

## Enregistrement (main.go)
```go
worker.RegisterHandler("fan_in", handlers.HandlerFanIn)
worker.RegisterHandler("conditional_branch", handlers.HandlerConditionalBranch)
worker.RegisterHandler("iterate_until", handlers.HandlerIterateUntil)
worker.RegisterHandler("batch_aggregator", handlers.HandlerBatchAggregator)
```

## Exemples Workflows
### Fan-Out/Fan-In
```json
{
  "steps_chain": [
    "pdf_to_images",      // Crée N jobs image_to_ocr
    "fan_in",             // Attend tous les OCR
    "consolidate_text"
  ]
}
```

### Branche Conditionnelle
```json
{
  "steps_chain": [
    "image_to_ocr",
    "conditional_branch",  // Si confidence < 0.8 → ocr_heavy, sinon → store
    "store_result"
  ]
}
```

### Boucle Itérative
```json
{
  "steps_chain": [
    "query_to_search",
    "iterate_until",      // Boucle jusqu'à pertinence OK (max 3 fois)
    "generate_response"
  ]
}
```

## Tests
- Test fan_in avec 10 jobs parallèles
- Test conditional_branch avec différentes conditions
- Test iterate_until avec max_iterations
- Valider que workflows complexes s'enchaînent correctement

## Livrables
- 4 handlers génériques
- Tests unitaires pour chaque pattern
- Documentation workflows exemples
