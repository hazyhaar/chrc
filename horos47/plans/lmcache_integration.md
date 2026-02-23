# LMCache Integration - Plan de Développement

## Objectif
Activer LMCache dans vLLM pour offloading KV cache vers CPU/NVMe, permettant contextes quasi-illimités.

## Architecture
- LMCache = couche entre vLLM et matériel
- 3 niveaux : VRAM GPU → RAM CPU → NVMe
- Transferts asynchrones pour masquer latence

## Implémentation
### Fichiers
- `configs/vllm_lmcache_vision.yaml` : Config vLLM Vision avec LMCache
- `configs/vllm_lmcache_think.yaml` : Config vLLM Think avec LMCache
- `scripts/benchmark_lmcache.sh` : Mesurer impact latence/capacité

### Paramètres vLLM clés
```yaml
--enable-lm-cache
--lm-cache-size-gpu=16GB     # VRAM dédiée au cache
--lm-cache-size-cpu=64GB     # RAM CPU pour overflow
--lm-cache-nvme-path=/nvme/lmcache
```

## Décision Architecture
**PAS dans GPU Feeder** : Le Feeder reste aveugle, il passe juste ces paramètres à `docker run`.

**Fichiers de config** : Profils YAML avec paramètres testés pour différents scénarios.

## Tests
- Benchmark latence avec contextes 4k, 16k, 64k tokens
- Mesurer hit rate cache GPU vs CPU vs NVMe
- Valider que context > 32k fonctionne sans OOM

## Livrables
- 2 fichiers YAML de config
- 1 script benchmark
- Documentation trade-offs latence/capacité
