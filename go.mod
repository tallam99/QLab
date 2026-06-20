// Root module exists only so `mage` can compile magefile.go at the repo root.
// The application lives in the nested backend/ module; this module intentionally
// has no dependencies (the magefile uses the standard library only).
module github.com/tallam99/qlab

go 1.26
