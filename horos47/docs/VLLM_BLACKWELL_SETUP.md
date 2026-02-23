# vLLM sur RTX 5090 Blackwell - Guide d'utilisation

## Image Docker

**Image** : `vllm-blackwell:v0.6.6-sm120`
**Taille** : 41.3 GB
**Base** : NVIDIA PyTorch NGC 25.02-py3
**Version vLLM** : v0.6.6 (stable)
**Architecture CUDA** : sm_120 (Blackwell)

## Configuration matérielle validée

- **GPU** : NVIDIA GeForce RTX 5090 (32 GB VRAM)
- **Driver** : 580.126.09
- **CUDA** : 12.8.93
- **Compute Capability** : 12.0 (sm_120)

## Optimisations Blackwell activées

- ✓ SCALED_MM_SM120 (matmul optimisé)
- ✓ NVFP4_SM120 (quantification FP4 native)
- ✓ CUTLASS_MOE_SM120 (Mixture of Experts)
- ✓ Flash Attention v2 (FA3 instable sur Blackwell)

## Lancement du container

### Méthode 1 : API Python directe (recommandé pour HOROS)

```bash
docker run -d \
  --name vllm-inference \
  --gpus all \
  --ipc=host \
  --ulimit memlock=-1 \
  --ulimit stack=67108864 \
  -v ~/.cache/huggingface:/root/.cache/huggingface \
  -p 8001:8001 \
  vllm-blackwell:v0.6.6-sm120 \
  sleep infinity
```

### Méthode 2 : Serveur OpenAI API (bugs d'import dans v0.6.6)

⚠️ **Non recommandé** - L'API serveur OpenAI a des problèmes d'imports circulaires dans v0.6.6.

## Utilisation depuis Python

```python
import sys
sys.path.insert(0, '/workspace/vllm')

from vllm import LLM, SamplingParams

# Charger le modèle
llm = LLM(
    model="microsoft/Phi-3-mini-4k-instruct",
    max_model_len=4096,
    gpu_memory_utilization=0.85,
    tensor_parallel_size=1,
    trust_remote_code=True
)

# Générer
prompts = ["Explique ce qu'est l'architecture Blackwell"]
sampling_params = SamplingParams(
    temperature=0.7,
    top_p=0.95,
    max_tokens=256
)

outputs = llm.generate(prompts, sampling_params)
for output in outputs:
    print(output.outputs[0].text)
```

## Modèles recommandés pour RTX 5090 (32 GB)

### Sans quantification (FP16/BF16)

| Modèle | VRAM | Contexte max | Débit estimé |
|--------|------|--------------|--------------|
| Phi-3-mini-4k | ~8 GB | 16k | ~200 tok/s |
| Qwen-2.5-7B | ~14 GB | 32k | ~130 tok/s |
| Llama-3-8B | ~16 GB | 8k | ~210 tok/s |
| Qwen-2.5-14B | ~28 GB | 16k | ~80 tok/s |

### Avec quantification AWQ 4-bit

| Modèle | VRAM | Contexte max | Débit estimé |
|--------|------|--------------|--------------|
| Qwen-2.5-32B-AWQ | ~20 GB | 16k | ~65 tok/s |
| Llama-3.1-70B-AWQ | ~40 GB | 8k | ~30 tok/s (nécessite 2x RTX 5090) |

## Paramètres optimaux

### Pour modèles ≤ 7B
```python
max_model_len=16384
gpu_memory_utilization=0.90
```

### Pour modèles 14B
```python
max_model_len=8192
gpu_memory_utilization=0.92
```

### Pour modèles 32B+ (quantifiés)
```python
max_model_len=4096
gpu_memory_utilization=0.92
quantization="awq"
```

## Intégration avec HOROS 47

Le service Think de HOROS utilise vLLM via un client Go qui communique avec le container Docker.

Voir `/inference/horos47/services/think/vllm_client.go` pour l'implémentation.

## Dépannage

### Import vLLM échoue
```bash
# Vérifier que sys.path inclut /workspace/vllm
docker exec vllm-inference python3 -c "import sys; sys.path.insert(0, '/workspace/vllm'); import vllm; print('OK')"
```

### GPU non détecté
```bash
# Vérifier accès GPU
docker exec vllm-inference python3 -c "import torch; print(torch.cuda.is_available())"
```

### OOM (Out of Memory)
- Réduire `max_model_len`
- Réduire `gpu_memory_utilization` à 0.8
- Utiliser un modèle plus petit ou quantifié

## Performances attendues (Blackwell vs Ada Lovelace)

- **Débit** : Jusqu'à **4x plus rapide** pour certains modèles
- **Latence TTFT** (Time To First Token) : **-30%** grâce à GDDR7
- **Efficacité énergétique** : **+40%** par token généré

## Build depuis sources (si modification nécessaire)

```bash
# Variables d'environnement critiques
export TORCH_CUDA_ARCH_LIST="12.0"
export VLLM_FLASH_ATTN_VERSION=2
export MAX_JOBS=6
export CUDA_HOME=/usr/local/cuda

# Compilation
cd /workspace/vllm
pip install --no-build-isolation -e .
```

**Durée** : ~30-40 minutes avec MAX_JOBS=6

## Références

- [vLLM Documentation](https://docs.vllm.ai/)
- [NVIDIA Blackwell Architecture](https://www.nvidia.com/en-us/data-center/technologies/blackwell-architecture/)
- [HOROS 47 Documentation](/inference/horos47/README.md)
