package store

import "reflect"

// A Draft method is one joining the contract, declared here before every
// implementation has it so a change can start in api and reach the services
// one at a time. Each is its own single-method interface, named for the
// method, beside Store rather than in it, and listed in Drafts:
//
//	type ArchiveTopic interface {
//		ArchiveTopic(ctx context.Context, callerID, id string) error
//	}
//
// A store that lacks a Draft method answers that method's route with 501, and
// storetest skips its Draft cases there as not implemented. A method leaves
// Draft by moving into Store; a release carries no Draft method (make
// release-check), so a tagged Store is one every implementation must satisfy.
var Drafts = []reflect.Type{}
