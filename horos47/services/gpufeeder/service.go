package gpufeeder

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Service gère orchestration GPU complète
type Service struct {
	db        *sql.DB // mainDB (for Vision jobs in table jobs)
	jobsDB    *sql.DB // gpu_jobs DB (for Think count)
	cfg       Config
	logger    *slog.Logger
	monitor   *GPUMonitor
	allocator *ResourceAllocator
	manager   *ProcessManager
	health    *HealthChecker

	currentStrategy AllocationStrategy
}

// New crée nouveau service GPU Feeder
func New(db *sql.DB, jobsDB *sql.DB, cfg Config, logger *slog.Logger) *Service {
	return &Service{
		db:        db,
		jobsDB:    jobsDB,
		cfg:       cfg,
		logger:    logger,
		monitor:   NewGPUMonitor(logger),
		allocator: NewResourceAllocator(logger, cfg.LMCacheEnabled),
		manager:   NewProcessManager(logger, cfg),
		health:    NewHealthChecker(logger),
	}
}

// Start démarre service GPU Feeder
func (s *Service) Start(ctx context.Context) error {
	s.logger.Info("GPU Feeder starting")

	// Lancer monitoring GPU continu
	go s.monitorLoop(ctx)

	// Lancer health checking continu
	go s.health.MonitorContinuous(ctx, s.manager, 10*time.Second)

	// Lancer boucle d'orchestration principale
	go s.orchestrationLoop(ctx)

	return nil
}

// monitorLoop boucle de monitoring GPU (1s interval Gemini)
func (s *Service) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := s.monitor.GetStats()
			if err != nil {
				s.logger.Error("Failed to get GPU stats", "error", err)
				continue
			}

			// Check thermal throttling (Gemini: >80°C warning)
			if stats.Temperature > 80 {
				s.logger.Warn("GPU thermal warning",
					"temp_c", stats.Temperature,
					"power_w", stats.PowerWatts)
			}

			// Check cache usage (Gemini: >90% critical)
			cacheUsage := s.monitor.GetCacheUsagePercent()
			if cacheUsage > 90.0 {
				s.logger.Warn("KV Cache usage critical",
					"usage_pct", cacheUsage,
					"action", "Consider reducing max_num_seqs")
			}
		}
	}
}

// orchestrationLoop boucle principale d'orchestration (5s interval)
func (s *Service) orchestrationLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.recomputeAllocation(ctx); err != nil {
				s.logger.Error("Failed to recompute allocation", "error", err)
			}
		}
	}
}

// recomputeAllocation recalcule et applique stratégie d'allocation
func (s *Service) recomputeAllocation(ctx context.Context) error {
	// 1. Récupérer stats GPU
	gpuStats, err := s.monitor.GetStats()
	if err != nil {
		return err
	}

	// 2. Détecter processus externes
	externalVRAM, err := s.monitor.DetectExternalProcesses()
	if err != nil {
		s.logger.Warn("Failed to detect external processes", "error", err)
		externalVRAM = 0
	}

	// 3. Récupérer charge de travail depuis DB
	workload, err := s.getWorkloadStats(ctx)
	if err != nil {
		return err
	}
	workload.ExternalProcessVRAM = externalVRAM

	// 4. Calculer stratégie optimale
	strategy := s.allocator.ComputeStrategy(workload, gpuStats)

	// 5. Valider cohérence VRAM
	if !s.allocator.ValidateStrategy(strategy, gpuStats) {
		s.logger.Error("Strategy validation failed, keeping current")
		return nil
	}

	// 6. Appliquer si changement
	if s.strategyChanged(strategy) {
		// Check if any model has processing jobs — don't kill containers mid-batch
		if s.hasProcessingJobs(ctx) {
			s.logger.Debug("Skipping strategy change, jobs in-flight")
			return nil
		}

		s.logger.Info("Strategy changed, applying",
			"reason", strategy.Reason,
			"vision_enabled", strategy.VisionEnabled,
			"think_enabled", strategy.ThinkEnabled)

		// Sauvegarder l'ancienne stratégie pour que applyStrategy puisse
		// comparer et décider quoi lancer/stopper
		previousStrategy := s.currentStrategy
		s.currentStrategy = strategy

		if err := s.applyStrategy(ctx, strategy, previousStrategy); err != nil {
			return err
		}
	}

	return nil
}

// getWorkloadStats récupère statistiques charge de travail depuis DB
func (s *Service) getWorkloadStats(ctx context.Context) (WorkloadStats, error) {
	var stats WorkloadStats

	// Count Vision jobs pending
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE type = 'image_to_ocr' AND status = 'pending'
	`).Scan(&stats.VisionJobsPending)
	if err != nil {
		return stats, err
	}

	// Count Think requests from gpu_jobs DB
	if s.jobsDB != nil {
		s.jobsDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM gpu_jobs
			WHERE model_type = 'think' AND status = 'pending'
		`).Scan(&stats.ThinkRequestsActive)
	}

	// Count Embeddings jobs pending
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM chunks
		WHERE chunk_id NOT IN (SELECT chunk_id FROM embeddings)
	`).Scan(&stats.EmbeddingsJobsPending)
	if err != nil {
		// Table might not exist yet
		stats.EmbeddingsJobsPending = 0
	}

	return stats, nil
}

// hasProcessingJobs checks if any gpu_jobs are currently being processed
func (s *Service) hasProcessingJobs(ctx context.Context) bool {
	if s.jobsDB == nil {
		return false
	}
	var count int
	err := s.jobsDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gpu_jobs WHERE status = 'processing'
	`).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// strategyChanged vérifie si stratégie a changé
func (s *Service) strategyChanged(new AllocationStrategy) bool {
	old := s.currentStrategy

	if new.VisionEnabled != old.VisionEnabled ||
		new.VisionResolution != old.VisionResolution ||
		new.VisionMaxSeqs != old.VisionMaxSeqs {
		return true
	}

	if new.ThinkEnabled != old.ThinkEnabled ||
		new.ThinkMaxSeqs != old.ThinkMaxSeqs {
		return true
	}

	if new.EmbeddingsEnabled != old.EmbeddingsEnabled {
		return true
	}

	return false
}

// applyStrategy applique nouvelle stratégie. previousStrategy permet de savoir
// quels modèles étaient actifs avant le changement (s.currentStrategy est déjà
// mise à jour à ce stade).
func (s *Service) applyStrategy(ctx context.Context, strategy AllocationStrategy, previousStrategy AllocationStrategy) error {
	// Arrêter instances désactivées — utilise les noms Docker connus
	// (pas la map interne, car les conteneurs peuvent avoir été lancés manuellement)
	if !strategy.VisionEnabled {
		s.manager.ForceStopContainer("vllm-qwen2-vl")
	}

	if !strategy.ThinkEnabled {
		s.manager.ForceStopContainer("vllm-qwen3-think")
	}

	if !strategy.EmbeddingsEnabled {
		s.manager.ForceStopContainer("vllm-gte-embed")
	}

	// Lancer instances activées
	if strategy.VisionEnabled {
		if err := s.manager.LaunchVisionVLLM(ctx, strategy); err != nil {
			return err
		}

		// Attendre que l'API soit prête (max 180s — CUDA graph warmup is slow)
		s.logger.Info("Waiting for Vision API to be ready...")
		if err := s.waitForHealthy(ctx, "vllm-vision", 180*time.Second); err != nil {
			s.logger.Error("Vision API failed to start", "error", err)
			return err
		}
	}

	if strategy.ThinkEnabled && !previousStrategy.ThinkEnabled {
		// Seulement lancer si n'était pas déjà actif
		// (évite restart inutile en mode standby)
		if err := s.manager.LaunchThinkVLLM(ctx, strategy); err != nil {
			return err
		}

		s.logger.Info("Waiting for Think API to be ready...")
		if err := s.waitForHealthy(ctx, "vllm-think", 180*time.Second); err != nil {
			s.logger.Error("Think API failed to start", "error", err)
			return err
		}
	}

	if strategy.EmbeddingsEnabled && !previousStrategy.EmbeddingsEnabled {
		if err := s.manager.LaunchEmbeddingVLLM(ctx, strategy); err != nil {
			return err
		}

		s.logger.Info("Waiting for Embed API to be ready...")
		if err := s.waitForHealthy(ctx, "vllm-embed", 120*time.Second); err != nil {
			s.logger.Error("Embed API failed to start", "error", err)
			return err
		}
	}

	return nil
}

// waitForHealthy attend qu'instance soit healthy.
// Vérifie que le conteneur Docker est toujours en vie pour abandonner
// rapidement en cas de crash ou suppression externe.
func (s *Service) waitForHealthy(ctx context.Context, instanceName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	consecutiveRefused := 0

	for time.Now().Before(deadline) {
		instance, exists := s.manager.GetInstance(instanceName)
		if !exists {
			return nil
		}

		result := s.health.CheckInstance(ctx, instance)
		if result.Healthy {
			s.logger.Info("Instance ready",
				"instance", instanceName,
				"latency_ms", result.Latency)
			return nil
		}

		// Si connection refused (conteneur disparu), compter les échecs consécutifs.
		// Après 5 échecs (10 secondes), vérifier si le conteneur existe encore.
		if result.Error != "" {
			consecutiveRefused++
		} else {
			consecutiveRefused = 0
		}

		if consecutiveRefused >= 5 {
			if !s.manager.ContainerRunning(instance.ContainerID) {
				s.logger.Warn("Container no longer running, aborting wait",
					"instance", instanceName,
					"container_id", instance.ContainerID)
				return fmt.Errorf("container for %s is no longer running", instanceName)
			}
			consecutiveRefused = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return context.DeadlineExceeded
}

// IsInstanceRunning vérifie si instance vLLM est active (version simplifiée pour test)
func (s *Service) IsInstanceRunning(modelType string) bool {
	serverURL := s.GetServerURL(modelType)

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(serverURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// GetServerURL retourne URL serveur selon type modèle
func (s *Service) GetServerURL(modelType string) string {
	switch modelType {
	case "think":
		return "http://localhost:8002"
	case "embed":
		return "http://localhost:8003"
	default:
		return "http://localhost:8001"
	}
}

// Close arrête service proprement
func (s *Service) Close() error {
	s.logger.Info("GPU Feeder shutting down")

	// Arrêter toutes instances
	for _, instance := range s.manager.ListInstances() {
		if err := s.manager.StopInstance(instance.Name); err != nil {
			s.logger.Error("Failed to stop instance",
				"instance", instance.Name,
				"error", err)
		}
	}

	return nil
}
