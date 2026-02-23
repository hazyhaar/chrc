package gpufeeder

import "time"

// GPUStats représente état GPU en temps réel
type GPUStats struct {
	MemoryUsedMB  int     `json:"memory_used_mb"`
	MemoryTotalMB int     `json:"memory_total_mb"`
	MemoryFreeMB  int     `json:"memory_free_mb"`
	Utilization   int     `json:"utilization_percent"`
	Temperature   int     `json:"temperature_c"`
	PowerWatts    int     `json:"power_watts"`
	Timestamp     int64   `json:"timestamp"`
}

// VLLMInstance représente un container vLLM actif
type VLLMInstance struct {
	Name              string    `json:"name"`
	Model             string    `json:"model"`
	Port              int       `json:"port"`
	ContainerID       string    `json:"container_id"`
	Status            string    `json:"status"` // "running", "stopped", "starting", "failed"
	GPUMemoryUtilPct  float64   `json:"gpu_memory_util_pct"`
	MaxNumSeqs        int       `json:"max_num_seqs"`
	MaxModelLen       int       `json:"max_model_len"`
	LastHealthCheck   time.Time `json:"last_health_check"`
	HealthStatus      bool      `json:"health_status"`
	StartedAt         time.Time `json:"started_at"`
	RestartCount      int       `json:"restart_count"`
	LMCacheMode       string    `json:"lmcache_mode"` // "complete", "degraded", ""
}

// AllocationStrategy définit stratégie d'allocation GPU
type AllocationStrategy struct {
	VisionEnabled     bool    `json:"vision_enabled"`
	VisionMemoryPct   float64 `json:"vision_memory_pct"`
	VisionMaxSeqs     int     `json:"vision_max_seqs"`
	VisionResolution  int     `json:"vision_resolution_dpi"` // 150 ou 300

	ThinkEnabled      bool    `json:"think_enabled"`
	ThinkMemoryPct    float64 `json:"think_memory_pct"`
	ThinkMaxSeqs      int     `json:"think_max_seqs"`
	ThinkMaxModelLen  int     `json:"think_max_model_len"` // 524288 (complete) ou 131072 (degraded)
	ThinkLMCacheMode  string  `json:"think_lmcache_mode"`  // "complete" ou "degraded"

	EmbeddingsEnabled bool    `json:"embeddings_enabled"`
	EmbeddingsMemoryPct float64 `json:"embeddings_memory_pct"`
	EmbeddingsMaxSeqs int     `json:"embeddings_max_seqs"`

	Reason            string  `json:"reason"` // Explication de la stratégie
}

// WorkloadStats représente charge de travail actuelle
type WorkloadStats struct {
	VisionJobsPending    int `json:"vision_jobs_pending"`
	ThinkRequestsActive  int `json:"think_requests_active"`
	EmbeddingsJobsPending int `json:"embeddings_jobs_pending"`
	ExternalProcessVRAM  int `json:"external_process_vram_mb"` // Mechabellum etc
}

// HealthCheckResult résultat santé d'une instance
type HealthCheckResult struct {
	InstanceName string    `json:"instance_name"`
	Healthy      bool      `json:"healthy"`
	Latency      int64     `json:"latency_ms"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// ContentPart représente partie de contenu (texte ou image) pour OpenAI API
type ContentPart struct {
	Type     string    `json:"type"` // "text" ou "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL représente URL image (data: ou file://)
type ImageURL struct {
	URL string `json:"url"`
}
