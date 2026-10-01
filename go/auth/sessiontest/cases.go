package sessiontest

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/metacensus/api/go/auth"
)

type sessionsCase struct {
	name    string
	promise string
	run     func(*scene)
}

type scene struct {
	t          *testing.T
	s          auth.Sessions
	now        *time.Time
	refreshTTL time.Duration
	promise    string
}

var issueCases = []sessionsCase{
	{
		name:    "mints a pair for the caller",
		promise: "Issue opens a new session lineage for a freshly authenticated caller (login, sign-up) and mints its first pair",
		run: func(sc *scene) {
			ada := sc.caller()
			tok := sc.issue(ada)
			sc.resolves(tok.Access, ada)
			sc.refresh(tok.Refresh, ada)
		},
	},
	{
		name:    "empty callerID",
		promise: "it refuses an empty callerID",
		run: func(sc *scene) {
			if _, err := sc.s.Issue(sc.t.Context(), ""); err == nil {
				sc.fail("Issue(\"\") succeeded")
			}
		},
	},
}

var resolveCases = []sessionsCase{
	{
		name:    "live until AccessExpiry",
		promise: "An access token resolves until its AccessExpiry",
		run: func(sc *scene) {
			ada := sc.caller()
			tok := sc.issue(ada)
			sc.at(tok.AccessExpiry.Add(-time.Second))
			sc.resolves(tok.Access, ada)
		},
	},
	{
		name:    "refused from AccessExpiry",
		promise: "and is refused from that instant on",
		run: func(sc *scene) {
			tok := sc.issue(sc.caller())
			sc.at(tok.AccessExpiry)
			sc.resolveRefused(tok.Access)
		},
	},
	{
		name:    "unknown",
		promise: "an unknown access token is refused",
		run:     func(sc *scene) { sc.resolveRefused(rand.Text()) },
	},
}

var refreshCases = []sessionsCase{
	{
		name:    "rotates the pair",
		promise: "Refresh consumes a refresh token and mints a new pair for the same caller",
		run: func(sc *scene) {
			ada := sc.caller()
			first := sc.issue(ada)
			second := sc.refresh(first.Refresh, ada)
			if second.Access == first.Access || second.Refresh == first.Refresh {
				sc.fail("Refresh handed back a token it was minted from")
			}
			sc.resolves(second.Access, ada)
		},
	},
	{
		name:    "consumed token reused",
		promise: "A refresh token is single-use: once consumed it is refused.",
		run: func(sc *scene) {
			ada := sc.caller()
			tok := sc.issue(ada)
			sc.refresh(tok.Refresh, ada)
			sc.refreshRefused(tok.Refresh)
		},
	},
	{
		name:    "live within the refresh lifetime",
		promise: "once the refresh lifetime has passed since it was minted",
		run: func(sc *scene) {
			ada := sc.caller()
			minted := *sc.now
			tok := sc.issue(ada)
			sc.at(minted.Add(sc.refreshTTL - time.Second))
			sc.refresh(tok.Refresh, ada)
		},
	},
	{
		name:    "refused once the refresh lifetime has passed",
		promise: "It is refused too once the refresh lifetime has passed since it was minted",
		run: func(sc *scene) {
			minted := *sc.now
			tok := sc.issue(sc.caller())
			sc.at(minted.Add(sc.refreshTTL))
			sc.refreshRefused(tok.Refresh)
		},
	},
	{
		name:    "unknown",
		promise: "and when it is unknown",
		run:     func(sc *scene) { sc.refreshRefused(rand.Text()) },
	},
	// Omitted: whether replaying a consumed token also ends its lineage is unstated.
	// Omitted: whether Refresh ends the access token minted beside the one it consumes is unstated.
}

var revokeCases = []sessionsCase{
	{
		name:    "ends the lineage",
		promise: "every access and refresh token minted on it is refused from then on",
		run: func(sc *scene) {
			ada := sc.caller()
			first := sc.issue(ada)
			second := sc.refresh(first.Refresh, ada)
			sc.revoke(second.Refresh)
			sc.resolveRefused(first.Access)
			sc.resolveRefused(second.Access)
			sc.refreshRefused(second.Refresh)
		},
	},
	{
		name:    "leaves other lineages",
		promise: "every other lineage, the same caller's included, is untouched",
		run: func(sc *scene) {
			ada, bob := sc.caller(), sc.caller()
			revoked, adas, bobs := sc.issue(ada), sc.issue(ada), sc.issue(bob)
			sc.revoke(revoked.Refresh)
			sc.resolves(adas.Access, ada)
			sc.resolves(bobs.Access, bob)
			sc.refresh(adas.Refresh, ada)
			sc.refresh(bobs.Refresh, bob)
		},
	},
	{
		name:    "a token it does not hold",
		promise: "Revoking a token it does not hold is not an error",
		run:     func(sc *scene) { sc.revoke(rand.Text()) },
	},
	{
		name:    "double logout",
		promise: "so a double logout is idempotent",
		run: func(sc *scene) {
			tok := sc.issue(sc.caller())
			sc.revoke(tok.Refresh)
			sc.revoke(tok.Refresh)
		},
	},
	// Omitted: whether a consumed refresh token still names its lineage to Revoke is unstated.
}

func (sc *scene) caller() string { return "sessiontest-" + rand.Text() }

func (sc *scene) fail(format string, args ...any) {
	sc.t.Helper()
	sc.t.Fatalf(format+"\n  promise: %s", append(args, sc.promise)...)
}

func (sc *scene) at(instant time.Time) {
	sc.t.Helper()
	if instant.Before(*sc.now) {
		sc.fail("clock cannot move back from %v to %v", *sc.now, instant)
	}
	*sc.now = instant
}

func (sc *scene) issue(caller string) auth.Tokens {
	sc.t.Helper()
	tok, err := sc.s.Issue(sc.t.Context(), caller)
	if err != nil {
		sc.fail("Issue(%q): %v", caller, err)
	}
	return tok
}

func (sc *scene) resolves(access, caller string) {
	sc.t.Helper()
	got, err := sc.s.Resolve(sc.t.Context(), access)
	if err != nil || got != caller {
		sc.fail("Resolve = %q, %v; want %q", got, err, caller)
	}
}

func (sc *scene) resolveRefused(access string) {
	sc.t.Helper()
	if got, err := sc.s.Resolve(sc.t.Context(), access); err == nil {
		sc.fail("Resolve = %q; want it refused", got)
	}
}

func (sc *scene) refresh(refresh, caller string) auth.Tokens {
	sc.t.Helper()
	got, tok, err := sc.s.Refresh(sc.t.Context(), refresh)
	if err != nil || got != caller {
		sc.fail("Refresh = %q, %v; want %q", got, err, caller)
	}
	return tok
}

func (sc *scene) refreshRefused(refresh string) {
	sc.t.Helper()
	if got, _, err := sc.s.Refresh(sc.t.Context(), refresh); err == nil {
		sc.fail("Refresh = %q; want it refused", got)
	}
}

func (sc *scene) revoke(refresh string) {
	sc.t.Helper()
	if err := sc.s.Revoke(sc.t.Context(), refresh); err != nil {
		sc.fail("Revoke: %v", err)
	}
}
