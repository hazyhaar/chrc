#!/bin/bash
# Script d'installation des dépendances OCR pour HOROS 47

set -e

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=== Installation Dépendances OCR HOROS 47 ===${NC}\n"

# Vérifier si on est root
if [ "$EUID" -ne 0 ]; then
  echo -e "${YELLOW}Ce script doit être exécuté avec sudo${NC}"
  echo "Usage: sudo $0"
  exit 1
fi

# 1. Installer Tesseract OCR
echo -e "${YELLOW}Step 1:${NC} Installation de Tesseract OCR..."

apt-get update -qq
apt-get install -y tesseract-ocr tesseract-ocr-eng tesseract-ocr-fra

echo -e "${GREEN}✓${NC} Tesseract OCR installé"
tesseract --version | head -1
echo ""

# 2. Vérifier poppler-utils (pdftotext, pdftoppm)
echo -e "${YELLOW}Step 2:${NC} Vérification poppler-utils..."

if command -v pdftotext >/dev/null 2>&1; then
  echo -e "${GREEN}✓${NC} poppler-utils déjà installé"
else
  echo "Installation de poppler-utils..."
  apt-get install -y poppler-utils
  echo -e "${GREEN}✓${NC} poppler-utils installé"
fi
echo ""

# 3. Optionnel : pdfinfo pour métadonnées
echo -e "${YELLOW}Step 3:${NC} Installation pdfinfo..."

apt-get install -y poppler-utils

echo -e "${GREEN}✓${NC} pdfinfo installé"
echo ""

# 4. Vérifier installations
echo -e "${YELLOW}Step 4:${NC} Vérification des installations..."

echo "Tesseract: $(which tesseract)"
echo "  Version: $(tesseract --version 2>&1 | head -1)"
echo "  Langues disponibles: $(tesseract --list-langs 2>&1 | tail -n +2 | tr '\n' ' ')"
echo ""

echo "pdftotext: $(which pdftotext)"
echo "  Version: $(pdftotext -v 2>&1 | head -1)"
echo ""

echo "pdftoppm: $(which pdftoppm)"
echo "  Version: $(pdftoppm -v 2>&1 | head -1)"
echo ""

echo "pdfinfo: $(which pdfinfo)"
echo ""

# 5. Créer répertoire temporaire pour uploads
echo -e "${YELLOW}Step 5:${NC} Configuration répertoires..."

mkdir -p /tmp/horos-uploads
chmod 755 /tmp/horos-uploads

echo -e "${GREEN}✓${NC} Répertoire uploads créé: /tmp/horos-uploads"
echo ""

echo -e "${GREEN}=== Installation terminée avec succès ===${NC}"
echo ""
echo "Les dépendances OCR sont maintenant installées :"
echo "  ✓ Tesseract OCR (images)"
echo "  ✓ poppler-utils (PDFs)"
echo "  ✓ Langues: anglais, français"
echo ""
echo "Pour tester :"
echo "  ./scripts/test_ocr.sh"
