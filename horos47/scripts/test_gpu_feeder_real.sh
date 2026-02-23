#!/bin/bash
set -e

echo "🧪 GPU Feeder Test - Fixture Réelle (440 Images PNG)"
echo "======================================================"

# Fixture réelle
FIXTURE_DIR="/inference/INGEST_PROCESSING/019c244c-b684-7a2d-a6e4-ed0bb2930282/pdf_to_images"
TEST_DIR="/tmp/test_gpu_feeder_$(date +%s)"

# Vérifier fixture existe
if [ ! -d "$FIXTURE_DIR" ]; then
    echo "❌ Fixture not found: $FIXTURE_DIR"
    exit 1
fi

# Compter images dans fixture
IMAGE_COUNT=$(ls -1 "$FIXTURE_DIR"/*.png 2>/dev/null | wc -l)
echo "✓ Fixture contains $IMAGE_COUNT images"

# Créer structure test
echo "→ Creating test directory: $TEST_DIR"
mkdir -p "$TEST_DIR/stage_1_ocr"
mkdir -p "$TEST_DIR/stage_5_think/pending"

# Symlink vers fixture (pas de copie)
echo "→ Symlinking fixture to test OCR pending directory..."
ln -s "$FIXTURE_DIR" "$TEST_DIR/stage_1_ocr/pending"

# Vérifier symlink
LINKED_COUNT=$(ls -1 "$TEST_DIR/stage_1_ocr/pending"/*.png 2>/dev/null | wc -l)
echo "✓ Symlink created: $LINKED_COUNT images accessible"

# Lancer worker
echo ""
echo "🚀 Launching GPU Feeder worker..."
echo "   Data directory: $TEST_DIR"
echo "   Expected behavior:"
echo "   - Detect $IMAGE_COUNT OCR images (threshold=50)"
echo "   - Transition IDLE → VISION (~5-10 seconds)"
echo "   - Start vLLM Vision container (port 8001)"
echo ""
echo "Press Ctrl+C to stop worker"
echo "---"

# Lancer avec logs JSON
cd /inference/horos47
./bin/gpu_feeder_test -datadir "$TEST_DIR"

# Cleanup (si interrompu)
trap "echo ''; echo '🧹 Cleaning up...'; docker rm -f vllm-vision vllm-think 2>/dev/null || true; rm -rf '$TEST_DIR'; echo '✓ Cleanup done'" EXIT
