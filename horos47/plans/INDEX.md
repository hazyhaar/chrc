# INDEX GLOBAL - Plans HOROS 47

**Date** : 2026-02-04
**Version** : 1.0
**Statut** : Synthèse finale validée

---

## Vue d'Ensemble

Cet index référence l'ensemble des plans de développement HOROS 47 organisés par priorité (TIER 1/2/3) et intégrant :
- **8 recherches techniques** analysées et arbitrées
- **3 architectures de référence** validées
- **12 plans actifs** (4 TIER 1 + 5 TIER 2 + 3 TIER 3)
- **Syncrétisme complet** dans SYNTHESE_FINALE.md

---

## Structure Répertoire

```
/inference/horos47/plans/
├── INDEX.md                              # ← Ce fichier
├── SYNTHESE_FINALE.md                    # Syncrétisme complet (tout intégré)
│
├── research/                             # Recherches sources + arbitrages
│   ├── 01_optimisation_llm_golang_vllm.md
│   ├── 02_dpi_vllm_performances.md
│   ├── 03_batching_rtx5090.md
│   ├── 04_horos47_architecture_revised.md
│   ├── 05_gpu_memory_offloading.md
│   ├── 06_fiabilite_batch_sqlite.md
│   ├── 07_architecture_gpu_offloading_fr.md
│   ├── 08_isolation_persistance_rag.md
│   ├── 00_SYNTHESE_GLOBALE.md
│   ├── ARBITRAGES_ET_SYNTHESE_DISTILLEE.md
│   ├── ARBITRAGES_MISE_A_JOUR.md
│   └── CLARIFICATIONS_ARBITRAGES.md
│
├── architectures/                        # Architectures référence validées
│   ├── horos47_acid_workers.md          # Workers offline vLLM + filesystem queue
│   ├── context_flow_persistence.md      # 6 strates ACID/idempotentes
│   └── horos_context_orchestration.md   # 5 strates workflows orchestrés
│
├── TIER1_CRITIQUE/                       # Phase 1 (2 semaines)
│   ├── lmcache_integration.md           # KV cache offloading 3 niveaux
│   ├── vllm_observability_simple.md     # SQLite + JSONL (pas Prometheus)
│   ├── vision_watcher.md                # Trigger filesystem ingestion
│   └── context_flow_core.md             # Persistance ACID 6 strates
│
├── TIER2_IMPORTANT/                      # Phase 2 (1 semaine)
│   ├── horos_context_orchestration.md   # Workflows orchestrés intelligents
│   ├── horos_context.md                 # Contexte enrichi workflows (existant)
│   ├── gpu_profiles.md                  # AWQ + scheduler adaptatif
│   ├── reasoning_profiles.md            # LMCache fields + persistence
│   ├── context_pruning_patterns.md      # Sélection pertinence (≠ GC)
│   └── pkg_sqliteutil.md                # BEGIN IMMEDIATE patterns
│
└── TIER3_OPTIMISATION/                   # Phase 3+ (différé)
    ├── agent_loop_orchestration.md      # Agents conversationnels
    ├── vllm_sleep_mode.md               # Optimisation switching
    └── lmcache_storage_analysis.md      # Outils inspection/rejeu
```

---

## Plans TIER 1 - CRITIQUE (Phase 1 - 2 semaines)

### 1. lmcache_integration.md

**Objectif** : KV cache offloading 3 niveaux (GPU VRAM → CPU RAM → NVMe)

**Clarification majeure** : Stockage **analysable et rejouable** (pas juste performance)

**Livrables** :
- Configuration vLLM avec LMCache
- Métadonnées SQLite enrichies (workflow_id, thread_id, profil)
- CLI inspection : `horos-lmcache stats/inspect/replay`
- API rejeu workflow complet

**Dépendances** : NVMe storage, vLLM
**Consensus** : 4 sources (01, 05, 07, 08)

---

### 2. vllm_observability_simple.md ✅

**Objectif** : Observabilité maximale simple (SQLite + JSONL, pas Prometheus/Grafana)

**Philosophie** : Tout capturer, analyser après, préférer trop que pas assez

**Livrables** :
- Binaire `horos_vllm_logger` (2 goroutines : logs + metrics)
- stdout/stderr → JSONL rotatifs (`/data/vllm_logs/YYYY-MM-DD.jsonl`)
- Prometheus metrics → SQLite (`/data/vllm_metrics.db`)
- CLI : `horos-metrics stats --container vllm-vision --last 1h`

**Dépendances** : vLLM containers
**Statut** : ✅ Créé

---

### 3. vision_watcher.md

**Objectif** : Trigger filesystem pour ingestion automatique

**Pattern** : Polling `/data/horos/INGEST_IN/` toutes les 10s (simple, pas inotify)

**Livrables** :
- Binaire `horos_vision_watcher` (~100 lignes Go)
- Systemd service
- Détection types fichiers (PDF, images)
- Création jobs ACID Workers pipeline
- Archivage dans inbox/ par date

**Dépendances** : ACID Workers pipeline, filesystem structure

---

### 4. context_flow_core.md

**Objectif** : Persistance ACID 6 strates pour contexte infini

**Architecture référence** : context_flow_persistence.md

**Les 6 strates** :
1. **storage.go** : NVMe atomique CAS
2. **state.go** : SQLite ACID (BEGIN IMMEDIATE)
3. **context.go** : Event sourcing + snapshots
4. **cache.go** : Write-through KV cache tiering
5. **loop.go** : Thinking stateless (input_hash skip)
6. **recovery.go** : Récupération crash automatique

**Livrables** : 6 fichiers .go, intégration Think service, tests unitaires

**Dépendances** : SQLite, NVMe storage, vLLM

---

## Plans TIER 2 - IMPORTANT (Phase 2 - 1 semaine)

### 5. horos_context_orchestration.md

**Objectif** : Workflows orchestrés intelligents avec scheduling adaptatif GPU/CPU

**Architecture référence** : horos_context_orchestration.md (files (3))

**Les 5 strates** :
1. **context_recipe.go** : DAG workflows avec validation
2. **context_steps.go** : Steps primitifs (RAG, Think, API, Synthesis, etc.)
3. **context_executor.go** : Orchestration parallélisation automatique
4. **context_registry.go** : Index + métriques P95/P99
5. **context_scheduler.go** : Scheduling adaptatif selon ressources

**Innovation** : Pendant attente GPU (génération lourde), exécuter contextes CPU-only en background

**Livrables** : 5 fichiers .go, examples.go recettes prédéfinies, métriques temps réel

**Dépendances** : Context Flow, RAG service, vLLM

---

### 6. horos_context.md ✅

**Objectif** : Workflows data-driven définis en SQL (contexte enrichi)

**Pattern** : "Jobs as Library" avec ticket workflow qui voyage

**Livrables** :
- Table `context_definitions` avec steps_chain JSON
- Handlers pour types steps (RAG, API, Think, Synthesis)
- Support composition, conditionnels, parallèle
- Intégration avec Context Flow pour persistance

**Statut** : ✅ Créé (à enrichir avec scheduler adaptatif)

---

### 7. gpu_profiles.md

**Objectif** : Profils GPU dynamiques avec AWQ quantization + scheduler adaptatif

**Livrables** :
- Table `gpu_profiles` avec configs modèles
- Support AWQ int4 quantization (FP8 suivi futur)
- Scheduler policies (FCFS Vision, Priority Think)
- Intégration GPU Feeder state machine

**Dépendances** : GPU Feeder, vLLM
**Consensus** : Sources 01, 02, 03, 07

---

### 8. reasoning_profiles.md

**Objectif** : Profils reasoning avec LMCache persistent + cache persistence

**Livrables** :
- Table `reasoning_profiles` avec champs LMCache
- `lmcache_enabled`, `lmcache_priority`, `cache_persistence_days`
- Tiering hot/warm/cold automatique
- Réutilisation input_hash contexte (idempotence)

**Dépendances** : LMCache integration, Context Flow
**Consensus** : Sources 01, 05, 08

---

### 9. context_pruning_patterns.md

**Objectif** : Sélection intelligente pertinence contexte (≠ GC données)

**Clarification** : Context = livré au consommateur, GC données = service tiers (hors scope)

**Patterns** :
- Fenêtre glissante (buffers circulaires Go)
- Scoring sémantique (distance embeddings)
- Filtrage annotations (user feedback)
- Compression résumé (si contexte trop gros)

**Livrables** : Package pruning réutilisable, intégration HOROS Context Filter step

**Dépendances** : RAG service, embeddings

---

### 10. pkg_sqliteutil.md

**Objectif** : Package patterns SQLite obligatoires (BEGIN IMMEDIATE, batch ops, retry)

**Patterns** :
- `RunInTransaction()` avec BEGIN IMMEDIATE
- `BatchInsert()` générique (performance 2s → 200ms)
- `RetryOnBusy()` avec backoff exponentiel
- `ConfigureDB()` pragmas optimaux

**Livrables** : `/pkg/sqliteutil/` (5 fichiers Go), tests concurrence, doc complète

**Dépendances** : modernc.org/sqlite
**Référence** : CLARIFICATIONS_ARBITRAGES.md section 4

---

## Plans TIER 3 - OPTIMISATION (Phase 3+, différé)

### 11. agent_loop_orchestration.md

**Objectif** : Agents autonomes conversationnels persistants

**Livrables** :
- State machine agent (idle, thinking, executing, waiting)
- Sessions persistantes SQLite
- Boucle autonome (observation → décision → action)
- Intégration HOROS Context pour workflows

**Dépendances** : Context Flow, HOROS Context, Think service

---

### 12. vllm_sleep_mode.md

**Objectif** : Optimisation switching Vision ↔ Think (<1s vs 5-10s)

**Technique** : Suspend/resume GPU via vLLM profile switching

**Livrables** :
- API suspend/resume vLLM
- GPU Feeder intégration (alternative docker rm/run)
- Benchmarks latence switching

**Dépendances** : GPU Feeder, vLLM
**Consensus** : Sources 01, 07

---

### 13. lmcache_storage_analysis.md

**Objectif** : Outils inspection/rejeu caches pour debugging

**Livrables** :
- CLI inspection : `lmcache-tools inspect/validate/diff`
- Export formats (HDF5, Parquet)
- Replay workflow avec validation bit-perfect
- Visualisation tiering hot/warm/cold

**Dépendances** : LMCache integration, metadata SQLite

---

## Dépendances entre Plans (DAG)

```
TIER 1 (Fondation)
├── lmcache_integration ────────┬─────────────────────┐
│                               │                      │
├── vllm_observability_simple   │                      │
│                               │                      │
├── vision_watcher              │                      │
│                               │                      │
└── context_flow_core ──────────┼──────────────┐      │
                                │              │      │
TIER 2 (Optimisation)           │              │      │
├── horos_context_orchestration ┤              │      │
│   └─ dépend de: context_flow_core           │      │
│                                              │      │
├── horos_context ───────────────────────────┘       │
│                                                     │
├── gpu_profiles ────────────────────────────────────┤
│                                                     │
├── reasoning_profiles ──────────────────────────────┘
│   └─ dépend de: lmcache_integration
│
├── context_pruning_patterns
│   └─ dépend de: horos_context
│
└── pkg_sqliteutil (transversal, utilisé partout)

TIER 3 (Différé)
├── agent_loop_orchestration
│   └─ dépend de: context_flow_core, horos_context_orchestration
│
├── vllm_sleep_mode
│   └─ dépend de: gpu_profiles
│
└── lmcache_storage_analysis
    └─ dépend de: lmcache_integration
```

---

## Métriques Globales

### Couverture Architecturale

| Architecture Référence | Plans Couvrant |
|------------------------|----------------|
| HOROS47 ACID Workers | vision_watcher, pkg_sqliteutil |
| Context Flow 6 Strates | context_flow_core, reasoning_profiles |
| HOROS Context 5 Strates | horos_context_orchestration, horos_context, context_pruning_patterns |

### Clarifications Intégrées

| Clarification | Plans Impactés |
|---------------|----------------|
| LMCache analysable (pas juste perf) | lmcache_integration, lmcache_storage_analysis |
| Observabilité simple (SQLite + JSONL) | vllm_observability_simple |
| Context ≠ GC données | context_pruning_patterns |
| Long-running confirmé | context_flow_core, reasoning_profiles, agent_loop_orchestration |
| BEGIN IMMEDIATE obligatoire | pkg_sqliteutil (transversal) |

### Effort Total Estimé

| Phase | Plans | Durée | Lignes Code Estimées |
|-------|-------|-------|---------------------|
| TIER 1 | 4 | 2 semaines | ~3000 lignes Go |
| TIER 2 | 5 | 1 semaine | ~2500 lignes Go |
| TIER 3 | 3 | 2-3 semaines (différé) | ~2000 lignes Go |
| **Total** | **12** | **~5 semaines actif** | **~7500 lignes** |

---

## Utilisation Index

### Commencer Implémentation

1. **Lire SYNTHESE_FINALE.md** pour contexte complet
2. **Choisir plan TIER 1** à implémenter
3. **Lire architecture référence** associée si applicable
4. **Vérifier dépendances** dans DAG ci-dessus
5. **Suivre plan** : Objectif → Architecture → Implémentation → Tests → Livrables

### Ajouter Nouveau Plan

1. Déterminer TIER selon priorité/dépendances
2. Créer fichier dans répertoire TIER approprié
3. Suivre structure standard (voir plans existants)
4. Mettre à jour cet INDEX.md (section concernée + DAG)
5. Mettre à jour SYNTHESE_FINALE.md si impact architectural

### Marquer Plan Complété

Ajouter ✅ après nom plan dans sections ci-dessus et indiquer date complétion.

---

## Références Principales

- **SYNTHESE_FINALE.md** : Syncrétisme complet (LIRE EN PREMIER)
- **research/CLARIFICATIONS_ARBITRAGES.md** : Corrections majeures post-analyse
- **architectures/*.md** : 3 architectures de référence validées
- **HOROS47.md** : Spécification globale architecture "Jobs as Library"
- **CLAUDE.md** : Instructions projet, patterns, conventions

---

## Changelog

**2026-02-04 v1.0** : Création index initial, 12 plans actifs, syncrétisme complet validé

---

**Navigation rapide** :
- [SYNTHESE_FINALE.md](./SYNTHESE_FINALE.md) - Lire en premier pour contexte complet
- [TIER1_CRITIQUE/](./TIER1_CRITIQUE/) - Plans Phase 1 (2 semaines)
- [TIER2_IMPORTANT/](./TIER2_IMPORTANT/) - Plans Phase 2 (1 semaine)
- [TIER3_OPTIMISATION/](./TIER3_OPTIMISATION/) - Plans Phase 3+ (différé)
- [architectures/](./architectures/) - Architectures de référence
- [research/](./research/) - Recherches sources + arbitrages
