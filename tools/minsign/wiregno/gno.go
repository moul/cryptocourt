// Package wiregno declares the one gnovm type that appears on the addpkg wire,
// so the signer does not have to import gnovm/pkg/gnolang (which reaches the
// whole VM, and through it crypto/x509).
//
// It is its own Go package for a mechanical reason: amino asserts that the Go
// package path passed to NewPackage is the real package of each registered
// type, so two amino packages ("vm" and "gno") cannot be registered from one
// Go package.
package wiregno

import "minsign/internal/amino"

// MemPackageType mirrors gnolang.MemPackageType, which is a plain string.
// Only the amino name and the string value reach the wire.
type MemPackageType string

// MPUserAll is what gnokey stamps on a user package being deployed: no
// stdlibs, a gno.land path, tests included.
const MPUserAll MemPackageType = "MPUserAll"

// Amino name "gno" produces the encoded type "/gno.MemPackageType", matching
// gnovm/pkg/gnolang's own registration.
var Package = amino.RegisterPackage(amino.NewPackage(
	"minsign/wiregno",
	"gno",
	amino.GetCallersDirname(),
).WithTypes(
	MemPackageType(""), "MemPackageType",
))
