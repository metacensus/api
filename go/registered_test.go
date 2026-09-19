package contract_test

// Every check in this package reads protoregistry.GlobalFiles, which holds
// only what this binary imported — these blank imports are what put each
// contract package there. A package missing here fails forEachContractFile.
import (
	_ "github.com/metacensus/api/go/metacensus/public/v1"
	_ "github.com/metacensus/api/go/metacensus/v1"
)
