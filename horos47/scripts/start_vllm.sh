#!/bin/bash
# Script de démarrage vLLM pour HOROS 47
# Usage: ./scripts/start_vllm.sh [model_name] [port]

set -e

MODEL=${1:-"microsoft/Phi-3-mini-4k-instruct"}
PORT=${2:-8001}
CONTAINER_NAME="vllm-horos"

echo "🚀 Démarrage vLLM pour HOROS 47"
echo "Modèle: $MODEL"
echo "Port: $PORT"

# Arrêter container existant si présent
if docker ps -a | grep -q $CONTAINER_NAME; then
    echo "Arrêt du container existant..."
    docker rm -f $CONTAINER_NAME 2>/dev/null || true
fi

# Lancer le container
echo "Lancement du container vLLM..."
docker run -d \
  --name $CONTAINER_NAME \
  --gpus all \
  --ipc=host \
  --ulimit memlock=-1 \
  --ulimit stack=67108864 \
  -v ~/.cache/huggingface:/root/.cache/huggingface \
  -p $PORT:$PORT \
  --restart unless-stopped \
  vllm-blackwell:v0.6.6-sm120 \
  sleep infinity

echo "✓ Container démarré"

# Attendre que le container soit prêt
sleep 2

# Créer le script Python de service vLLM
echo "Configuration du service vLLM..."
docker exec $CONTAINER_NAME bash -c "cat > /workspace/vllm_service.py << 'PYEOF'
import sys
sys.path.insert(0, '/workspace/vllm')

from vllm import LLM, SamplingParams
from flask import Flask, request, jsonify
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = Flask(__name__)

# Charger le modèle au démarrage
logger.info(f'Chargement du modèle: $MODEL')
llm = LLM(
    model='$MODEL',
    max_model_len=4096,
    gpu_memory_utilization=0.85,
    tensor_parallel_size=1,
    trust_remote_code=True
)
logger.info('✓ Modèle chargé')

@app.route('/health', methods=['GET'])
def health():
    return jsonify({'status': 'ok', 'model': '$MODEL'})

@app.route('/generate', methods=['POST'])
def generate():
    try:
        data = request.json
        prompts = data.get('prompts', [])

        if not prompts:
            return jsonify({'error': 'No prompts provided'}), 400

        sampling_params = SamplingParams(
            temperature=data.get('temperature', 0.7),
            top_p=data.get('top_p', 0.95),
            max_tokens=data.get('max_tokens', 256)
        )

        outputs = llm.generate(prompts, sampling_params)

        results = []
        for output in outputs:
            results.append({
                'prompt': output.prompt,
                'text': output.outputs[0].text,
                'tokens': len(output.outputs[0].token_ids)
            })

        return jsonify({'results': results})

    except Exception as e:
        logger.error(f'Error: {e}')
        return jsonify({'error': str(e)}), 500

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=$PORT)
PYEOF
"

echo "✓ Service configuré"

# Démarrer le service vLLM
echo "Démarrage du service vLLM (cela peut prendre 1-2 minutes)..."
docker exec -d $CONTAINER_NAME bash -c "cd /workspace && pip install -q flask && python3 vllm_service.py > /workspace/vllm_service.log 2>&1"

echo ""
echo "✅ vLLM démarré avec succès !"
echo ""
echo "Service disponible sur: http://localhost:$PORT"
echo ""
echo "Test de santé:"
echo "  curl http://localhost:$PORT/health"
echo ""
echo "Génération de texte:"
echo "  curl -X POST http://localhost:$PORT/generate -H 'Content-Type: application/json' -d '{\"prompts\": [\"Hello\"], \"max_tokens\": 50}'"
echo ""
echo "Logs du service:"
echo "  docker exec $CONTAINER_NAME tail -f /workspace/vllm_service.log"
echo ""
