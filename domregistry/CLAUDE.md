# domregistry

Responsabilite: Registre communautaire de profils DOM partages — CRUD profils, corrections avec auto-accept, rapports de pannes, leaderboard, generation HTML statique.
Depend de: `github.com/hazyhaar/pkg/kit`, `github.com/hazyhaar/pkg/idgen`, `github.com/hazyhaar/pkg/connectivity`, `github.com/modelcontextprotocol/go-sdk/mcp`, `modernc.org/sqlite`
Dependants: `e2e/` (tests integration)
Point d'entree: `registry.go`
Types cles: `Registry` (orchestrateur), `Config` (DBPath, AutoAccept, DegradedThreshold), `Profile`, `Correction`, `Report`, `InstanceReputation`, `LeaderboardEntry`, `Stats`
Invariants:
- Flux Pull : domkeeper demarre un crawl -> interroge le registre -> importe le profil
- Flux Push : domkeeper auto-repair -> soumet correction au registre
- Flux Report : extracteur echoue -> domkeeper reporte -> registre ajuste success_rate
- AutoAccept : si active et reputation suffisante, la correction est auto-acceptee via `ScoreCorrection`
- DegradedThreshold par defaut = 0.5 (en dessous, profil marque degrade)
- RegisterMCP expose 6 tools (search_profiles, submit_correction, report_failure, leaderboard, stats, publish_profile)
- RegisterConnectivity expose 7 handlers
- `GenerateLeaderboardHTML` produit une page HTML statique complete
NE PAS:
- Confondre `GetProfileByPattern` (exact match URL pattern) avec `SearchProfiles` (recherche par domaine)
- Oublier que les corrections non auto-acceptees restent en status "pending" jusqu'a review manuelle
- Modifier les types re-exportes dans `types.go` sans mettre a jour `internal/store`
