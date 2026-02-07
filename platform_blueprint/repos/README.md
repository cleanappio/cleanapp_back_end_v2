# Repos

The platform repo should pin exact versions of the three codebases.

Recommended options:

## Option A: Git submodules

Pros:
- explicit SHAs
- easy to update intentionally

Cons:
- submodule ergonomics

## Option B: Git subtree

Pros:
- everything in one repo

Cons:
- more work to sync

## Minimum requirement

Regardless of approach, the platform repo must record:
- backend SHA
- frontend SHA
- mobile SHA
- contract versions (OpenAPI, events)

