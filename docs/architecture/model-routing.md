# Model routing

Status: done.

## Rule

Work on this project is split across two model tiers by blast radius, not by convenience.

| Tier | When | Examples |
|---|---|---|
| Opus | Safety-critical, high blast radius | the deletion engine, the path-validation and capability layer, privilege-escalation guards, protection-rule schema design, any security review pass |
| Sonnet | Volume work, well-specified once the core exists | CLI plumbing, serializers, test scaffolding, rule-file authoring once the schema is fixed, documentation, refactors |

Fable is not used on this project. It has not been evaluated on a contained, low-risk task
elsewhere, and this project's failure mode (irreversible data loss) is the wrong place to run
that evaluation.

## No silent switching

Before any Opus-tier work begins, the agent states exactly what is about to be built and why it
needs that tier, then waits for the user to switch model or explicitly agree to proceed. The
agent never switches tiers on its own judgment alone, even when it believes the work clearly
warrants it.

## Why this split

The deletion engine and path validation are the parts of this project where a subtle mistake
destroys a user's data with no way back. That is a different risk profile from writing a JSON
serializer or a test fixture, and it is worth paying for a stronger model specifically there
rather than uniformly across the whole codebase.

## How this could improve with time

Once the deletion engine and path-validation layer are built, reviewed, and covered by an
adversarial test suite, later additions to those packages may not need the same tier if they
are small, well-scoped changes reviewed against an already-locked contract. That judgment call
should still be surfaced to the user rather than made silently.
