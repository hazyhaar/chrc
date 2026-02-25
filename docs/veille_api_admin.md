# Administration de la veille par API

Guide operationnel pour administrer le service veille via son API REST. Toutes les commandes utilisent `curl` avec Basic Auth (nginx) + JWT (applicatif).

## Acces

| | Valeur |
|---|---|
| URL | `https://veille.docbusinessia.fr` |
| Basic Auth | user `veille`, password dans `.env` (`AUTH_PASSWORD`) |
| Admin | email `admin`, password `admin123!!!` |
| Port interne | 8085 |

## Authentification

Deux couches :
1. **Basic Auth** (nginx) — sur toutes les requetes
2. **JWT** (applicatif) — cookie `token`, 30 jours, obtenu via `/api/auth/login`

### Login

```bash
# Variables (a mettre en haut de script)
BASE="https://veille.docbusinessia.fr"
AUTH="veille:r1rZBKkRL7qdhM3g07ghrMtDgqWXYd"
COOKIES="/tmp/veille_cookies"

# Login → recupere le cookie JWT
curl -s -u "$AUTH" -c "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{"email":"admin","password":"admin123!!!"}' \
  "$BASE/api/auth/login" | python3 -m json.tool
```

Reponse : `{"id": "...", "name": "admin", "role": "admin"}`

### Verifier la session

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/auth/me" | python3 -m json.tool
```

### Logout

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X POST "$BASE/api/auth/logout"
```

## Espaces (spaces)

Un espace isole un ensemble de sources dans un shard SQLite separe. Quota : **50 espaces par utilisateur**.

### Lister les espaces

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/spaces" | python3 -m json.tool
```

Reponse : tableau de `{"space_id": "...", "name": "...", "created_at": ...}`

### Creer un espace

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{"name":"Mon espace"}' \
  "$BASE/api/spaces" | python3 -m json.tool
```

Reponse (201) : `{"space_id": "...", "name": "Mon espace", ...}`
Erreur (429) : quota depasse

### Supprimer un espace

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE "$BASE/api/spaces/$SPACE_ID"
```

## Sources

Chaque source est rattachee a un espace. Quota : **1000 sources par espace**. Les URLs sont normalisees et dedoublonnees automatiquement.

### Types de sources

| Type | Description |
|------|-------------|
| `rss` | Flux RSS/Atom (defaut) |
| `web` | Page web, extraction HTML |
| `api` | Endpoint JSON, dot-notation |
| `document` | Fichier local |
| `question` | Question trackee (auto-cree par AddQuestion) |
| `{custom}` | Types decouverts via ConnectivityBridge (`github`, etc.) |

### Lister les sources d'un espace

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/spaces/$SPACE_ID/sources" | python3 -m json.tool
```

### Ajouter une source

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Hacker News",
    "url": "https://news.ycombinator.com/rss",
    "source_type": "rss",
    "fetch_interval": 3600000
  }' \
  "$BASE/api/spaces/$SPACE_ID/sources" | python3 -m json.tool
```

**Valeurs par defaut** si omises :
- `source_type` → `"rss"`
- `fetch_interval` → `3600000` (1 heure)

**Validation appliquee** :
- `name` : non vide, max 512 caracteres
- `url` : non vide, max 4096 caracteres, schema http(s) requis
- `source_type` : doit etre un type connu (voir tableau ci-dessus)
- `fetch_interval` : entre 60000 (1 min) et 604800000 (7 jours) ms
- `config_json` : JSON valide, max 8192 octets (optionnel)

**Codes d'erreur** :
| Code | Signification |
|------|---------------|
| 201 | Source creee |
| 400 | Input invalide (nom, URL, type, intervalle) |
| 409 | URL dupliquee (meme URL deja presente dans l'espace) |
| 429 | Quota depasse (>1000 sources) |

### Modifier une source

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X PUT \
  -H "Content-Type: application/json" \
  -d '{"name": "HN RSS", "fetch_interval": 1800000}' \
  "$BASE/api/spaces/$SPACE_ID/sources/$SOURCE_ID" | python3 -m json.tool
```

Champs modifiables : `name`, `url`, `enabled` (bool), `fetch_interval`.

### Supprimer une source

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE \
  "$BASE/api/spaces/$SPACE_ID/sources/$SOURCE_ID"
```

### Fetch immediat

Declenche un fetch sans attendre le scheduler :

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X POST \
  "$BASE/api/spaces/$SPACE_ID/sources/$SOURCE_ID/fetch"
```

### Ajouter depuis le registre

Le registre (`source_registry`) contient des sources pre-configurees. Pour en ajouter une a un espace :

```bash
# Lister le registre
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/source-registry" | python3 -m json.tool

# Ajouter une source du registre dans un espace
curl -s -u "$AUTH" -b "$COOKIES" -X POST \
  "$BASE/api/spaces/$SPACE_ID/sources/from-registry/$REGISTRY_ID"
```

## Extractions et recherche

### Lister les extractions d'une source

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  "$BASE/api/spaces/$SPACE_ID/sources/$SOURCE_ID/extractions?limit=20" | python3 -m json.tool
```

### Historique de fetch

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  "$BASE/api/spaces/$SPACE_ID/sources/$SOURCE_ID/history?limit=10" | python3 -m json.tool
```

### Recherche FTS5

Recherche plein texte sur les extractions d'un espace :

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  "$BASE/api/spaces/$SPACE_ID/search?q=intelligence+artificielle&limit=20" | python3 -m json.tool
```

### Statistiques

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/spaces/$SPACE_ID/stats" | python3 -m json.tool
```

## Questions trackees

Les questions sont des recherches periodiques sur des moteurs de recherche.

### Ajouter une question

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "LLM inference optimization 2026",
    "keywords": "LLM inference optimization",
    "channels": "[\"brave_api\"]",
    "schedule_ms": 86400000,
    "max_results": 20,
    "follow_links": true
  }' \
  "$BASE/api/spaces/$SPACE_ID/questions" | python3 -m json.tool
```

### Lister les questions

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/spaces/$SPACE_ID/questions" | python3 -m json.tool
```

### Modifier une question

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X PUT \
  -H "Content-Type: application/json" \
  -d '{"text": "Updated question", "enabled": false}' \
  "$BASE/api/spaces/$SPACE_ID/questions/$QUESTION_ID" | python3 -m json.tool
```

### Supprimer une question

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE \
  "$BASE/api/spaces/$SPACE_ID/questions/$QUESTION_ID"
```

### Executer une question immediatement

```bash
curl -s -u "$AUTH" -b "$COOKIES" -X POST \
  "$BASE/api/spaces/$SPACE_ID/questions/$QUESTION_ID/run" | python3 -m json.tool
```

Reponse : `{"status": "ok", "new_results": 5}`

### Resultats d'une question

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  "$BASE/api/spaces/$SPACE_ID/questions/$QUESTION_ID/results?limit=50" | python3 -m json.tool
```

## Administration (admin only)

Les routes `/api/admin/*` requierent `role=admin`.

### Utilisateurs

```bash
# Lister
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/admin/users" | python3 -m json.tool

# Creer
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","name":"John","password":"secure123","role":"user"}' \
  "$BASE/api/admin/users" | python3 -m json.tool

# Supprimer
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE "$BASE/api/admin/users/$USER_ID"
```

### Moteurs de recherche globaux

```bash
# Lister
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/admin/engines" | python3 -m json.tool

# Creer
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Brave Search",
    "strategy": "api",
    "url_template": "https://api.search.brave.com/res/v1/web/search?q=${QUERY}",
    "api_config": "{\"headers\":{\"X-Subscription-Token\":\"${BRAVE_API_KEY}\"}}",
    "rate_limit_ms": 2000,
    "max_pages": 3,
    "enabled": true
  }' \
  "$BASE/api/admin/engines" | python3 -m json.tool

# Modifier
curl -s -u "$AUTH" -b "$COOKIES" -X PUT \
  -H "Content-Type: application/json" \
  -d '{"name":"Brave Search v2","enabled":true}' \
  "$BASE/api/admin/engines/$ENGINE_ID" | python3 -m json.tool

# Supprimer
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE "$BASE/api/admin/engines/$ENGINE_ID"
```

### Registre de sources

Le registre est un catalogue global de sources pre-configurees que les utilisateurs peuvent ajouter a leurs espaces.

```bash
# Lister
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/admin/source-registry" | python3 -m json.tool

# Creer
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Hacker News",
    "url": "https://news.ycombinator.com/rss",
    "source_type": "rss",
    "category": "tech",
    "description": "Hacker News front page RSS",
    "fetch_interval": 3600000,
    "enabled": true
  }' \
  "$BASE/api/admin/source-registry" | python3 -m json.tool

# Modifier
curl -s -u "$AUTH" -b "$COOKIES" -X PUT \
  -H "Content-Type: application/json" \
  -d '{"name":"HN RSS","fetch_interval":1800000}' \
  "$BASE/api/admin/source-registry/$ENTRY_ID" | python3 -m json.tool

# Supprimer
curl -s -u "$AUTH" -b "$COOKIES" -X DELETE "$BASE/api/admin/source-registry/$ENTRY_ID"
```

### Vue d'ensemble (overview)

Vue cross-tenant de tous les espaces et sources :

```bash
curl -s -u "$AUTH" -b "$COOKIES" "$BASE/api/admin/overview" | python3 -m json.tool
```

### Historique de recherche d'un espace

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  "$BASE/api/admin/overview/$USER_ID/$SPACE_ID/searches?limit=50" | python3 -m json.tool
```

### Promouvoir une recherche en question trackee

```bash
curl -s -u "$AUTH" -b "$COOKIES" \
  -H "Content-Type: application/json" \
  -d '{"query":"LLM trends","channels":["brave_api"],"schedule_ms":86400000}' \
  "$BASE/api/admin/overview/$USER_ID/$SPACE_ID/promote" | python3 -m json.tool
```

## Import OPML (Feedly, etc.)

Script Python pour import en masse. Cree 1 espace par categorie OPML.

```bash
python3 /tmp/import_feedly.py /path/to/subscriptions.opml
```

Le script :
1. Parse l'OPML (categories + feeds)
2. Login JWT
3. Cree les espaces manquants (reutilise les existants)
4. Ajoute chaque feed comme source `rss`
5. Gere les doublons (409), erreurs de validation (400), quotas (429)

## Health check

```bash
curl -s -u "$AUTH" "$BASE/health"
# {"status":"ok"}
```

## Normalisation des URLs

Les URLs sont automatiquement normalisees avant stockage :
- Schema et host en minuscules
- Fragment (`#...`) supprime
- Trailing slash supprime (sauf root `/`)
- Query params tries par cle

Cela evite les doublons `HTTPS://Example.COM/feed/` vs `https://example.com/feed`.

## Deploiement

```bash
cd "/data/HOROS SYSTEM DEV AREA"
./scripts/deploy/deploy.sh veille
```

Le script deploy compile, sauvegarde N-1, deploie, redemarre systemd, verifie le health check. Rollback automatique si echec.
