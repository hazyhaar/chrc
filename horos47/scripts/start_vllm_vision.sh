#!/bin/bash
# Script de lancement vLLM avec Qwen2-VL-7B-Instruct pour OCR Vision

set -e

MODEL_PATH="/models/qwen2-vl-7b-instruct"
CONTAINER_NAME="vllm-qwen2-vl"
PORT=8001

echo "=== Lancement vLLM Vision (Qwen2-VL-7B) ==="
echo "Model: $MODEL_PATH"
echo "Port: $PORT"
echo "Container: $CONTAINER_NAME"

# Arrêter container existant si présent
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "Arrêt du container existant..."
    docker rm -f $CONTAINER_NAME
fi

# Vérifier que le modèle est complet
echo "Vérification du modèle..."
if [ ! -f "/inference${MODEL_PATH}/config.json" ]; then
    echo "❌ Erreur: Modèle non trouvé dans /inference${MODEL_PATH}"
    exit 1
fi

SAFETENSORS_COUNT=$(ls /inference${MODEL_PATH}/*.safetensors 2>/dev/null | wc -l)
echo "Fichiers safetensors trouvés: $SAFETENSORS_COUNT/5"

if [ $SAFETENSORS_COUNT -lt 5 ]; then
    echo "⚠️  Avertissement: Tous les fichiers safetensors ne sont pas encore téléchargés"
    echo "    Le modèle pourrait ne pas fonctionner correctement"
fi

# Lancer container vLLM Vision
echo "Lancement du container avec allocateur dynamique PyTorch..."
docker run -d \
    --gpus all \
    -e PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True \
    --name $CONTAINER_NAME \
    --shm-size=16gb \
    --ipc=host \
    --ulimit memlock=-1 \
    --ulimit stack=67108864 \
    -p ${PORT}:8000 \
    -v /inference/models:/models \
    lmcache/vllm-openai:build-latest \
    --model $MODEL_PATH \
    --gpu-memory-utilization 0.75 \
    --max-num-seqs 8 \
    --max-model-len 16384

echo ""
echo "✓ Container lancé avec succès"
echo ""
echo "Attente du chargement du modèle (30-60s)..."
sleep 30

# Vérifier que le serveur est prêt
echo "Test de l'API..."
for i in {1..10}; do
    if curl -s http://localhost:${PORT}/health > /dev/null 2>&1; then
        echo "✓ API Vision prête sur http://localhost:${PORT}"

        # Afficher les logs récents
        echo ""
        echo "=== Derniers logs ==="
        docker logs $CONTAINER_NAME 2>&1 | tail -10

        echo ""
        echo "=== Pour tester le slave Vision ==="
        echo "VLLM_VISION_URL=http://localhost:${PORT} ./bin/horos_vision"
        echo ""
        echo "=== Pour voir les logs en temps réel ==="
        echo "docker logs -f $CONTAINER_NAME"
        exit 0
    fi
    echo "Attente démarrage API ($i/10)..."
    sleep 5
done

echo "❌ Erreur: L'API n'a pas démarré après 60 secondes"
echo "Logs du container:"
docker logs $CONTAINER_NAME 2>&1 | tail -30
exit 1
