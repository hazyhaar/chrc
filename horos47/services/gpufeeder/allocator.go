package gpufeeder

import (
	"log/slog"
	"os"
	"os/exec"
)

// DetectLMCache vérifie la disponibilité de l'infrastructure LMCache.
// Retourne true si l'image Docker, le point de montage NVMe et le fichier
// de configuration sont tous présents. Avertit et retourne false sinon.
func DetectLMCache(cfg Config, logger *slog.Logger) bool {
	// Vérifier image Docker
	cmd := exec.Command("docker", "image", "inspect", cfg.LMCacheDockerImage)
	if err := cmd.Run(); err != nil {
		logger.Warn("LMCache Docker image not found, degraded mode",
			"image", cfg.LMCacheDockerImage)
		return false
	}

	// Vérifier point de montage NVMe
	info, err := os.Stat(cfg.KVCacheMountPoint)
	if err != nil || !info.IsDir() {
		logger.Warn("LMCache KV cache mount point not available, degraded mode",
			"path", cfg.KVCacheMountPoint)
		return false
	}

	// Vérifier fichier de configuration
	if _, err := os.Stat(cfg.LMCacheConfigPath); err != nil {
		logger.Warn("LMCache config file not found, degraded mode",
			"path", cfg.LMCacheConfigPath)
		return false
	}

	// Vérifier et appliquer kernel.numa_balancing=0 (réduit la latence
	// des transferts KV en empêchant la migration de pages NUMA)
	numaVal, err := os.ReadFile("/proc/sys/kernel/numa_balancing")
	if err == nil && len(numaVal) > 0 && numaVal[0] != '0' {
		applyCmd := exec.Command("sysctl", "-w", "kernel.numa_balancing=0")
		if err := applyCmd.Run(); err != nil {
			logger.Warn("Failed to disable NUMA balancing (needs root), performance may suffer",
				"error", err)
		} else {
			logger.Info("NUMA balancing disabled for LMCache performance")
		}
	}

	logger.Info("LMCache infrastructure detected, complete mode available")
	return true
}

// ResourceAllocator calcule stratégie d'allocation GPU optimale
type ResourceAllocator struct {
	logger          *slog.Logger
	lmCacheEnabled  bool
}

// NewResourceAllocator crée nouvel allocateur
func NewResourceAllocator(logger *slog.Logger, lmCacheEnabled bool) *ResourceAllocator {
	return &ResourceAllocator{
		logger:         logger,
		lmCacheEnabled: lmCacheEnabled,
	}
}

// ComputeStrategy calcule stratégie optimale selon charge de travail
// Implémente recommandations Gemini : max 32 seqs, résolution adaptative
func (a *ResourceAllocator) ComputeStrategy(workload WorkloadStats, gpuStats *GPUStats) AllocationStrategy {
	strategy := AllocationStrategy{}

	// Règle Gemini : Réserver minimum 6 GB si processus externes
	externalReserve := 0
	if workload.ExternalProcessVRAM > 0 {
		externalReserve = 6000 // 6 GB safety margin
		a.logger.Info("External GPU process detected, reserving VRAM",
			"external_vram_mb", workload.ExternalProcessVRAM,
			"reserve_mb", externalReserve)
	}

	availableVRAM := gpuStats.MemoryTotalMB - externalReserve

	// Priorité 1 : Think (génération utilisateur)
	// MAIS : ne pas sacrifier un gros backlog vision pour quelques think jobs
	// (1 orphan think job ne doit pas bloquer 700+ vision jobs pendant 180s de warmup)
	if workload.ThinkRequestsActive > 0 {
		// Si vision a un backlog bien plus gros, ne pas switcher
		if workload.VisionJobsPending > 0 &&
			workload.ThinkRequestsActive <= 3 &&
			workload.VisionJobsPending > workload.ThinkRequestsActive*10 {
			a.logger.Info("Think jobs present but vision backlog much larger, deferring think",
				"think_pending", workload.ThinkRequestsActive,
				"vision_pending", workload.VisionJobsPending)
			// Fall through to vision priority
		} else {
			strategy.ThinkEnabled = true
			strategy.ThinkMaxSeqs = 10 // Conservative pour latence

			if a.lmCacheEnabled {
				strategy.ThinkMemoryPct = 0.90
				strategy.ThinkMaxModelLen = 524288
				strategy.ThinkLMCacheMode = "complete"
				strategy.Reason = "Think prioritaire LMCache (KV offloadé VRAM→RAM→NVMe)"
			} else {
				strategy.ThinkMemoryPct = 0.60 // Gemini: 60% pour Think prioritaire
				strategy.ThinkMaxModelLen = 131072
				strategy.ThinkLMCacheMode = "degraded"
				strategy.Reason = "Think prioritaire dégradé (offloading natif vLLM)"
			}

			// Vision impossible simultanément (17.7 GB modèles + KV = OOM)
			strategy.VisionEnabled = false
			strategy.EmbeddingsEnabled = false

			a.logger.Info("Strategy: Think priority mode",
				"think_requests", workload.ThinkRequestsActive,
				"memory_pct", strategy.ThinkMemoryPct,
				"lmcache_mode", strategy.ThinkLMCacheMode,
				"max_model_len", strategy.ThinkMaxModelLen)

			return strategy
		}
	}

	// Priorité 2 : Vision OCR batch (si jobs pending)
	if workload.VisionJobsPending > 0 {
		strategy.VisionEnabled = true
		strategy.ThinkEnabled = false // Pas assez de VRAM pour les deux

		// Résolution adaptative Gemini :
		// - 150 DPI par défaut (1280px max dim)
		// - 300 DPI seulement si batch < 20 (risque OOM)
		if workload.VisionJobsPending > 50 {
			// Batch intensif : 150 DPI, max throughput
			strategy.VisionResolution = 150
			strategy.VisionMemoryPct = 0.95 // Gemini: aggressive pour batching
			strategy.VisionMaxSeqs = 32     // Gemini soft cap
			strategy.Reason = "Vision batch intensif (150 DPI, max throughput)"
		} else if workload.VisionJobsPending > 10 {
			// Batch modéré : 150 DPI standard
			strategy.VisionResolution = 150
			strategy.VisionMemoryPct = 0.90
			strategy.VisionMaxSeqs = 32
			strategy.Reason = "Vision batch modéré (150 DPI)"
		} else {
			// Peu de jobs : Qualité max 300 DPI possible
			strategy.VisionResolution = 300
			strategy.VisionMemoryPct = 0.95
			strategy.VisionMaxSeqs = 16 // Gemini: ~19 max théorique, 16 safe
			strategy.Reason = "Vision qualité max (300 DPI, batch réduit)"
		}

		// Embeddings peut coexister si VRAM disponible > 25 GB
		if availableVRAM > 25000 {
			strategy.EmbeddingsEnabled = true
			strategy.EmbeddingsMemoryPct = 0.30
			strategy.EmbeddingsMaxSeqs = 64
		}

		a.logger.Info("Strategy: Vision batch mode",
			"jobs_pending", workload.VisionJobsPending,
			"resolution_dpi", strategy.VisionResolution,
			"max_seqs", strategy.VisionMaxSeqs)

		return strategy
	}

	// Priorité 3 : Embeddings batch (si chunks sans embeddings)
	// Un seul modèle à la fois sur le GPU — Think stoppé pendant le batch embed
	if workload.EmbeddingsJobsPending > 0 {
		strategy.EmbeddingsEnabled = true
		strategy.EmbeddingsMemoryPct = 0.90
		strategy.EmbeddingsMaxSeqs = 128
		strategy.ThinkEnabled = false
		strategy.VisionEnabled = false
		strategy.Reason = "Embeddings batch (Think stoppé)"

		a.logger.Info("Strategy: Embeddings batch mode",
			"jobs_pending", workload.EmbeddingsJobsPending)

		return strategy
	}

	// Mode Idle : Think standby uniquement
	strategy.ThinkEnabled = true
	strategy.ThinkMaxSeqs = 2
	strategy.VisionEnabled = false
	strategy.EmbeddingsEnabled = false

	if a.lmCacheEnabled {
		strategy.ThinkMemoryPct = 0.90
		strategy.ThinkMaxModelLen = 524288
		strategy.ThinkLMCacheMode = "complete"
		strategy.Reason = "Idle LMCache (Think standby, KV offloadé)"
	} else {
		strategy.ThinkMemoryPct = 0.85
		strategy.ThinkMaxModelLen = 131072
		strategy.ThinkLMCacheMode = "degraded"
		strategy.Reason = "Idle dégradé (Think standby, offloading natif)"
	}

	a.logger.Info("Strategy: Idle mode (Think standby)",
		"lmcache_mode", strategy.ThinkLMCacheMode)

	return strategy
}

// ValidateStrategy vérifie cohérence stratégie vs budget VRAM
// Gemini : 13.28 GB budget KV disponible
func (a *ResourceAllocator) ValidateStrategy(strategy AllocationStrategy, gpuStats *GPUStats) bool {
	const (
		staticMB      = 16720 // Modèles + overhead
		activationsMB = 2000
	)

	kvBudgetMB := gpuStats.MemoryTotalMB - staticMB - activationsMB

	// Calcul consommation KV estimée selon stratégie
	estimatedKV := 0

	if strategy.VisionEnabled {
		// Gemini: 192 MB/req (150 DPI) ou 672 MB/req (300 DPI)
		kvPerReq := 192
		if strategy.VisionResolution == 300 {
			kvPerReq = 672
		}
		estimatedKV += strategy.VisionMaxSeqs * kvPerReq
	}

	if strategy.ThinkEnabled {
		// Mode "complete" : KV offloadé vers RAM/NVMe, pression GPU réduite
		// Mode "degraded" : KV en VRAM uniquement, budget classique
		kvPerReqThink := 512
		if strategy.ThinkLMCacheMode == "complete" {
			kvPerReqThink = 256
		}
		estimatedKV += strategy.ThinkMaxSeqs * kvPerReqThink
	}

	if strategy.EmbeddingsEnabled {
		// Embeddings : ~10 MB/req (léger)
		estimatedKV += strategy.EmbeddingsMaxSeqs * 10
	}

	valid := estimatedKV <= kvBudgetMB

	if !valid {
		a.logger.Error("Strategy validation FAILED",
			"estimated_kv_mb", estimatedKV,
			"budget_mb", kvBudgetMB,
			"overflow_mb", estimatedKV-kvBudgetMB)
	} else {
		margin := kvBudgetMB - estimatedKV
		a.logger.Info("Strategy validation OK",
			"estimated_kv_mb", estimatedKV,
			"budget_mb", kvBudgetMB,
			"margin_mb", margin)
	}

	return valid
}
