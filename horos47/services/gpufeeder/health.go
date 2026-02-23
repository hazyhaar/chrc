package gpufeeder

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HealthChecker vérifie santé des instances vLLM
type HealthChecker struct {
	logger *slog.Logger
	client *http.Client
}

// NewHealthChecker crée nouveau health checker
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		logger: logger,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// CheckInstance vérifie santé d'une instance vLLM
func (hc *HealthChecker) CheckInstance(ctx context.Context, instance *VLLMInstance) HealthCheckResult {
	result := HealthCheckResult{
		InstanceName: instance.Name,
		Timestamp:    time.Now(),
	}

	url := fmt.Sprintf("http://localhost:%d/health", instance.Port)

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	resp, err := hc.client.Do(req)
	latency := time.Since(start)
	result.Latency = latency.Milliseconds()

	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		hc.logger.Warn("Health check failed",
			"instance", instance.Name,
			"error", err,
			"latency_ms", result.Latency)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		result.Healthy = true
		instance.LastHealthCheck = time.Now()
		instance.HealthStatus = true
		instance.Status = "running"

		hc.logger.Debug("Health check OK",
			"instance", instance.Name,
			"latency_ms", result.Latency)
	} else {
		result.Error = fmt.Sprintf("unexpected status: %d", resp.StatusCode)
		instance.HealthStatus = false

		hc.logger.Warn("Health check failed",
			"instance", instance.Name,
			"status", resp.StatusCode,
			"latency_ms", result.Latency)
	}

	return result
}

// CheckAllInstances vérifie toutes instances
func (hc *HealthChecker) CheckAllInstances(ctx context.Context, manager *ProcessManager) []HealthCheckResult {
	instances := manager.ListInstances()
	results := make([]HealthCheckResult, 0, len(instances))

	for _, instance := range instances {
		if instance.Status == "stopped" {
			continue
		}

		result := hc.CheckInstance(ctx, instance)
		results = append(results, result)
	}

	return results
}

// MonitorContinuous lance monitoring continu avec auto-restart
func (hc *HealthChecker) MonitorContinuous(ctx context.Context, manager *ProcessManager, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			results := hc.CheckAllInstances(ctx, manager)

			for _, result := range results {
				if !result.Healthy {
					// Instance unhealthy - considérer restart
					instance, exists := manager.GetInstance(result.InstanceName)
					if !exists {
						continue
					}

					// Backoff exponentiel (max 5 restarts)
					if instance.RestartCount >= 5 {
						hc.logger.Error("Instance failed too many times, giving up",
							"instance", instance.Name,
							"restart_count", instance.RestartCount)
						instance.Status = "failed"
						continue
					}

					hc.logger.Warn("Instance unhealthy, scheduling restart",
						"instance", instance.Name,
						"restart_count", instance.RestartCount)

					// TODO: Implémenter logique de restart avec backoff
					// Pour l'instant, juste logger
				}
			}
		}
	}
}
