package gateway

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ClarificationQuestion is a mobile-first QCM question generated from an uncertainty.
type ClarificationQuestion struct {
	QuestionID   string           `json:"question_id"`
	Header       string           `json:"header"`        // max 12 chars
	Text         string           `json:"question_text"`
	QuestionType string           `json:"question_type"`  // single_choice | multiple_choice
	Options      []QuestionOption `json:"options"`
	AllowOther   bool             `json:"allow_other"`
	Priority     int              `json:"priority"`       // 1=critical, 2=important, 3=optional
}

// QuestionOption is a single option in a QCM question.
type QuestionOption struct {
	OptionID    string `json:"option_id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func newQuestionID() string {
	return fmt.Sprintf("q_%s", uuid.New().String()[:8])
}

// GenerateQuestions converts detected uncertainties into clarification questions.
func GenerateQuestions(uncertainties []Uncertainty) []ClarificationQuestion {
	var questions []ClarificationQuestion

	for _, u := range uncertainties {
		var q *ClarificationQuestion

		switch u.Type {
		case "semantic_ambiguity":
			q = generateSemanticAmbiguityQuestion(u)
		case "vague_intention":
			q = generateVagueIntentionQuestion(u)
		case "undefined_scope":
			q = generateUndefinedScopeQuestion(u)
		case "unclear_expertise_level":
			q = generateExpertiseLevelQuestion(u)
		default:
			q = generateGenericQuestion(u)
		}

		if q != nil {
			questions = append(questions, *q)
		}
	}

	return questions
}

func generateSemanticAmbiguityQuestion(u Uncertainty) *ClarificationQuestion {
	term := "terme"
	if len(u.Evidence) > 0 {
		term = u.Evidence[0]
	}

	// Extract meanings from polysemicWords map
	meanings, ok := polysemicWords[strings.ToLower(term)]
	if !ok {
		meanings = []string{"Option 1", "Option 2", "Option 3"}
	}

	options := make([]QuestionOption, 0, len(meanings))
	for i, meaning := range meanings {
		options = append(options, QuestionOption{
			OptionID:    fmt.Sprintf("opt%d", i+1),
			Label:       strings.ToUpper(meaning[:1]) + meaning[1:],
			Description: fmt.Sprintf("Vous faites référence à %s", meaning),
		})
	}

	return &ClarificationQuestion{
		QuestionID:   newQuestionID(),
		Header:       "Sens terme",
		Text:         fmt.Sprintf("Quand vous mentionnez \"%s\", parlez-vous de :", term),
		QuestionType: "single_choice",
		Options:      options,
		AllowOther:   true,
		Priority:     1,
	}
}

func generateVagueIntentionQuestion(_ Uncertainty) *ClarificationQuestion {
	return &ClarificationQuestion{
		QuestionID:   newQuestionID(),
		Header:       "Objectif",
		Text:         "Quel est votre objectif principal :",
		QuestionType: "single_choice",
		Options: []QuestionOption{
			{OptionID: "obj1", Label: "Comprendre concept", Description: "Obtenir explication théorique générale"},
			{OptionID: "obj2", Label: "Résoudre problème", Description: "Trouver solution à problème spécifique rencontré"},
			{OptionID: "obj3", Label: "Implémenter feature", Description: "Obtenir guide pratique implémentation"},
			{OptionID: "obj4", Label: "Optimiser existant", Description: "Améliorer performance ou qualité code existant"},
		},
		AllowOther: true,
		Priority:   1,
	}
}

func generateUndefinedScopeQuestion(_ Uncertainty) *ClarificationQuestion {
	return &ClarificationQuestion{
		QuestionID:   newQuestionID(),
		Header:       "Environnement",
		Text:         "Dans quel environnement cette question s'applique-t-elle ?",
		QuestionType: "multiple_choice",
		Options: []QuestionOption{
			{OptionID: "env1", Label: "Développement local", Description: "Machine développeur, tests locaux"},
			{OptionID: "env2", Label: "Staging", Description: "Environnement pré-production, tests intégration"},
			{OptionID: "env3", Label: "Production", Description: "Environnement live, utilisateurs réels"},
		},
		AllowOther: true,
		Priority:   2,
	}
}

func generateExpertiseLevelQuestion(_ Uncertainty) *ClarificationQuestion {
	return &ClarificationQuestion{
		QuestionID:   newQuestionID(),
		Header:       "Niveau détail",
		Text:         "Quel niveau de détail attendez-vous dans la réponse ?",
		QuestionType: "single_choice",
		Options: []QuestionOption{
			{OptionID: "lvl1", Label: "Vue d'ensemble", Description: "Comprendre concepts généraux, big picture"},
			{OptionID: "lvl2", Label: "Guide pratique", Description: "Étapes concrètes implémentation avec exemples"},
			{OptionID: "lvl3", Label: "Analyse approfondie", Description: "Détails techniques, edge cases, alternatives, trade-offs"},
		},
		AllowOther: false,
		Priority:   3,
	}
}

func generateGenericQuestion(u Uncertainty) *ClarificationQuestion {
	text := u.Suggestion
	if text == "" {
		text = "Pouvez-vous préciser ce point ?"
	}

	return &ClarificationQuestion{
		QuestionID:   newQuestionID(),
		Header:       "Précision",
		Text:         text,
		QuestionType: "single_choice",
		Options: []QuestionOption{
			{OptionID: "yes", Label: "Oui", Description: "Je peux préciser"},
			{OptionID: "no", Label: "Non", Description: "Générer sans précision"},
		},
		AllowOther: true,
		Priority:   3,
	}
}
