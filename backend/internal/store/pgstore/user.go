package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore/sqlcgen"
)

// Users are global (not lab-scoped) and the users table is not under row-level
// security, so these reads/writes run directly on the pool — no inLabTx, no
// app.current_lab_id. They resolve a caller's identity before any lab is chosen.

// UserByFirebaseUID loads the user linked to a Firebase identity. A missing row is
// store.ErrNotFound (the identity has not been provisioned yet).
func (s *Store) UserByFirebaseUID(ctx context.Context, firebaseUID string) (store.User, error) {
	row, err := s.q.UserByFirebaseUID(ctx, firebaseUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	if err != nil {
		return store.User{}, fmt.Errorf("load user by firebase uid: %w", err)
	}
	return store.User{
		ID: row.UsersID, FirebaseUID: derefString(row.FirebaseUid),
		Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
	}, nil
}

// UserByEmail loads the user with the given canonical email. A missing row is
// store.ErrNotFound (no invite at that address).
func (s *Store) UserByEmail(ctx context.Context, email string) (store.User, error) {
	row, err := s.q.UserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	if err != nil {
		return store.User{}, fmt.Errorf("load user by email: %w", err)
	}
	return store.User{
		ID: row.UsersID, FirebaseUID: derefString(row.FirebaseUid),
		Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
	}, nil
}

// LinkFirebaseUID binds a Firebase uid to an existing user row (first-login
// provisioning), filling blank name parts from the provider and recording the user
// as their own updater. A missing target row is store.ErrNotFound.
func (s *Store) LinkFirebaseUID(ctx context.Context, userID uuid.UUID, firebaseUID, firstName, lastName string) (store.User, error) {
	row, err := s.q.LinkFirebaseUID(ctx, sqlcgen.LinkFirebaseUIDParams{
		FirebaseUid: firebaseUID,
		FirstName:   firstName,
		LastName:    lastName,
		Actor:       nilUUID(userID),
		UsersID:     userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	if err != nil {
		return store.User{}, fmt.Errorf("link firebase uid: %w", err)
	}
	return store.User{
		ID: row.UsersID, FirebaseUID: derefString(row.FirebaseUid),
		Email: row.Email, FirstName: row.FirstName, LastName: row.LastName,
	}, nil
}
