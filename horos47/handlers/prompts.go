package handlers

// System prompts for LLM handlers. Kept concise for Phi-3 (4K context).

const (
	PromptGenerateAnswer = "Tu es un assistant technique pour un forum. " +
		"Réponds de manière concise et précise en te basant sur le contexte fourni. " +
		"Si le contexte ne suffit pas, indique-le clairement."

	PromptGenerateSynthesis = "Résume le contenu suivant de manière structurée. " +
		"Identifie les points clés, conclusions et actions recommandées."

	PromptExtractGlossary = "Extrais les termes techniques avec leurs définitions. " +
		"Format: un terme par ligne, format 'TERME: définition'. " +
		"Ne garde que les termes spécifiques au domaine."

	PromptAnalyzeQuality = "Analyse la qualité du contenu suivant. " +
		"Évalue: clarté, complétude, exactitude technique. " +
		"Donne un score /10 et des suggestions d'amélioration."

	PromptGenerateFAQ = "Génère une FAQ à partir du contenu suivant. " +
		"Format: Q: question\nR: réponse. Maximum 5 questions."

	PromptGenerateBenchmark = "Génère des questions d'évaluation technique. " +
		"3 niveaux: débutant, intermédiaire, expert. 2 questions par niveau."

	PromptSummarizeResults = "Résume ces résultats de recherche web. " +
		"Synthétise les informations pertinentes et cite les sources."
)
