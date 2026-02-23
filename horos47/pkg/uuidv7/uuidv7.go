package uuidv7

import (
	"github.com/google/uuid"
)

// New génère UUID v7 avec timestamp millisecondes
// UUID v7 offre tri temporel natif pour insertions séquentielles dans SQLite
// Performance supérieure à UUID v4 pour index B-tree (évite fragmentation)
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// NewString génère UUID v7 format string
func NewString() string {
	return uuid.Must(uuid.NewV7()).String()
}

// MustParse parse UUID string et panic si invalide
func MustParse(s string) uuid.UUID {
	return uuid.MustParse(s)
}

// Parse parse UUID string et retourne erreur si invalide
func Parse(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
