package contract_test

// Every check in this package reads protoregistry.GlobalFiles, which holds
// only what something in the test binary imported. These blank imports are
// what put each contract package there.
//
// **A package missing from this list is invisible, not failing.** Its files
// never register, so every range over the registry skips it and every
// invariant passes without having read it. That was survivable while there was
// one package and one test file already imported it for other reasons; with
// two it is a trap, so the imports are here, alone, with the reason.
//
// forEachContractFile fails per package when a package in contractPackages
// registers no files, which is what turns a forgotten line here into a red
// build rather than a quiet one.
import (
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
)
