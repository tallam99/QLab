package store

import "github.com/google/uuid"

// User is a person known to the application. A user is global, not lab-scoped (one
// person can belong to several labs via labs_users), so the users table carries no
// labs_id and is not under row-level security — these reads run unscoped.
//
// FirebaseUID is the stable external identity from the auth provider. It is empty
// between invite and first login (the column is NULL until then); first login links
// it (LinkFirebaseUID). Email is the canonical lowercase address — the invite key
// and the join between a verified token and the local row.
type User struct {
	ID          uuid.UUID
	FirebaseUID string
	Email       string
	FirstName   string
	LastName    string
}
