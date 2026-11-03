// Package integration runs the P1 test vectors of B1–B4 against the real
// ois, oic and oi binaries: one OIS (deployment A) with SQLite or
// PostgreSQL, OIC-A and OIC-B, and the T1–T3 fixtures (B4 §4). Set OI_IT=1
// to run; OI_IT_BACKENDS selects "sqlite", "postgres" or both.
package integration
