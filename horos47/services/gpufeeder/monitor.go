package gpufeeder

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GPUMonitor surveille état GPU via nvidia-smi
type GPUMonitor struct {
	logger       *slog.Logger
	pollInterval time.Duration
	lastStats    *GPUStats
}

// NewGPUMonitor crée nouveau moniteur GPU
func NewGPUMonitor(logger *slog.Logger) *GPUMonitor {
	return &GPUMonitor{
		logger:       logger,
		pollInterval: 1 * time.Second, // Gemini recommande monitoring continu
	}
}

// GetStats récupère statistiques GPU actuelles
func (m *GPUMonitor) GetStats() (*GPUStats, error) {
	// nvidia-smi --query-gpu=memory.used,memory.total,memory.free,utilization.gpu,temperature.gpu,power.draw --format=csv,noheader,nounits
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=memory.used,memory.total,memory.free,utilization.gpu,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nvidia-smi failed: %w", err)
	}

	// Parse output: "9277, 32607, 23330, 45, 62, 245"
	line := strings.TrimSpace(out.String())
	fields := strings.Split(line, ",")
	if len(fields) < 6 {
		return nil, fmt.Errorf("invalid nvidia-smi output: %s", line)
	}

	stats := &GPUStats{
		Timestamp: time.Now().Unix(),
	}

	// Parse chaque champ (trim whitespace)
	stats.MemoryUsedMB, _ = strconv.Atoi(strings.TrimSpace(fields[0]))
	stats.MemoryTotalMB, _ = strconv.Atoi(strings.TrimSpace(fields[1]))
	stats.MemoryFreeMB, _ = strconv.Atoi(strings.TrimSpace(fields[2]))
	stats.Utilization, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
	stats.Temperature, _ = strconv.Atoi(strings.TrimSpace(fields[4]))

	// Power peut être float (ex: "245.67")
	powerStr := strings.TrimSpace(fields[5])
	powerFloat, _ := strconv.ParseFloat(powerStr, 64)
	stats.PowerWatts = int(powerFloat)

	m.lastStats = stats
	return stats, nil
}

// DetectExternalProcesses détecte processus externes utilisant GPU (Mechabellum etc)
func (m *GPUMonitor) DetectExternalProcesses() (int, error) {
	// nvidia-smi --query-compute-apps=pid,used_memory,process_name --format=csv,noheader
	cmd := exec.Command("nvidia-smi",
		"--query-compute-apps=pid,used_memory,process_name",
		"--format=csv,noheader")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		// Pas de processus GPU = OK
		return 0, nil
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	totalExternalMB := 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}

		processName := strings.TrimSpace(fields[2])
		memoryMB, _ := strconv.Atoi(strings.TrimSpace(fields[1]))

		// Ignorer nos propres processus vLLM
		if strings.Contains(processName, "vllm") ||
		   strings.Contains(processName, "horos") {
			continue
		}

		// Processus externe détecté
		m.logger.Info("External GPU process detected",
			"name", processName,
			"vram_mb", memoryMB)

		totalExternalMB += memoryMB
	}

	return totalExternalMB, nil
}

// CheckThermalThrottling vérifie si GPU throttle thermiquement
func (m *GPUMonitor) CheckThermalThrottling() bool {
	if m.lastStats == nil {
		return false
	}

	// Gemini: Au-delà de 80°C, risque de throttling
	if m.lastStats.Temperature > 80 {
		m.logger.Warn("GPU thermal warning",
			"temp_c", m.lastStats.Temperature,
			"threshold", 80)
		return true
	}

	return false
}

// GetCacheUsagePercent calcule pourcentage utilisation KV cache
// Basé sur formule Gemini : Budget KV = Total - Statique - Activations
func (m *GPUMonitor) GetCacheUsagePercent() float64 {
	if m.lastStats == nil {
		return 0
	}

	const (
		staticMB      = 16720 // 15.22 GB modèle + 1.5 GB overhead (Gemini)
		activationsMB = 2000  // 2 GB activations prefill (Gemini)
	)

	kvBudgetMB := m.lastStats.MemoryTotalMB - staticMB - activationsMB
	kvUsedMB := m.lastStats.MemoryUsedMB - staticMB

	if kvUsedMB < 0 {
		kvUsedMB = 0
	}

	if kvBudgetMB <= 0 {
		return 100.0
	}

	return (float64(kvUsedMB) / float64(kvBudgetMB)) * 100.0
}
