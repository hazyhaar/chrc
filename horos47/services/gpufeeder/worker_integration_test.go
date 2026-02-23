package gpufeeder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWorker_IntegrationBatchLoad teste le worker avec fixture réelle (440 images PNG)
func TestWorker_IntegrationBatchLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Fixture réelle : 440 images PNG d'un PDF
	fixtureImagesDir := "/inference/INGEST_PROCESSING/019c244c-b684-7a2d-a6e4-ed0bb2930282/pdf_to_images"

	// Vérifier que fixture existe
	if _, err := os.Stat(fixtureImagesDir); os.IsNotExist(err) {
		t.Skipf("Fixture directory not found: %s", fixtureImagesDir)
	}

	// Setup tmpdir avec symlink vers fixture réelle
	tmpDir := t.TempDir()
	ocrDir := filepath.Join(tmpDir, "stage_1_ocr", "pending")
	thinkDir := filepath.Join(tmpDir, "stage_5_think", "pending")

	if err := os.MkdirAll(filepath.Dir(ocrDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(thinkDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Symlink vers vraies images (pas de copie, juste référence)
	if err := os.Symlink(fixtureImagesDir, ocrDir); err != nil {
		t.Fatal(err)
	}

	// Logger verbeux pour debug
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Worker avec pollInterval court pour tests rapides
	worker := NewWorker(tmpDir, logger)
	worker.pollInterval = 100 * time.Millisecond // Poll rapide pour tests

	// Compter les images dans la fixture
	imageCount := countFilesInDir(fixtureImagesDir)
	t.Logf("Fixture contains %d images (should be ~440)", imageCount)

	if imageCount < 50 {
		t.Fatalf("Fixture has only %d images, need at least 50 for test", imageCount)
	}

	// Context avec timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Lancer worker en goroutine
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- worker.Run(ctx)
	}()

	// Test 1: Fixture réelle 440 images → doit passer en mode VISION
	t.Logf("Waiting for worker to detect %d images and transition to VISION...", imageCount)
	time.Sleep(300 * time.Millisecond)

	if worker.currentMode != ModeVision {
		t.Errorf("Expected ModeVision with %d images (threshold=50), got %v", imageCount, worker.currentMode)
	}

	// Test 2: Pre-emption Think (créer 1 job Think)
	t.Log("Creating 1 Think job for pre-emption test...")
	thinkFile := filepath.Join(thinkDir, "think_urgent.json")
	if err := os.WriteFile(thinkFile, []byte(`{"prompt":"urgent"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Attendre transition VISION → THINK
	time.Sleep(300 * time.Millisecond)
	if worker.currentMode != ModeThink {
		t.Errorf("Expected ModeThink after pre-emption, got %v", worker.currentMode)
	}

	// Test 3: Retour VISION après suppression Think
	t.Log("Removing Think job, Vision should resume...")
	os.Remove(thinkFile)

	// Attendre transition THINK → VISION (images OCR toujours présentes)
	time.Sleep(300 * time.Millisecond)
	if worker.currentMode != ModeVision {
		t.Errorf("Expected ModeVision after Think job removed, got %v", worker.currentMode)
	}

	// Arrêter worker proprement
	cancel()
	<-workerDone

	t.Log("✓ Test passed: 440 real images fixture, transitions IDLE→VISION→THINK→VISION")
}

// countFilesInDir compte tous les fichiers (pas les répertoires) dans un dossier
func countFilesInDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

// BenchmarkWorker_CountPending benchmarke le count fichiers avec gros volume
func BenchmarkWorker_CountPending(b *testing.B) {
	// Setup tmpdir avec 10 000 fichiers
	tmpDir := b.TempDir()
	ocrDir := filepath.Join(tmpDir, "stage_1_ocr", "pending")
	os.MkdirAll(ocrDir, 0755)

	b.Log("Creating 10,000 OCR jobs for benchmark...")
	for i := 0; i < 10000; i++ {
		filename := filepath.Join(ocrDir, fmt.Sprintf("job_%05d.json", i))
		os.WriteFile(filename, []byte(fmt.Sprintf(`{"page":%d}`, i)), 0644)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	worker := &Worker{
		dataDir: tmpDir,
		logger:  logger,
	}

	b.ResetTimer()

	// Benchmark count (doit être rapide même avec 10k fichiers)
	for i := 0; i < b.N; i++ {
		count := worker.countPending("stage_1_ocr")
		if count != 10000 {
			b.Fatalf("Expected 10000, got %d", count)
		}
	}
}

// TestWorker_ThrashingPrevention teste que l'hysteresis empêche le thrashing
func TestWorker_ThrashingPrevention(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	ocrDir := filepath.Join(tmpDir, "stage_1_ocr", "pending")
	thinkDir := filepath.Join(tmpDir, "stage_5_think", "pending")
	os.MkdirAll(ocrDir, 0755)
	os.MkdirAll(thinkDir, 0755)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	worker := &Worker{
		dataDir:      tmpDir,
		logger:       logger,
		currentMode:  ModeIdle,
		stateCfg:     Config{VisionThreshold: 50, ThinkThreshold: 1},
		dockerCfg:    DefaultDockerConfig,
		pollInterval: 50 * time.Millisecond,
	}

	transitionCount := 0
	worker.executeAction = func(action Action) {
		if action != ActionNone {
			transitionCount++
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Run(ctx)

	// Créer exactement 49 jobs OCR (sous le seuil Vision = 50)
	// Hysteresis doit empêcher activation Vision
	t.Log("Creating 49 OCR jobs (below threshold)...")
	for i := 0; i < 49; i++ {
		filename := filepath.Join(ocrDir, fmt.Sprintf("job_%02d.json", i))
		os.WriteFile(filename, []byte(fmt.Sprintf(`{"page":%d}`, i)), 0644)
	}

	// Attendre plusieurs cycles polling
	time.Sleep(500 * time.Millisecond)

	// Worker doit rester IDLE (pas de transition)
	if worker.currentMode != ModeIdle {
		t.Errorf("Expected ModeIdle with 49 jobs (below threshold 50), got %v", worker.currentMode)
	}

	if transitionCount > 0 {
		t.Errorf("Expected 0 transitions with jobs below threshold, got %d", transitionCount)
	}

	// Ajouter 1 job pour passer le seuil (49 + 1 = 50)
	t.Log("Adding 1 more job to reach threshold (50)...")
	filename := filepath.Join(ocrDir, "job_50.json")
	os.WriteFile(filename, []byte(`{"page":50}`), 0644)

	time.Sleep(200 * time.Millisecond)

	// Maintenant doit transitionner vers Vision
	if worker.currentMode != ModeVision {
		t.Errorf("Expected ModeVision with 50 jobs (at threshold), got %v", worker.currentMode)
	}

	if transitionCount != 1 {
		t.Errorf("Expected exactly 1 transition, got %d", transitionCount)
	}

	cancel()
}

// TestWorker_RapidJobCreation teste création jobs pendant que worker tourne
func TestWorker_RapidJobCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	ocrDir := filepath.Join(tmpDir, "stage_1_ocr", "pending")
	os.MkdirAll(ocrDir, 0755)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	worker := &Worker{
		dataDir:      tmpDir,
		logger:       logger,
		currentMode:  ModeIdle,
		stateCfg:     Config{VisionThreshold: 50, ThinkThreshold: 1},
		dockerCfg:    DefaultDockerConfig,
		pollInterval: 100 * time.Millisecond,
	}

	worker.executeAction = func(action Action) {
		// Mock action
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go worker.Run(ctx)

	// Créer jobs rapidement (1000 jobs/seconde pendant 1 seconde = 1000 jobs)
	t.Log("Creating 1000 jobs rapidly (stress test)...")
	jobCreationDone := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			filename := filepath.Join(ocrDir, fmt.Sprintf("job_%04d.json", i))
			os.WriteFile(filename, []byte(fmt.Sprintf(`{"page":%d}`, i)), 0644)
			if i%100 == 0 {
				time.Sleep(10 * time.Millisecond) // Throttle un peu
			}
		}
		close(jobCreationDone)
	}()

	<-jobCreationDone

	// Attendre que worker détecte
	time.Sleep(300 * time.Millisecond)

	// Worker doit avoir transitionné vers Vision
	if worker.currentMode != ModeVision {
		t.Errorf("Expected ModeVision after rapid job creation, got %v", worker.currentMode)
	}

	// Vérifier count précis
	finalCount := worker.countPending("stage_1_ocr")
	if finalCount != 1000 {
		t.Errorf("Expected 1000 jobs counted, got %d", finalCount)
	}

	cancel()
}

// TestWorker_ConcurrentReads teste que countPending est safe avec lectures concurrentes
func TestWorker_ConcurrentReads(t *testing.T) {
	tmpDir := t.TempDir()
	ocrDir := filepath.Join(tmpDir, "stage_1_ocr", "pending")
	os.MkdirAll(ocrDir, 0755)

	// Créer 1000 fichiers
	for i := 0; i < 1000; i++ {
		filename := filepath.Join(ocrDir, fmt.Sprintf("job_%04d.json", i))
		os.WriteFile(filename, []byte(fmt.Sprintf(`{"page":%d}`, i)), 0644)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	worker := &Worker{
		dataDir: tmpDir,
		logger:  logger,
	}

	// Lancer 10 goroutines qui comptent en parallèle
	done := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			count := worker.countPending("stage_1_ocr")
			done <- count
		}()
	}

	// Vérifier que toutes retournent 1000
	for i := 0; i < 10; i++ {
		count := <-done
		if count != 1000 {
			t.Errorf("Concurrent read %d: expected 1000, got %d", i, count)
		}
	}
}
