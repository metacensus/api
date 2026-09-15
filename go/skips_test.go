package contract_test

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// skips is the only place a convention in this package is deliberately not
// held, so `git log go/skips_test.go` is the whole history of every exception
// this contract has granted.
//
// **Adding an entry is meant to cost more than fixing the code**, because
// almost always it does. What it takes:
//
//   - Violation is what the check printed, verbatim. It is compared, so an
//     exception cannot be written by anyone who has not run the test and read
//     the failure.
//   - Why is the argument you would make in review. The test rejects a
//     restatement of Violation and a one-liner; "legacy" and "not worth it"
//     will get past it and should not get past a reader.
//   - Fix names the issue that removes this, or says permanent — which claims
//     the rule is wrong here, not that the code is. Choose on purpose.
//
// An entry that stops being needed fails the build until it is deleted, so
// nothing here can outlive what it excused.
var skips = map[string]skip{
	"route GET /healthcheck": {
		Violation: "write over GET",
		Why: "A read whose name does not say so. isRead knows List, Get and " +
			"Lookup; renaming this to GetHealth so a test can see it would be " +
			"the test wagging the contract, and GET is right for a health probe.",
		Fix: permanent,
	},
}

type skip struct {
	Violation string
	Why       string
	Fix       string
}

const permanent = "permanent"

// holdToConventions fails on a subject that newly breaks a convention, on one
// whose breakage has changed under an existing skip, and on a skip that is no
// longer needed.
//
// kind prefixes the subject so one list serves checks over routes, services and
// messages without their keys colliding. found maps subject to what it broke.
func holdToConventions(t *testing.T, kind string, found map[string]string) {
	t.Helper()

	for _, subject := range slices.Sorted(maps.Keys(found)) {
		key := kind + " " + subject
		s, ok := skips[key]
		if !ok {
			t.Errorf("%s is newly non-conforming: %s.\n"+
				"Fix it, or — if it is deliberate — add %q to skips in skips_test.go "+
				"with the argument for it.", subject, found[subject], key)
			continue
		}
		if s.Violation != found[subject] {
			t.Errorf("%s was skipped for %q and now breaks %q. The reason it was "+
				"granted is not the reason it is failing; re-argue it or fix it.",
				subject, s.Violation, found[subject])
		}
	}

	for _, key := range slices.Sorted(maps.Keys(skips)) {
		subject, ok := strings.CutPrefix(key, kind+" ")
		if !ok {
			continue
		}
		if _, broken := found[subject]; !broken {
			t.Errorf("%s now conforms; delete its skip.", key)
		}
	}
}

// minWhy is a speed bump rather than a judge: it makes a one-word dismissal
// fail, and says nothing about whether a longer one is any good.
const minWhy = 80

var issueRef = regexp.MustCompile(`^#\d+$`)

// An exception that does not say why is not an exception; it is a silence with
// a struct literal around it.
func TestSkipsAreArgued(t *testing.T) {
	for _, key := range slices.Sorted(maps.Keys(skips)) {
		s := skips[key]
		why := strings.TrimSpace(s.Why)

		switch {
		case s.Violation == "":
			t.Errorf("%s: no Violation. It has to be what the check printed, so that "+
				"granting an exception means having seen the failure.", key)
		case why == "":
			t.Errorf("%s: no Why.", key)
		case strings.EqualFold(why, strings.TrimSpace(s.Violation)):
			t.Errorf("%s: Why restates Violation. The question is not what it breaks "+
				"but why breaking it is right here.", key)
		case len(why) < minWhy:
			t.Errorf("%s: Why is %d characters. Say why the rule is wrong for this "+
				"one, not that it is inconvenient.", key, len(why))
		case s.Fix != permanent && !issueRef.MatchString(s.Fix):
			t.Errorf("%s: Fix is %q. It must be the issue that removes this, like "+
				"\"#12\", or %q if the rule is wrong here rather than the code.",
				key, s.Fix, permanent)
		}
	}
}
