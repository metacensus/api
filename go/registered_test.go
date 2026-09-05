package contract_test

// Every check in this package reads protoregistry.GlobalFiles, which holds only
// what the test binary imported. These blank imports are what put each contract
// package there.
//
// A package missing here registers no files, so forEachContractFile fails for
// it and TestEveryPackageIsGoverned catches one that is missing from
// contractPackages as well.
import (
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
)
