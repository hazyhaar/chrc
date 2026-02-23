#!/bin/bash
# Test rapide de l'endpoint /rag/embed

set -e

API_BASE="${API_BASE:-http://localhost:8443}"
TEXT="${1:-NVIDIA Blackwell GPU architecture}"

echo "Testing /rag/embed endpoint..."
echo "Text: $TEXT"
echo ""

RESPONSE=$(curl -s -X POST "$API_BASE/rag/embed" \
  -H "Content-Type: application/json" \
  -d "{\"text\": \"$TEXT\"}")

# Extraire info
DIMENSION=$(echo "$RESPONSE" | jq -r '.dimension')
MODEL=$(echo "$RESPONSE" | jq -r '.model')
EMBEDDING_SAMPLE=$(echo "$RESPONSE" | jq -r '.embedding[0:5]')

echo "✓ Embedding généré"
echo "  Model: $MODEL"
echo "  Dimension: $DIMENSION"
echo "  Sample (premiers 5 éléments): $EMBEDDING_SAMPLE"
echo ""

if [ "$DIMENSION" = "768" ]; then
  echo "✓ Test RÉUSSI - Dimension correcte (768)"
  exit 0
else
  echo "✗ Test ÉCHOUÉ - Dimension incorrecte: $DIMENSION (attendu: 768)"
  exit 1
fi
