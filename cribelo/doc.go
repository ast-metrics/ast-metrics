// Package cribelo is the engine of Cribelo: it reads a change in a git
// repository and builds its reading map, the files worth a human reader, in
// which order, with the lines to read closely and those to glance at, and
// what the diff does not show. Everything it computes is deterministic: the
// same repository state and the same weights give the same plan.
//
// The package stops at the data. A Plan, a Backtest, their facts and reasons
// are worded in plain sentences and serialized as JSON here; how they are
// laid out for a terminal or a pull request comment is the business of the
// command line, which lives in github.com/cribelo/cribelo and pins a revision
// of this module. Nothing here writes to standard output.
package cribelo
