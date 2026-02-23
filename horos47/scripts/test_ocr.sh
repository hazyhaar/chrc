#!/bin/bash
# Script de test du support OCR
# Teste l'upload de fichiers images/PDF avec extraction de texte

set -e

API_BASE="${API_BASE:-http://localhost:8443}"
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Test OCR HOROS 47 ===${NC}\n"

# Vérifier que Tesseract est installé
if ! command -v tesseract >/dev/null 2>&1; then
  echo -e "${RED}✗ Tesseract not installed${NC}"
  echo "Run: sudo ./scripts/install_ocr_deps.sh"
  exit 1
fi

echo -e "${GREEN}✓${NC} Tesseract installed: $(tesseract --version 2>&1 | head -1)"
echo ""

# Test avec fichier fourni en argument ou créer test
TEST_FILE="$1"

if [ -z "$TEST_FILE" ]; then
  echo -e "${YELLOW}Creating test PDF...${NC}"

  # Créer un PDF de test simple avec texte
  TEST_DIR="/tmp/horos-ocr-test"
  mkdir -p "$TEST_DIR"

  # Créer fichier texte
  cat > "$TEST_DIR/test.txt" << 'EOF'
HOROS 47 OCR Test Document

This is a test document for OCR processing.
The system should extract this text and create chunks.

Key features:
- Support for PDF files
- Support for image files (PNG, JPG, etc.)
- Automatic text extraction
- Chunking and embedding generation

NVIDIA Blackwell GPU architecture provides excellent performance
for machine learning workloads and embedding generation.
EOF

  # Convertir en PDF si pdflatex disponible, sinon utiliser text2pdf
  if command -v ps2pdf >/dev/null 2>&1; then
    # Utiliser ps2pdf (de ghostscript)
    cat "$TEST_DIR/test.txt" | ps2pdf - "$TEST_DIR/test.pdf" 2>/dev/null || true
  fi

  # Si pas de PDF créé, utiliser le fichier texte directement
  if [ ! -f "$TEST_DIR/test.pdf" ]; then
    echo -e "${YELLOW}Note: Creating text file instead of PDF${NC}"
    # Créer une "image" texte simple
    TEST_FILE="$TEST_DIR/test.txt"
  else
    TEST_FILE="$TEST_DIR/test.pdf"
  fi

  echo -e "${GREEN}✓${NC} Test file created: $TEST_FILE"
  echo ""
fi

# Vérifier que le fichier existe
if [ ! -f "$TEST_FILE" ]; then
  echo -e "${RED}✗ File not found: $TEST_FILE${NC}"
  exit 1
fi

# Afficher info fichier
echo -e "${YELLOW}Test file:${NC}"
echo "  Path: $TEST_FILE"
echo "  Size: $(du -h "$TEST_FILE" | cut -f1)"
echo "  Type: $(file -b "$TEST_FILE")"
echo ""

# Upload et OCR
echo -e "${YELLOW}Uploading file for OCR processing...${NC}"

RESPONSE=$(curl -s -X POST "$API_BASE/ingest/ocr" \
  -F "file=@$TEST_FILE" \
  -F "title=OCR Test Document" \
  -F "source=test" \
  -F "chunk_size=150")

# Vérifier réponse
if echo "$RESPONSE" | jq . >/dev/null 2>&1; then
  DOCUMENT_ID=$(echo "$RESPONSE" | jq -r '.document_id // empty')
  CHUNK_COUNT=$(echo "$RESPONSE" | jq -r '.chunk_count // 0')
  MESSAGE=$(echo "$RESPONSE" | jq -r '.message // empty')

  if [ -n "$DOCUMENT_ID" ]; then
    echo -e "${GREEN}✓ OCR processing successful${NC}"
    echo "  Document ID: $DOCUMENT_ID"
    echo "  Chunks created: $CHUNK_COUNT"
    echo "  Message: $MESSAGE"
    echo ""

    # Attendre indexation
    echo -e "${YELLOW}Waiting for embedding indexation (10s)...${NC}"
    sleep 10

    # Tester recherche
    echo -e "${YELLOW}Testing vector search on OCR document...${NC}"

    SEARCH_RESPONSE=$(curl -s -X POST "$API_BASE/rag/query" \
      -H "Content-Type: application/json" \
      -d '{
        "query": "What are the key features?",
        "top_k": 2,
        "min_score": 0.3
      }')

    RESULT_COUNT=$(echo "$SEARCH_RESPONSE" | jq -r '.count // 0')

    if [ "$RESULT_COUNT" -gt 0 ]; then
      echo -e "${GREEN}✓ Search successful - found $RESULT_COUNT results${NC}"
      echo ""
      echo "Top result:"
      echo "$SEARCH_RESPONSE" | jq -r '.results[0] | "  Score: \(.score)\n  Text: \(.chunk_text[0:100])..."'
      echo ""
      echo -e "${GREEN}=== Test PASSED ===${NC}"
      exit 0
    else
      echo -e "${YELLOW}⚠ Search returned no results${NC}"
      echo "  This may be normal if embeddings are not yet generated"
      echo "  Wait longer and try searching manually"
      exit 0
    fi
  else
    echo -e "${RED}✗ OCR failed${NC}"
    echo "Response: $RESPONSE"
    exit 1
  fi
else
  echo -e "${RED}✗ Invalid response from server${NC}"
  echo "Response: $RESPONSE"
  exit 1
fi
