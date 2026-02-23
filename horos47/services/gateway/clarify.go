package gateway

import (
	"regexp"
	"strings"
)

// Uncertainty represents a detected semantic uncertainty in user content.
type Uncertainty struct {
	Type       string   `json:"type"`
	Severity   string   `json:"severity"`
	Evidence   []string `json:"evidence"`
	Suggestion string   `json:"suggestion"`
}

// UncertaintyDetector analyzes post content for semantic uncertainties
// that may require user clarification before LLM processing.
// Pure deterministic detection via regex patterns — no LLM call.
type UncertaintyDetector struct{}

// Polysemic words requiring context disambiguation (FR)
var polysemicWords = map[string][]string{
	"serveur": {"machine physique", "application serveur", "service backend"},
	"base":    {"base de données", "base de code", "fondation"},
	"client":  {"application cliente", "utilisateur", "customer"},
	"worker":  {"processus worker", "employé", "thread"},
	"queue":   {"file d'attente", "tail", "sequence"},
}

// Vague question patterns (FR)
var vaguePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bcomment faire\b`),
	regexp.MustCompile(`(?i)\bc'est quoi\b`),
	regexp.MustCompile(`(?i)\bpourquoi ça\b`),
	regexp.MustCompile(`(?i)\bestce que\b`),
	regexp.MustCompile(`(?i)\bça marche comment\b`),
	regexp.MustCompile(`(?i)\bcomment ça marche\b`),
}

// Contradiction markers (FR)
var contradictionMarkers = []string{
	"mais", "cependant", "néanmoins", "par contre", "en revanche", "contrairement",
}

// Scope keywords
var scopeKeywords = []string{
	"développement", "production", "staging", "test", "local",
}

// Priority keywords
var priorityKeywords = []string{
	"urgent", "prioritaire", "bloquant", "critique", "important",
}

// Aspect keywords
var aspectKeywords = []string{
	"performance", "sécurité", "fonctionnalité", "design", "documentation",
}

// Beginner markers
var beginnerMarkers = []string{
	"débutant", "commencer", "apprendre", "comprendre", "c'est quoi",
}

// Expert markers
var expertMarkers = []string{
	"optimiser", "architecture", "performance", "scalabilité", "design pattern",
}

// External reference pattern
var externalRefPattern = regexp.MustCompile(`(?i)\b(le|la|ce|cette)\s+(\w+)\b`)

// Analyze detects uncertainties in content and returns whether clarification is needed.
// needsClarification is true if at least one HIGH or CRITICAL uncertainty is found.
func (d *UncertaintyDetector) Analyze(content string) (bool, []Uncertainty) {
	var uncertainties []Uncertainty

	contentLower := strings.ToLower(content)

	// 1. Semantic ambiguity — polysemic words without context
	detectSemanticAmbiguity(contentLower, &uncertainties)

	// 2. Vague intention — open-ended questions without clear objective
	detectVagueIntention(content, &uncertainties)

	// 3. Apparent contradiction — contradiction markers present
	detectApparentContradiction(contentLower, &uncertainties)

	// 4. Missing context — external references without definition
	detectMissingContext(content, &uncertainties)

	// 5. Undefined scope — long post without environment mention
	detectUndefinedScope(content, contentLower, &uncertainties)

	// 6. Unknown priority — multiple aspects without prioritization
	detectUnknownPriority(contentLower, &uncertainties)

	// 7. Unclear expertise level — beginner AND expert markers simultaneously
	detectUnclearExpertiseLevel(contentLower, &uncertainties)

	needsClarification := false
	for _, u := range uncertainties {
		if u.Severity == "high" || u.Severity == "critical" {
			needsClarification = true
			break
		}
	}

	return needsClarification, uncertainties
}

func detectSemanticAmbiguity(contentLower string, out *[]Uncertainty) {
	for word, meanings := range polysemicWords {
		if !strings.Contains(contentLower, word) {
			continue
		}

		// Check if context already disambiguates
		contextFound := false
		for _, meaning := range meanings {
			if strings.Contains(contentLower, strings.ToLower(meaning)) {
				contextFound = true
				break
			}
		}

		if !contextFound {
			*out = append(*out, Uncertainty{
				Type:       "semantic_ambiguity",
				Severity:   "medium",
				Evidence:   []string{word},
				Suggestion: "Préciser quel sens de '" + word + "' est concerné parmi : " + strings.Join(meanings, ", "),
			})
		}
	}
}

func detectVagueIntention(content string, out *[]Uncertainty) {
	var evidence []string

	for _, pat := range vaguePatterns {
		matches := pat.FindAllString(content, -1)
		evidence = append(evidence, matches...)
	}

	if len(evidence) >= 2 {
		*out = append(*out, Uncertainty{
			Type:       "vague_intention",
			Severity:   "high",
			Evidence:   evidence,
			Suggestion: "Préciser l'objectif recherché ou le problème à résoudre",
		})
	}
}

func detectApparentContradiction(contentLower string, out *[]Uncertainty) {
	var found []string
	for _, marker := range contradictionMarkers {
		if strings.Contains(contentLower, marker) {
			found = append(found, marker)
		}
	}

	if len(found) > 0 {
		*out = append(*out, Uncertainty{
			Type:       "apparent_contradiction",
			Severity:   "medium",
			Evidence:   found,
			Suggestion: "Préciser si cette contradiction est intentionnelle (nouvelle information, correction)",
		})
	}
}

func detectMissingContext(content string, out *[]Uncertainty) {
	matches := externalRefPattern.FindAllStringSubmatch(content, -1)

	if len(matches) > 3 {
		evidence := make([]string, 0, 5)
		for i, m := range matches {
			if i >= 5 {
				break
			}
			evidence = append(evidence, m[0])
		}
		*out = append(*out, Uncertainty{
			Type:       "missing_context",
			Severity:   "medium",
			Evidence:   evidence,
			Suggestion: "Préciser à quoi réfèrent ces éléments dans le contexte actuel",
		})
	}
}

func detectUndefinedScope(content, contentLower string, out *[]Uncertainty) {
	if len(content) <= 200 {
		return
	}

	for _, kw := range scopeKeywords {
		if strings.Contains(contentLower, kw) {
			return
		}
	}

	*out = append(*out, Uncertainty{
		Type:       "undefined_scope",
		Severity:   "low",
		Evidence:   nil,
		Suggestion: "Préciser dans quel environnement cette question s'applique",
	})
}

func detectUnknownPriority(contentLower string, out *[]Uncertainty) {
	var aspectsFound []string
	for _, kw := range aspectKeywords {
		if strings.Contains(contentLower, kw) {
			aspectsFound = append(aspectsFound, kw)
		}
	}

	if len(aspectsFound) <= 1 {
		return
	}

	for _, kw := range priorityKeywords {
		if strings.Contains(contentLower, kw) {
			return
		}
	}

	*out = append(*out, Uncertainty{
		Type:       "unknown_priority",
		Severity:   "low",
		Evidence:   aspectsFound,
		Suggestion: "Préciser quel aspect prioriser",
	})
}

func detectUnclearExpertiseLevel(contentLower string, out *[]Uncertainty) {
	beginnerCount := 0
	for _, m := range beginnerMarkers {
		if strings.Contains(contentLower, m) {
			beginnerCount++
		}
	}

	expertCount := 0
	for _, m := range expertMarkers {
		if strings.Contains(contentLower, m) {
			expertCount++
		}
	}

	if beginnerCount > 0 && expertCount > 0 {
		*out = append(*out, Uncertainty{
			Type:       "unclear_expertise_level",
			Severity:   "low",
			Evidence:   nil,
			Suggestion: "Préciser niveau détail attendu (vue d'ensemble, guide pratique, analyse approfondie)",
		})
	}
}
