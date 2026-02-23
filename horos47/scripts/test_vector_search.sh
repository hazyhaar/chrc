#!/bin/bash
# Script de test de la recherche vectorielle
# Test end-to-end : ingestion → indexation → recherche

set -e

API_BASE="http://localhost:8443"
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=== Test Recherche Vectorielle HOROS 47 ===${NC}\n"

# 1. Ingérer un document de test
echo -e "${YELLOW}Step 1:${NC} Ingestion d'un document de test..."

INGEST_RESPONSE=$(curl -s -X POST "$API_BASE/ingest/document" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "NVIDIA Blackwell Architecture",
    "content": "The NVIDIA Blackwell architecture represents a revolutionary advancement in GPU technology. It features the RTX 5090 with 32GB of GDDR7 memory and advanced AI capabilities. The architecture introduces Programmatic Dependent Launch (PDL) for improved parallel processing. Blackwell GPUs are optimized for machine learning workloads and offer exceptional performance for embedding generation tasks.",
    "source": "test",
    "chunk_size": 100
  }')

DOCUMENT_ID=$(echo "$INGEST_RESPONSE" | jq -r '.document_id')
CHUNK_COUNT=$(echo "$INGEST_RESPONSE" | jq -r '.chunk_count')

echo -e "${GREEN}✓${NC} Document ingéré"
echo "  Document ID: $DOCUMENT_ID"
echo "  Chunks: $CHUNK_COUNT"
echo ""

# 2. Attendre que les embeddings soient générés
echo -e "${YELLOW}Step 2:${NC} Attente de la génération des embeddings (30s)..."
echo "  Note: Le worker d'indexation doit être actif"

sleep 30

echo -e "${GREEN}✓${NC} Délai écoulé"
echo ""

# 3. Effectuer une requête RAG
echo -e "${YELLOW}Step 3:${NC} Recherche vectorielle..."

QUERY_RESPONSE=$(curl -s -X POST "$API_BASE/rag/query" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What are the key features of Blackwell GPUs?",
    "top_k": 3,
    "min_score": 0.3
  }')

RESULT_COUNT=$(echo "$QUERY_RESPONSE" | jq -r '.count')

echo -e "${GREEN}✓${NC} Recherche terminée"
echo "  Résultats trouvés: $RESULT_COUNT"
echo ""

# 4. Afficher les résultats
echo -e "${YELLOW}Step 4:${NC} Résultats de la recherche:"
echo ""

echo "$QUERY_RESPONSE" | jq -r '.results[] | "  Score: \(.score | tonumber * 100 | round)%\n  Chunk: \(.chunk_text[0:120])...\n"'

# 5. Vérifier les scores
MIN_SCORE=$(echo "$QUERY_RESPONSE" | jq -r '.results[0].score // 0')

if (( $(echo "$MIN_SCORE > 0.5" | bc -l) )); then
  echo -e "${GREEN}✓ Test RÉUSSI${NC} - Score maximal: $(echo "$MIN_SCORE * 100" | bc -l | xargs printf "%.1f")%"
  exit 0
else
  echo -e "${YELLOW}⚠ Test PARTIEL${NC} - Score maximal: $(echo "$MIN_SCORE * 100" | bc -l | xargs printf "%.1f")%"
  echo "  Note: Scores faibles peuvent indiquer que les embeddings ne sont pas encore générés"
  exit 1
fi
