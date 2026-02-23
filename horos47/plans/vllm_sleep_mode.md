# vLLM Sleep Mode - Plan de Développement

## Objectif
Utiliser Sleep Mode vLLM pour switching modèle <1s au lieu de 5-10s (redémarrage conteneur).

## Principe
vLLM peut suspendre un modèle en VRAM sans libérer mémoire, puis le réveiller en ~300ms.

## Architecture
### Option 1 : Intégré GPU Feeder
GPU Feeder dialogue avec API vLLM pour envoyer commandes pause/wake.
**Problème** : Viole principe "Blind Orchestrator" (Feeder doit rester simple).

### Option 2 : Proxy vLLM Controller (RECOMMANDÉ)
- Nouveau binaire : `horos_vllm_controller`
- Proxy entre GPU Feeder et conteneurs vLLM
- GPU Feeder appelle API simple : `POST /start-vision`, `POST /start-think`
- Controller traduit en commandes vLLM appropriées (wake/sleep si possible, sinon restart)

## Implémentation
### Fichiers
- `cmd/horos_vllm_controller/main.go` : Binaire proxy
- `services/vllmproxy/` : Logique gestion état conteneurs

### API Controller
```
POST /start-vision   → Wake si endormi, sinon docker run
POST /start-think    → Wake si endormi, sinon docker run
POST /stop-vision    → Sleep au lieu de docker rm
POST /stop-think     → Sleep au lieu de docker rm
GET /status          → État actuel (running/sleeping/stopped)
```

## Décision Architecture
**Sous-projet optionnel** : GPU Feeder fonctionne sans (docker rm/run classique).
Controller est optimisation activable pour users avancés.

## Tests
- Mesurer latence transition avec/sans Sleep Mode
- Valider pas de fuite VRAM après N cycles sleep/wake
- Benchmarker throughput après wake vs cold start

## Livrables
- Binaire `horos_vllm_controller` (optionnel)
- API REST simple compatible avec interface GPU Feeder
- Documentation activation Sleep Mode
