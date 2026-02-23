package handlers

import (
	"horos47/core/jobs"
)

// RegisterAll registers all handler functions on the job worker.
func (h *Handlers) RegisterAll(worker *jobs.Worker) {
	// Vision pipeline (PDF → images → OCR → database)
	worker.RegisterHandler("pdf_to_images", h.HandlePDFToImages)
	worker.RegisterHandler("image_to_ocr", h.HandleImageToOCR)
	worker.RegisterHandler("ocr_to_database", h.HandleOCRToDatabase)

	// Gateway-driven workflow handlers
	worker.RegisterHandler("clarify_intent", h.HandleClarifyIntent)
	worker.RegisterHandler("fetch_and_ingest", h.HandleFetchAndIngest)
	worker.RegisterHandler("detect_format", h.HandleDetectFormat)
	worker.RegisterHandler("complete_ingest", h.HandleCompleteIngest)

	// RAG retrieval
	worker.RegisterHandler("rag_retrieve", h.HandleRAGRetrieve)

	// LLM generation handlers (via GPU Feeder V3 Think)
	worker.RegisterHandler("generate_answer", h.HandleGenerateAnswer)
	worker.RegisterHandler("generate_synthesis", h.HandleGenerateSynthesis)
	worker.RegisterHandler("extract_glossary", h.HandleExtractGlossary)
	worker.RegisterHandler("analyze_quality", h.HandleAnalyzeQuality)
	worker.RegisterHandler("generate_faq", h.HandleGenerateFAQ)
	worker.RegisterHandler("generate_benchmark", h.HandleGenerateBenchmark)

	// Stubs (external dependencies not yet available)
	worker.RegisterHandler("web_search", h.HandleWebSearchStub)
	worker.RegisterHandler("summarize_results", h.HandleSummarizeResultsStub)
	worker.RegisterHandler("embed_chunks", h.HandleEmbedChunks)

	h.Logger.Info("All handlers registered")
}
