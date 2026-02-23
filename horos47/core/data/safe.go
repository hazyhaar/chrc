// Package data fournit helpers sécurisés gestion erreurs base données.
// Pattern extrait depuis audit Gemini 2026-01-29 identifiant 142 suppressions erreurs.
package data

import (
	"database/sql"
	"io"
	"log"
)

// SafeClose ferme io.Closer avec logging erreur si échec.
// Remplace pattern _ = rows.Close() _ = stmt.Close() supprimant erreurs silencieusement.
//
// Usage:
//
//	rows, err := db.Query(...)
//	defer data.SafeClose(rows, "close query rows")
//
// Rationale: Fermeture échouée indique fuite ressources descripteurs fichiers pool connexions saturé.
// Logging permet détection problèmes infrastructure versus silence dangereux.
func SafeClose(closer io.Closer, context string) {
	if closer == nil {
		return
	}

	if err := closer.Close(); err != nil {
		log.Printf("[DATA] WARN: failed to close %s: %v", context, err)
	}
}

// SafeTxRollback rollback transaction avec logging erreur si échec non trivial.
// Remplace pattern _ = tx.Rollback() supprimant erreurs silencieusement.
//
// Usage:
//
//	tx, _ := db.Begin()
//	defer data.SafeTxRollback(tx, "cleanup transaction")
//
//	// ... operations ...
//
//	if err := tx.Commit(); err != nil {
//	    return err // defer appellera Rollback automatiquement
//	}
//
// Rationale: Rollback peut échouer si transaction déjà commit ou rollback.
// sql.ErrTxDone attendu après commit réussi donc filtré silencieusement.
// Autres erreurs indiquent problèmes infrastructure donc loggées.
func SafeTxRollback(tx *sql.Tx, context string) {
	if tx == nil {
		return
	}

	if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
		log.Printf("[DATA] WARN: failed to rollback %s: %v", context, err)
	}
}
