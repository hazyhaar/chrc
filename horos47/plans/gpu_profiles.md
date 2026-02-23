# GPU Profiles - Plan de Développement

## Objectif
Bibliothèque de profils de configuration vLLM testés pour différentes cartes GPU.

## Principe
Paramètres vLLM optimaux varient selon GPU (VRAM, architecture, CUDA compute capability).
Au lieu de hardcoder pour RTX 5090, fournir profils éditables.

## Structure
```
configs/gpu_profiles/
├── rtx_5090_vision.yaml
├── rtx_5090_think.yaml
├── rtx_4090_vision.yaml
├── a100_vision.yaml
└── README.md
```

### Contenu Profil
```yaml
gpu_model: "RTX 5090"
vram_total: 32GB
profile_vision:
  model: "Qwen/Qwen2-VL-7B-Instruct"
  gpu_memory_utilization: 0.95
  max_num_seqs: 32
  max_model_len: 16384
  dtype: bfloat16
  enable_chunked_prefill: true
profile_think:
  model: "microsoft/Phi-3-mini-4k-instruct"
  gpu_memory_utilization: 0.60
  max_num_seqs: 10
  max_model_len: 4096
  dtype: bfloat16
```

## Utilisation
GPU Feeder lit profil au démarrage :
```go
profile := loadGPUProfile("configs/gpu_profiles/rtx_5090_vision.yaml")
dockerArgs := buildDockerArgs(profile.ProfileVision)
```

## Scripts
### `scripts/benchmark_gpu_profile.sh`
Teste un profil et mesure :
- Throughput (tokens/s)
- Latence (ms)
- VRAM utilisée
- Taux erreur OOM

### `scripts/generate_gpu_profile.sh`
Tool interactif qui demande GPU, modèle, et génère profil optimisé via tests itératifs.

## Livrables
- Profils YAML pour RTX 5090, 4090, A100
- Scripts benchmark et génération
- README avec guide ajout nouveau GPU
