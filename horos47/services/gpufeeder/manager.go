package gpufeeder

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// ProcessManager gère lifecycle containers vLLM
type ProcessManager struct {
	logger    *slog.Logger
	cfg       Config
	instances map[string]*VLLMInstance // name -> instance
}

// NewProcessManager crée nouveau gestionnaire processus
func NewProcessManager(logger *slog.Logger, cfg Config) *ProcessManager {
	return &ProcessManager{
		logger:    logger,
		cfg:       cfg,
		instances: make(map[string]*VLLMInstance),
	}
}

// LaunchVisionVLLM lance container vLLM Vision avec params Gemini
func (pm *ProcessManager) LaunchVisionVLLM(ctx context.Context, strategy AllocationStrategy) error {
	const (
		name          = "vllm-vision"
		model         = "/models/qwen2-vl-7b-instruct"
		port          = 8001
		containerName = "vllm-qwen2-vl"
	)

	// Arrêter container existant
	pm.stopContainer(containerName)

	pm.logger.Info("Launching vLLM Vision",
		"resolution_dpi", strategy.VisionResolution,
		"max_seqs", strategy.VisionMaxSeqs,
		"gpu_memory_util", strategy.VisionMemoryPct)

	// Disable upstream flash_attn to avoid cu_seqlens_q CUDA crash on SM 12.0 (RTX 5090).
	// This forces the vision encoder to use TORCH_SDPA backend instead of upstream flash_attn.
	// The text layers still use vLLM's internal FlashInfer backend.
	disableFlashAttn := "mv /usr/local/lib/python3.12/dist-packages/flash_attn{,.disabled} 2>/dev/null; " +
		"mv /usr/local/lib/python3.12/dist-packages/flash_attn_2_cuda.cpython-312-x86_64-linux-gnu.so{,.disabled} 2>/dev/null; " +
		`mv "/usr/local/lib/python3.12/dist-packages/flash_attn-2.7.4.post1+25.11.dist-info"{,.disabled} 2>/dev/null; `

	vllmCmd := fmt.Sprintf("%sexec vllm serve %s --dtype bfloat16 --gpu-memory-utilization %.2f --max-model-len 16384 --max-num-seqs %d",
		disableFlashAttn, model, strategy.VisionMemoryPct, strategy.VisionMaxSeqs)

	args := []string{
		"run", "-d",
		"--gpus", "all",
		"-e", "PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True",
		"--name", containerName,
		"--shm-size=16gb",
		"--ipc=host",
		"--ulimit", "memlock=-1",
		"--ulimit", "stack=67108864",
		"-p", fmt.Sprintf("%d:8000", port),
		"-v", "/inference/models:/models",
		"--entrypoint", "/bin/bash",
		"nvcr.io/nvidia/vllm:25.11-py3",
		"-c", vllmCmd,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to launch vLLM Vision: %w\nOutput: %s", err, out.String())
	}

	containerID := strings.TrimSpace(out.String())

	// Créer instance tracking
	instance := &VLLMInstance{
		Name:             name,
		Model:            model,
		Port:             port,
		ContainerID:      containerID,
		Status:           "starting",
		GPUMemoryUtilPct: strategy.VisionMemoryPct,
		MaxNumSeqs:       strategy.VisionMaxSeqs,
		MaxModelLen:      16384,
		StartedAt:        time.Now(),
	}

	pm.instances[name] = instance

	pm.logger.Info("vLLM Vision container started",
		"container_id", containerID[:12],
		"port", port)

	return nil
}

// LaunchThinkVLLM lance container vLLM Think (Qwen3-8B NVFP4).
// Bifurque entre mode LMCache complet et mode dégradé (offloading natif)
// selon strategy.ThinkLMCacheMode.
func (pm *ProcessManager) LaunchThinkVLLM(ctx context.Context, strategy AllocationStrategy) error {
	const (
		name          = "vllm-think"
		model         = "/models/Qwen3-8B-NVFP4"
		port          = 8002
		containerName = "vllm-qwen3-think"
	)

	pm.stopContainer(containerName)

	pm.logger.Info("Launching vLLM Think (Qwen3-8B-NVFP4)",
		"max_seqs", strategy.ThinkMaxSeqs,
		"gpu_memory_util", strategy.ThinkMemoryPct,
		"lmcache_mode", strategy.ThinkLMCacheMode,
		"max_model_len", strategy.ThinkMaxModelLen)

	var args []string
	if strategy.ThinkLMCacheMode == "complete" {
		args = pm.buildThinkArgsComplete(strategy, model, port, containerName)
	} else {
		args = pm.buildThinkArgsDegraded(strategy, model, port, containerName)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to launch vLLM Think (%s): %w\nOutput: %s",
			strategy.ThinkLMCacheMode, err, out.String())
	}

	containerID := strings.TrimSpace(out.String())

	maxModelLen := strategy.ThinkMaxModelLen
	if maxModelLen == 0 {
		maxModelLen = 131072
	}

	instance := &VLLMInstance{
		Name:             name,
		Model:            model,
		Port:             port,
		ContainerID:      containerID,
		Status:           "starting",
		GPUMemoryUtilPct: strategy.ThinkMemoryPct,
		MaxNumSeqs:       strategy.ThinkMaxSeqs,
		MaxModelLen:      maxModelLen,
		LMCacheMode:      strategy.ThinkLMCacheMode,
		StartedAt:        time.Now(),
	}

	pm.instances[name] = instance

	pm.logger.Info("vLLM Think container started",
		"container_id", containerID[:12],
		"port", port,
		"lmcache_mode", strategy.ThinkLMCacheMode)

	return nil
}

// buildThinkArgsComplete construit les arguments Docker pour le mode LMCache complet.
// Utilise l'image NVIDIA vLLM standard (SM_120 pré-compilé) avec pip install lmcache
// au démarrage. Offloading hiérarchique VRAM → RAM (48 Go) → NVMe (/kvcache).
func (pm *ProcessManager) buildThinkArgsComplete(strategy AllocationStrategy, model string, port int, containerName string) []string {
	maxModelLen := strategy.ThinkMaxModelLen
	if maxModelLen == 0 {
		maxModelLen = 524288
	}

	kvTransferConfig := `{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_both"}`

	// Injecter lmcache via pip dans le conteneur NVIDIA (qui a FlashInfer SM_120 pré-compilé).
	// Le conteneur NVIDIA 25.11 utilise CUDA 13 : il faut fournir libcudart.so.12 (via
	// nvidia-cuda-runtime-cu12) car lmcache.c_ops est compilé contre CUDA 12, puis
	// forcer nixl-cu13 pour éviter le pull de nixl-cu12.
	vllmCmd := fmt.Sprintf(
		"pip install nvidia-cuda-runtime-cu12 nixl-cu13 && "+
			"pip install --no-deps nixl && "+
			"pip install lmcache && "+
			"vllm serve %s "+
			"--dtype auto "+
			"--max-model-len %d "+
			"--gpu-memory-utilization %.2f "+
			"--max-num-seqs %d "+
			"--kv-transfer-config '%s' "+
			"--enable-chunked-prefill",
		model, maxModelLen, strategy.ThinkMemoryPct, strategy.ThinkMaxSeqs, kvTransferConfig,
	)

	return []string{
		"run", "-d",
		"--gpus", "all",
		"--name", containerName,
		"--shm-size=16gb", "--ipc=host",
		"-p", fmt.Sprintf("%d:8000", port),
		"-v", "/inference/models:/models",
		"-v", pm.cfg.KVCacheMountPoint + ":/kvcache",
		"-v", pm.cfg.LMCacheConfigPath + ":/etc/lmcache.yaml:ro",
		"-e", "LMCACHE_CONFIG_FILE=/etc/lmcache.yaml",
		"-e", "LMCACHE_USE_EXPERIMENTAL=True",
		"-e", "VLLM_ALLOW_LONG_MAX_MODEL_LEN=1",
		"-e", "VLLM_FLASH_ATTN_VERSION=2",
		"-e", "NCCL_DMABUF_ENABLE=1",
		"-e", "NCCL_P2P_LEVEL=PCI",
		"--entrypoint", "/bin/bash",
		"nvcr.io/nvidia/vllm:25.11-py3",
		"-c", vllmCmd,
	}
}

// buildThinkArgsDegraded construit les arguments Docker pour le mode dégradé.
// Utilise l'image NVIDIA vLLM standard avec offloading natif KV vers la RAM système.
func (pm *ProcessManager) buildThinkArgsDegraded(strategy AllocationStrategy, model string, port int, containerName string) []string {
	maxModelLen := strategy.ThinkMaxModelLen
	if maxModelLen == 0 {
		maxModelLen = 131072
	}

	return []string{
		"run", "-d",
		"--gpus", "all",
		"--name", containerName,
		"--shm-size=8gb", "--ipc=host",
		"-p", fmt.Sprintf("%d:8000", port),
		"-v", "/inference/models:/models",
		"-e", "VLLM_FLASH_ATTN_VERSION=2",
		"-e", "NCCL_DMABUF_ENABLE=1",
		"-e", "NCCL_P2P_LEVEL=PCI",
		"--entrypoint", "vllm",
		"nvcr.io/nvidia/vllm:25.11-py3",
		"serve", model,
		"--dtype", "auto",

		"--max-model-len", fmt.Sprintf("%d", maxModelLen),
		"--gpu-memory-utilization", fmt.Sprintf("%.2f", strategy.ThinkMemoryPct),
		"--max-num-seqs", fmt.Sprintf("%d", strategy.ThinkMaxSeqs),
		"--kv-offloading-backend", "native",
		"--kv-offloading-size", fmt.Sprintf("%d", pm.cfg.NativeOffloadingSizeGB),
		"--enable-chunked-prefill",
	}
}

// LaunchEmbeddingVLLM lance container vLLM Embedding (gte-Qwen2-1.5B-instruct)
func (pm *ProcessManager) LaunchEmbeddingVLLM(ctx context.Context, strategy AllocationStrategy) error {
	const (
		name          = "vllm-embed"
		model         = "/models/gte-Qwen2-1.5B-instruct"
		port          = 8003
		containerName = "vllm-gte-embed"
	)

	pm.stopContainer(containerName)

	pm.logger.Info("Launching vLLM Embedding (gte-Qwen2-1.5B-instruct)",
		"max_seqs", strategy.EmbeddingsMaxSeqs,
		"gpu_memory_util", strategy.EmbeddingsMemoryPct)

	args := []string{
		"run", "-d",
		"--gpus", "all",
		"--name", containerName,
		"--shm-size=4gb", "--ipc=host",
		"-p", fmt.Sprintf("%d:8000", port),
		"-v", "/inference/models:/models",
		"--entrypoint", "vllm",
		"nvcr.io/nvidia/vllm:25.11-py3",
		"serve", model,
		"--task", "embedding",
		"--dtype", "bfloat16",
		"--max-model-len", "8192",
		"--gpu-memory-utilization", fmt.Sprintf("%.2f", strategy.EmbeddingsMemoryPct),
		"--max-num-seqs", fmt.Sprintf("%d", strategy.EmbeddingsMaxSeqs),
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to launch vLLM Embedding: %w\nOutput: %s", err, out.String())
	}

	containerID := strings.TrimSpace(out.String())

	instance := &VLLMInstance{
		Name:             name,
		Model:            model,
		Port:             port,
		ContainerID:      containerID,
		Status:           "starting",
		GPUMemoryUtilPct: strategy.EmbeddingsMemoryPct,
		MaxNumSeqs:       strategy.EmbeddingsMaxSeqs,
		MaxModelLen:      8192,
		StartedAt:        time.Now(),
	}

	pm.instances[name] = instance

	pm.logger.Info("vLLM Embedding container started",
		"container_id", containerID[:12],
		"port", port)

	return nil
}

// StopInstance arrête une instance vLLM
func (pm *ProcessManager) StopInstance(name string) error {
	instance, exists := pm.instances[name]
	if !exists {
		return fmt.Errorf("instance %s not found", name)
	}

	containerName := instance.ContainerID
	if containerName == "" {
		return nil
	}

	return pm.stopContainer(containerName)
}

// ForceStopContainer stops a Docker container by its Docker name, regardless
// of whether it was registered in the process manager's instances map.
func (pm *ProcessManager) ForceStopContainer(containerName string) {
	if err := pm.stopContainer(containerName); err != nil {
		pm.logger.Debug("ForceStopContainer", "name", containerName, "error", err)
	}
	delete(pm.instances, containerName)
}

// stopContainer helper pour arrêter container Docker
func (pm *ProcessManager) stopContainer(name string) error {
	// Check if exists
	checkCmd := exec.Command("docker", "ps", "-a", "--format", "{{.Names}}")
	var out bytes.Buffer
	checkCmd.Stdout = &out
	if err := checkCmd.Run(); err != nil {
		return err
	}

	if !strings.Contains(out.String(), name) {
		return nil // Container doesn't exist
	}

	pm.logger.Info("Stopping container", "name", name)

	// Stop and remove
	stopCmd := exec.Command("docker", "rm", "-f", name)
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", name, err)
	}

	return nil
}

// ContainerRunning vérifie si un conteneur Docker est en cours d'exécution.
func (pm *ProcessManager) ContainerRunning(containerID string) bool {
	if containerID == "" {
		return false
	}
	out, err := exec.Command("docker", "inspect", "--format={{.State.Running}}", containerID).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// GetInstance retourne instance par nom
func (pm *ProcessManager) GetInstance(name string) (*VLLMInstance, bool) {
	instance, ok := pm.instances[name]
	return instance, ok
}

// ListInstances retourne toutes instances actives
func (pm *ProcessManager) ListInstances() []*VLLMInstance {
	instances := make([]*VLLMInstance, 0, len(pm.instances))
	for _, inst := range pm.instances {
		instances = append(instances, inst)
	}
	return instances
}
