package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Worker est le moteur asynchrone de traitement des jobs
type Worker struct {
	queue       *Queue
	logger      *slog.Logger
	handlers    map[string]JobHandler
	concurrency map[string]int // max parallel jobs per type
}

// JobHandler est une fonction qui traite un job spécifique
type JobHandler func(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)

// NewWorker crée un nouveau worker avec sa queue
func NewWorker(db *sql.DB, logger *slog.Logger) *Worker {
	queue, _ := NewQueue(db)

	return &Worker{
		queue:       queue,
		logger:      logger,
		handlers:    make(map[string]JobHandler),
		concurrency: make(map[string]int),
	}
}

// RegisterHandler enregistre un handler pour un type de job
func (w *Worker) RegisterHandler(jobType string, handler JobHandler) {
	w.handlers[jobType] = handler
	w.logger.Info("Job handler registered", "type", jobType)
}

// SetConcurrency configure le parallélisme pour un type de job
func (w *Worker) SetConcurrency(jobType string, n int) {
	if n < 1 {
		n = 1
	}
	w.concurrency[jobType] = n
	w.logger.Info("Job concurrency configured", "type", jobType, "concurrency", n)
}

func (w *Worker) getConcurrency(jobType string) int {
	if n, ok := w.concurrency[jobType]; ok {
		return n
	}
	return 1 // default: sequential
}

// Start démarre la boucle de traitement des jobs
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("Job worker starting")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	retryTicker := time.NewTicker(30 * time.Second)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Job worker stopping")
			return ctx.Err()

		case <-retryTicker.C:
			retried, err := w.queue.RetryFailed()
			if err != nil {
				w.logger.Error("Failed to retry failed jobs", "error", err)
			} else if retried > 0 {
				w.logger.Info("Retried failed jobs", "count", retried)
			}

		case <-ticker.C:
			for jobType := range w.handlers {
				concurrency := w.getConcurrency(jobType)
				if concurrency > 1 {
					if err := w.processJobsBatch(ctx, jobType, concurrency); err != nil {
						w.logger.Error("Failed to process batch", "type", jobType, "error", err)
					}
				} else {
					if err := w.processNextJob(ctx, jobType); err != nil {
						if err != sql.ErrNoRows {
							w.logger.Error("Failed to process job", "type", jobType, "error", err)
						}
					}
				}
			}
		}
	}
}

// processJobsBatch traite un batch de jobs en parallèle avec semaphore
func (w *Worker) processJobsBatch(ctx context.Context, jobType string, limit int) error {
	jobs, err := w.queue.PollBatch(jobType, limit)
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	w.logger.Info("Processing batch",
		"type", jobType,
		"count", len(jobs),
		"concurrency", limit)

	handler, ok := w.handlers[jobType]
	if !ok {
		return fmt.Errorf("no handler registered for job type: %s", jobType)
	}

	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for _, job := range jobs {
		wg.Add(1)
		sem <- struct{}{}

		go func(j *Job) {
			defer wg.Done()
			defer func() { <-sem }()

			w.logger.Info("Processing job", "id", j.ID.String(), "type", j.Type)

			result, err := handler(ctx, j.Payload)
			if err != nil {
				w.logger.Error("Job handler failed",
					"id", j.ID.String(),
					"type", j.Type,
					"attempt", j.Attempts+1,
					"error", err)

				if failErr := w.queue.Fail(j.ID, err.Error()); failErr != nil {
					w.logger.Error("Failed to mark job as failed", "id", j.ID.String(), "error", failErr)
				}
				return
			}

			if err := w.queue.Complete(j.ID, result); err != nil {
				w.logger.Error("Failed to complete job", "id", j.ID.String(), "error", err)
				return
			}

			w.logger.Info("Job completed successfully", "id", j.ID.String())
		}(job)
	}

	wg.Wait()
	return nil
}

// processNextJob traite le prochain job en attente pour un type donné (séquentiel)
func (w *Worker) processNextJob(ctx context.Context, jobType string) error {
	job, err := w.queue.Poll(jobType)
	if err != nil {
		return err
	}

	if job == nil {
		return nil
	}

	w.logger.Info("Processing job", "id", job.ID.String(), "type", job.Type)

	handler, ok := w.handlers[job.Type]
	if !ok {
		return fmt.Errorf("no handler registered for job type: %s", job.Type)
	}

	result, err := handler(ctx, job.Payload)
	if err != nil {
		w.logger.Error("Job handler failed",
			"id", job.ID.String(),
			"attempt", job.Attempts+1,
			"error", err)

		if failErr := w.queue.Fail(job.ID, err.Error()); failErr != nil {
			w.logger.Error("Failed to mark job as failed", "id", job.ID.String(), "error", failErr)
		}

		return err
	}

	if err := w.queue.Complete(job.ID, result); err != nil {
		w.logger.Error("Failed to complete job", "id", job.ID.String(), "error", err)
		return err
	}

	w.logger.Info("Job completed successfully", "id", job.ID.String())

	return nil
}
