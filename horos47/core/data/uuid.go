package data

import (
	"database/sql/driver"
	"fmt"

	"github.com/google/uuid"
)

// UUID est un wrapper autour de gofrs/uuid.UUID optimisé pour SQLite.
// Implémente sql.Scanner et driver.Valuer pour stockage BLOB transparent.
type UUID struct {
	uuid.UUID
}

// NewUUID génère un nouvel UUIDv7 avec monotonicité garantie.
// Les UUIDv7 sont séquentiels (timestamp + compteur) pour performance B-Tree.
func NewUUID() UUID {
	// google/uuid.NewV7() ne retourne pas d'erreur (contrairement à gofrs/uuid)
	id := uuid.Must(uuid.NewV7())
	return UUID{UUID: id}
}

// MustParseUUID parse une chaîne UUID et panic si invalide.
// Utile pour constantes ou valeurs hardcodées garanties valides.
func MustParseUUID(s string) UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(fmt.Sprintf("invalid UUID string %q: %v", s, err))
	}
	return UUID{UUID: id}
}

// ParseUUID parse une chaîne UUID et retourne erreur si invalide.
func ParseUUID(s string) (UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return UUID{}, err
	}
	return UUID{UUID: id}, nil
}

// UUIDFromBytes crée un UUID depuis bytes 16 octets.
func UUIDFromBytes(b []byte) (UUID, error) {
	id, err := uuid.FromBytes(b)
	if err != nil {
		return UUID{}, err
	}
	return UUID{UUID: id}, nil
}

// String retourne la représentation texte de l'UUID.
// Format : "550e8400-e29b-41d4-a716-446655440000"
func (u UUID) String() string {
	return u.UUID.String()
}

// Bytes retourne la représentation binaire de l'UUID (16 octets).
func (u UUID) Bytes() []byte {
	return u.UUID[:]
}

// IsZero retourne true si l'UUID est nil (00000000-0000-0000-0000-000000000000).
func (u UUID) IsZero() bool {
	return u.UUID == uuid.Nil
}

// Value implémente driver.Valuer pour stockage dans SQLite.
// Stocke l'UUID en format BLOB (16 octets) au lieu de TEXT (36 octets).
// Réduction 56% taille colonnes ID = meilleure performance cache.
func (u UUID) Value() (driver.Value, error) {
	if u.IsZero() {
		return nil, nil // NULL en SQL
	}
	return u.Bytes(), nil
}

// Scan implémente sql.Scanner pour lecture depuis SQLite.
// Supporte formats BLOB (16 octets) et TEXT (36 octets) pour compatibilité.
func (u *UUID) Scan(src any) error {
	if src == nil {
		u.UUID = uuid.Nil
		return nil
	}

	switch v := src.(type) {
	case []byte:
		// Format BLOB préféré (16 octets)
		if len(v) == 16 {
			id, err := uuid.FromBytes(v)
			if err != nil {
				return fmt.Errorf("invalid UUID bytes: %w", err)
			}
			u.UUID = id
			return nil
		}

		// Format TEXT stocké en BLOB (36 octets UTF-8)
		if len(v) == 36 {
			id, err := uuid.Parse(string(v))
			if err != nil {
				return fmt.Errorf("invalid UUID string: %w", err)
			}
			u.UUID = id
			return nil
		}

		return fmt.Errorf("invalid UUID bytes length: %d (expected 16 or 36)", len(v))

	case string:
		// Format TEXT (compatibilité anciennes bases)
		id, err := uuid.Parse(v)
		if err != nil {
			return fmt.Errorf("invalid UUID string: %w", err)
		}
		u.UUID = id
		return nil

	default:
		return fmt.Errorf("unsupported UUID type: %T", src)
	}
}
