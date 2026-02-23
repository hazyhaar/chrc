#!/bin/bash
set -e

REPO_ROOT="/inference/horos47"

echo "🧹 HOROS 47 - ONNX Runtime Cleanup"
echo "===================================="

# 1. Librairies
echo "→ Removing ONNX libraries (292 MB)..."
rm -rf "$REPO_ROOT/libs/libonnxruntime*.so*"
rm -rf "$REPO_ROOT/libs/backup_genai_old/"
echo "  ✓ Libraries removed"

# 2. Bindings C
echo "→ Removing C bindings..."
rm -rf "$REPO_ROOT/core/genai/"
rm -rf "$REPO_ROOT/include/"
echo "  ✓ C bindings removed"

# 3. Binaires
echo "→ Removing CGO binaries..."
rm -f "$REPO_ROOT/bin/horos_hugot"
rm -f "$REPO_ROOT/bin/horos_llm"
echo "  ✓ Binaries removed"

# 4. Modèles ONNX
echo "→ Removing ONNX models (~3 GB)..."
rm -rf "$REPO_ROOT/models/bge-base-en-v1.5-onnx/"
rm -rf "$REPO_ROOT/models/phi-3-mini-4k-onnx/"
echo "  ✓ Models removed"

# 5. Code source slaves
echo "→ Removing slave source code..."
rm -rf "$REPO_ROOT/cmd/horos_hugot/"
rm -rf "$REPO_ROOT/cmd/horos_llm/"
echo "  ✓ Slave sources removed"

# 6. go.mod cleanup
echo "→ Cleaning Go dependencies..."
cd "$REPO_ROOT"
go mod edit -droprequire github.com/knights-analytics/hugot 2>/dev/null || true
go mod edit -droprequire github.com/knights-analytics/ortgenai 2>/dev/null || true
go mod tidy
echo "  ✓ go.mod cleaned"

# Résumé
echo ""
echo "✅ ONNX Runtime cleanup completed"
echo ""
echo "Freed space:"
du -sh "$REPO_ROOT/libs/" 2>/dev/null || echo "  libs/: 0 MB (deleted)"
du -sh "$REPO_ROOT/models/" 2>/dev/null | grep -v "^0"
echo ""
echo "Next steps:"
echo "  1. Update Makefile (remove CGO targets)"
echo "  2. Update services/rag/service.go (remove Hugot spawn)"
echo "  3. Update services/think/service.go (remove LLM spawn)"
echo "  4. Rebuild: make build-master"
