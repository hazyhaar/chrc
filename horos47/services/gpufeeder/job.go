package gpufeeder

import "github.com/google/uuid"

// Job représente job GPU depuis table gpu_jobs
type Job struct {
	ID          uuid.UUID
	PayloadPath string
	ParentID    *uuid.UUID
	FragmentIdx int
	TotalFrags  int
	ModelType   string
	Attempts    int
}
