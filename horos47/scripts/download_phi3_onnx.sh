#!/bin/bash
# Script de téléchargement du modèle Phi-3-mini ONNX pour HOROS 47

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== Téléchargement Phi-3-mini ONNX ===${NC}\n"

MODEL_DIR="/inference/horos47/models/phi-3-mini-4k-onnx"
REPO="microsoft/Phi-3-mini-4k-instruct-onnx"

# Vérifier si huggingface-cli est installé
if ! command -v huggingface-cli >/dev/null 2>&1; then
  echo -e "${RED}✗ huggingface-cli not found${NC}"
  echo "Installation : pip install huggingface-hub[cli]"
  exit 1
fi

echo -e "${GREEN}✓${NC} huggingface-cli found"
echo ""

# Vérifier si le modèle existe déjà
if [ -d "$MODEL_DIR" ] && [ -f "$MODEL_DIR/model.onnx" ]; then
  echo -e "${YELLOW}Model already exists at $MODEL_DIR${NC}"
  read -p "Re-download? (y/N) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Skipping download"
    exit 0
  fi
  rm -rf "$MODEL_DIR"
fi

# Créer répertoire
mkdir -p "$MODEL_DIR"

# Télécharger modèle
echo -e "${YELLOW}Downloading Phi-3-mini ONNX (~2.4 GB)...${NC}"
echo "This may take 5-10 minutes depending on your connection"
echo ""

hf download "$REPO" \
  --local-dir "$MODEL_DIR"

# Vérifier téléchargement
if [ ! -d "$MODEL_DIR/cuda-int4-rtn-block-32" ]; then
  echo -e "${RED}✗ Download failed - cuda-int4-rtn-block-32 directory not found${NC}"
  ls -la "$MODEL_DIR"
  exit 1
fi

if [ ! -f "$MODEL_DIR/cuda-int4-rtn-block-32/model.onnx" ] && [ ! -f "$MODEL_DIR/cuda-int4-rtn-block-32/phi-3-mini-4k-instruct-int4-cpu.onnx" ]; then
  echo -e "${RED}✗ Download failed - model.onnx not found${NC}"
  ls -la "$MODEL_DIR/cuda-int4-rtn-block-32/"
  exit 1
fi

echo ""
echo -e "${GREEN}✓ Phi-3-mini ONNX downloaded successfully${NC}"
echo ""
echo "Model location: $MODEL_DIR"
echo "Model file: $(ls -lh "$MODEL_DIR/cuda-int4-rtn-block-32/model.onnx" | awk '{print $5}')"
echo ""
echo "Test with:"
echo "  ./scripts/test_llm_slave.sh"
