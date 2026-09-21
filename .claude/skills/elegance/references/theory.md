# Information and elegance

An organization runs on a representation of a reality it cannot hold directly. This is what that representation is for, why every element in it costs something, and how it is kept in orbit. The terms build in order.

## 1 · Reality and perception

### 1.1 Reality and representation

Reality is the shared space, and nobody holds it directly. We hold a representation — information captured so someone who was not there can act later — through perception that is partial and delayed, so it is only ever as fresh as its last contact with the world.

### 1.2 Elegance

**Elegance is the presentation of data or concepts with maximum clarity using the fewest possible elements.** Clarity is predictive validity from the reader's side: acting on the node lands where intended. Fewest is the cost side: every element is a node that can drift, must be read, and must agree with its neighbors, so the cost of the next change rises with the count. The two are one objective because an element that adds nothing to clarity only adds to drift. **Abstractness is feedback latency,** the time until reality says a node is wrong: minutes for a file, a decade for a principle. That ordering is the ladder a node's **scope** sits on; section 5 divides it into tiers. **Generality is not vagueness:** a crisp principle is tested by what derives from it; a vague one has no feedback and is never corrected.

### 1.3 Completeness and attention

**Completeness is bounded by purpose.** Every representation is incomplete; the only question is whether what is missing would change an action, and most absences would not. **Attention follows the map,** so regions it stops pointing at decay unseen — and every element added spreads attention thinner over the rest.

## 2 · Information and action

### 2.1 The loop

Information drives action, and action produces the next information. Four stations: **current** is what observation keeps true; **future** derives from it, what could be and once decided what will be; **action** changes the world; **past** is the record action leaves. Observation of the changed world refreshes the current and closes the loop. The graph meets reality at only two points, observation and action; every other edge is inference, cheap and unbounded where contact is expensive. Most drift enters where inference has outrun contact.

### 2.2 Time as the station

**Time is the station, not the tense:** read to act, written from observation, or written from action. A sentence does not carry which. A shopping list in the shopper's hand is future; come home without butter and the shopping is wrong. The same list in a detective's notebook is past; if it says butter and he bought none, the list is wrong. **The decision is a check mark:** a plan sits undecided or decided, differing only in whether someone with standing committed, and a separate node would duplicate it.

### 2.3 The graph

Treat the representation as a graph, improved by altering it, including deletion, more often than by expanding it. A node's **connectedness** — how much is downstream of it — is measurable where edges exist; where it disagrees with abstractness it is a signal: an abstract node with few dependents is a proposal, a concrete node with many a hidden load-bearing detail.

**The artifact is the node.** Code, schema, contract, and data are nodes, not things described from outside, and the best node carries its meaning in its name, types, structure, and tests. A comment is a second node, earning its place only for what the first cannot carry and only when shorter than what it describes.

**Edges are cheaper than copies:** a reference costs one lookup, a restatement costs agreement forever. The one legitimate copy is a summary, one scope above its source and linked back; at the same scope it is repetition, below its source misplacement. **A duplicate is a missing edge,** the cheapest defect to fix: one is the source, the other is deleted and pointed at it.

**What is derivable is not written down.** A count, a directory's contents, anything a reader can compute from the artifact in front of them, is wrong the moment things move; where a derived number serves a reader, the thing that computes it prints it. The expensive form is **a claim that quantifies over a set** — *the only*, *all but*, an enumeration of coverage — which goes false when the set grows in another node, so no diff over its own file shows it turning wrong. Derive membership where the set is defined.

## 3 · Drift

### 3.1 Drift

**Drift is the accumulating gap between representation and reality,** as incorrectness (they disagree) or incompleteness (reality holds what the graph lacks). It is a rate, not a level. **A check refuses incorrectness only,** so a green check is evidence of correctness, never of coverage — which is why holes must be representable ([§3.4](#34-holes)). Neither form yields to the obvious move: adding to end incompleteness breeds incorrectness, and in practice is the form observed almost every time.

### 3.2 How drift compounds

Three mechanisms. **Derivation:** an error in a fundamental node is inherited downstream before anyone rereads it. **Attention:** a drifted region stops being visited. **Residue:** a correction that leaves a trace of what it replaced pulls the next edit back toward the old shape. **Repetition is drift waiting to happen:** two nodes that must agree with nothing making them. **Misplaced scope compounds fastest:** a function comment explaining the architecture goes stale every time the architecture moves and is never corrected, because nobody looks for architecture inside a function.

### 3.3 Pricing drift

**Fundamentalness is abstractness times connectedness:** how many nodes a wrong one corrupts, times how long before reality reports it. **Confidence is the check that was passed,** never the author's feeling or the prose's fluency: strongest to weakest, a gate that makes the wrong state impossible, runtime validation, review by a person at that scope, a bare assertion. It reaches no further than the check ranged, and agent-generated content enters at assertion however it reads. **Risk is one minus confidence, times fundamentalness.**

### 3.4 Holes

Because incompleteness is invisible from inside, an open question is a named hole — a marker on an empty place at a known scope. A representation that cannot hold one can be corrected for correctness only.

## 4 · Alignment

### 4.1 The orbit

**Alignment is an orbit, not a state:** never aligned, only correcting drift faster than it accumulates. So **expand only as fast as you can check** — build the check, then go fast; where only judgment exists, slow down and build a check.

### 4.2 Where information goes

**Raise the rung before writing more.** Can the artifact carry it — a name, type, test? Can it be a gate rather than a sentence? A rule that became a paragraph is a permanent tax paid back at partial compliance. Strongest rung first: the artifact, a gate, runtime validation, colocation, a triggered procedure, a path-scoped note, and only last an always-loaded sentence, which buys routing, not knowledge.

**Place by scope: summarize upward, reference sideways, never explain downward.** The test is whether a reader here needs it and whether a reader elsewhere would ever find it here.

**Addition is the default, undone only on purpose.** For an agent, completion is rewarded, addition is locally safe, deletion needs global knowledge, and a document is a free place for uncertainty. So restraint needs its own artifact: non-goals in the plan, what was not changed at close, reuse-or-justify before anything new. A tool that captures information must be biased against addition or, applied widely, it manufactures exactly this debt. Its outputs in order: make the artifact carry it, link to where it lives, edit the node that holds it, and only last add one naming its scope and what it retires.

### 4.3 Keeping the loop closed

**Make the representation show its own drift:** every scope has a named instrument with a cadence, and holes are marked. **Make subtraction as cheap as addition:** the blocker on removal is what depends on the node, so the prune set is computable — low connectedness at assertion confidence, the same signature as over-addition. **Finish every change:** it is finished when nothing left describes what it replaced.

## 5 · The axes

Scope is the ladder of feedback latency in six tiers; time is the station in the loop. Every artifact sits in one cell of the grid they form, and every cell has a type of location where its information belongs. [scope.md](scope.md) holds the ladder, [time.md](time.md) the stations, [grid.md](grid.md) the cells, and each cell's file under [cells/](cells/) says what clarity and excess look like there.
