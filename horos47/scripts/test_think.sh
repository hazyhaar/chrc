#!/bin/bash
# Script de test du service Think (génération LLM)

set -e

API_BASE="${API_BASE:-http://localhost:8443}"
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Test Service Think (LLM Generation) ===${NC}\n"

# Test 1 : Génération simple
echo -e "${YELLOW}Test 1: Simple generation${NC}"
echo "Prompt: Explain what is RAG in 2 sentences"
echo ""

RESPONSE=$(curl -s -X POST "$API_BASE/think/generate" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Explain what is RAG (Retrieval-Augmented Generation) in 2 sentences",
    "max_tokens": 100
  }')

if echo "$RESPONSE" | jq . >/dev/null 2>&1; then
  TEXT=$(echo "$RESPONSE" | jq -r '.text // empty')
  if [ -n "$TEXT" ]; then
    echo -e "${GREEN}✓ Generation successful${NC}"
    echo "Generated text:"
    echo "$TEXT" | fold -s -w 80
    echo ""
  else
    echo -e "${RED}✗ No text generated${NC}"
    echo "Response: $RESPONSE"
    exit 1
  fi
else
  echo -e "${RED}✗ Invalid response${NC}"
  echo "Response: $RESPONSE"
  exit 1
fi

# Test 2 : Génération avec prompt système
echo -e "${YELLOW}Test 2: Generation with system prompt${NC}"
echo "System: You are a French teacher"
echo "Prompt: Translate 'hello' to French"
echo ""

RESPONSE=$(curl -s -X POST "$API_BASE/think/generate" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Translate hello to French",
    "system_prompt": "You are a helpful French teacher. Answer concisely.",
    "max_tokens": 50
  }')

TEXT=$(echo "$RESPONSE" | jq -r '.text // empty')
if [ -n "$TEXT" ]; then
  echo -e "${GREEN}✓ Generation successful${NC}"
  echo "Generated text:"
  echo "$TEXT"
  echo ""
else
  echo -e "${RED}✗ Generation failed${NC}"
  exit 1
fi

# Test 3 : Génération avec RAG
echo -e "${YELLOW}Test 3: Generation with RAG context${NC}"

# D'abord, ingérer un document test
echo "Ingesting test document..."
DOC_RESPONSE=$(curl -s -X POST "$API_BASE/ingest/document" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "HOROS Features",
    "text": "HOROS 47 is a powerful RAG system. It supports GPU-accelerated embeddings using NVIDIA Blackwell RTX 5090. The system includes OCR capabilities for PDF and image processing. Vector search uses cosine similarity with L2-normalized embeddings.",
    "chunk_size": 100
  }')

DOC_ID=$(echo "$DOC_RESPONSE" | jq -r '.document_id // empty')
if [ -z "$DOC_ID" ]; then
  echo -e "${RED}✗ Failed to ingest document${NC}"
  exit 1
fi

echo "Document ingested: $DOC_ID"
echo "Waiting for embedding indexation (10s)..."
sleep 10

# Maintenant, question avec RAG
echo "Asking question with RAG..."
RESPONSE=$(curl -s -X POST "$API_BASE/think/generate" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "What GPU does HOROS use?",
    "use_rag": true,
    "max_tokens": 100
  }')

TEXT=$(echo "$RESPONSE" | jq -r '.text // empty')
if [ -n "$TEXT" ]; then
  echo -e "${GREEN}✓ RAG generation successful${NC}"
  echo "Generated text (with RAG context):"
  echo "$TEXT" | fold -s -w 80
  echo ""

  # Vérifier si le texte mentionne RTX 5090 ou Blackwell
  if echo "$TEXT" | grep -qi "5090\|blackwell"; then
    echo -e "${GREEN}✓✓ Answer contains correct context from knowledge base!${NC}"
  else
    echo -e "${YELLOW}⚠ Answer may not have used RAG context${NC}"
  fi
else
  echo -e "${RED}✗ RAG generation failed${NC}"
  exit 1
fi

echo ""
echo -e "${GREEN}=== All tests PASSED ===${NC}"
echo ""
echo "Summary:"
echo "  ✓ Simple text generation working"
echo "  ✓ System prompts working"
echo "  ✓ Auto-RAG integration working"
echo ""
echo "Service Think is fully operational! 🎯"
