package gpufeeder

import (
	"time"
)

// Config configuration GPU Feeder V3
type Config struct {
	// Database
	JobsDBPath string // Chemin vers jobs.db SQLite

	// Worker
	PollInterval    time.Duration // Intervalle polling jobs (défaut: 5s)
	BatchSize       int           // Nombre max jobs à claim par batch (défaut: 32)
	WorkerTimeout   time.Duration // Timeout traitement job (défaut: 120s)
	MaxConcurrency  int           // Max jobs en parallèle (défaut: 8)

	// vLLM Servers (URLs des serveurs persistants)
	VisionServerURL string // URL serveur Vision (défaut: http://localhost:8001)
	ThinkServerURL  string // URL serveur Think (défaut: http://localhost:8002)
	EmbedServerURL  string // URL serveur Embed (défaut: http://localhost:8003)

	// Staging directories
	DataDir string // Répertoire data racine (défaut: /data/horos)

	// Thermal
	BatchCooldown time.Duration // Pause between batches to let GPU cool (default: 10s)

	// Retry
	MaxAttempts int // 3

	// LMCache tiering
	LMCacheEnabled         bool   // Détecté au démarrage par DetectLMCache()
	LMCacheConfigPath      string // Chemin vers lmcache.yaml
	KVCacheMountPoint      string // Point de montage NVMe pour KV cache
	LMCacheDockerImage     string // Image Docker LMCache/vLLM
	NativeOffloadingSizeGB int    // Taille offloading natif vLLM (RAM) en Go
}

// DefaultConfig retourne configuration par défaut v3
func DefaultConfig() Config {
	return Config{
		JobsDBPath:      "/tmp/gpu_feeder_v3/jobs.db",
		PollInterval:    5 * time.Second,
		BatchSize:       32,
		WorkerTimeout:   120 * time.Second,
		MaxConcurrency:  8,
		VisionServerURL: "http://localhost:8001",
		ThinkServerURL:  "http://localhost:8002",
		EmbedServerURL:  "http://localhost:8003",
		DataDir:         "/data/horos",
		BatchCooldown:   10 * time.Second,
		MaxAttempts:     3,

		LMCacheEnabled:         false, // Détecté au démarrage par DetectLMCache()
		LMCacheConfigPath:      "/etc/horos/lmcache.yaml",
		KVCacheMountPoint:      "/kvcache",
		LMCacheDockerImage:     "lmcache/vllm-openai:build-latest",
		NativeOffloadingSizeGB: 48,
	}
}
