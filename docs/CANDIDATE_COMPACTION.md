# Candidate Compaction Architecture

## Short description

This RFC adds a candidate-compaction layer on top of an append-only records store.
A candidate is a proposed canonical version of a record. It is stored outside the main journal until it is approved, resolved and compacted.
Resolution is partition-wide: all pending candidates in one partition are decided together and split into applied and denied.
Compaction materializes applied candidates into the journal partition by rewriting that partition.

This design is intentionally strict, small-scale, and file-based.

## Main entities

Record -- canonical row stored in journal/{partition}.
Candidate -- proposed canonical row for a record_id in a partition.
Candidate identity: partition, record_id, submitted_at, client_id
Candidate states: pending; applied; denied.

Possible directory layout:
- journal/{partition} — canonical materialized records
- candidates/pending/{partition} — unresolved candidates
- candidates/applied/{partition} — accepted candidates waiting for compaction
- candidates/denied/{partition} — rejected candidates
- candidates/audit/... — operational records and trace

## Main logic

### Submit
- validate candidate shape
- create candidates/pending/{partition} if needed
- append candidate to candidates/pending/{partition}
- if another pending candidate exists for the same record_id, still append and log duplicate
Limitations:
- submit is blocked during resolve same partition
- submit is allowed while candidates/applied/{partition} exists

### Resolve
- run for non-empty candidates/pending/{partition}
- resolves the whole pending partition at once
- every candidate becomes either applied or denied
- for one record_id, at most one candidate may be applied
- collision case is exceptional -> current outcome is deny all
- successful resolve always creates both: candidates/applied/{partition} and candidates/denied/{partition}
- resolved candidates are removed from candidates/pending/{partition} atomically
Limitation:
- resolve is blocked, if candidates/applied/{partition} already exists
- write applied/denied and remove pending should be atomically or idempotent

### Compact
- read journal/{partition} in original order
- replace rows whose record_id matches an applied candidate
- if an candidate from candidates/applied/{partition} has no matching row, append it to the end
- replace journal partition
- delete candidates/applied/{partition} after successful compaction
Limitation:
- replace and delete candidates/applied/{partition} should be atomically or idempotent
Notes:
- denied/{partition} is independent from compaction

## Main invariants
- canonical state lives in journal/{partition}
- unresolved candidates live in candidates/pending/{partition}
- resolve is atomic and partition-wide
- successful resolve always creates both candidates/applied/{partition} and candidates/denied/{partition}
- at most one unresolved applied candidates/pending/{partition} may exist per partition
- compaction is replace-or-append by record_id


## Failure rules

If resolve fails before atomic replacement completes, committed state must remain readable and consistent.

If compaction installs the new record successfully but fails to delete applied/{partition}, keep applied/{partition} and allow retry.
Re-running compaction on the same applied batch must be safe -> idempotent.

Locking and protocol restrictions

Per partition:
- submit and resolve do not overlap
- resolve and compaction do not overlap
- if applied/{partition} exists, resolve is blocked
- submit may continue while applied/{partition} exists

## Journal

journal/ is temporary trace audit
Useful event types:
- candidate_submitted
- candidate_duplicate_detected
- partition_resolved
- candidate_resolved
- partition_compacted
- recovery_cleanup

To log action on candidate use candidate identity.
