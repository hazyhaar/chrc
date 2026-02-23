#!/bin/bash
# Script de démarrage du worker d'indexation automatique des embeddings
# Ce worker scanne en continu la table chunks pour trouver ceux sans embeddings
# et crée des jobs dans la queue pour génération GPU via Hugot

set -e

# Configuration
export DB_PATH="${DB_PATH:-/inference/horos47/data/main.db}"

# Couleurs
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}Starting HOROS Embedding Indexer Worker${NC}"
echo "Database: $DB_PATH"
echo ""

# Vérifier que la DB existe
if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH"
    exit 1
fi

# Vérifier que le binaire existe
if [ ! -f "./bin/embedding_indexer" ]; then
    echo "Error: Binary not found. Run 'make build-indexer' first"
    exit 1
fi

echo -e "${GREEN}✓${NC} Starting worker..."
echo ""

# Lancer le worker
exec ./bin/embedding_indexer
