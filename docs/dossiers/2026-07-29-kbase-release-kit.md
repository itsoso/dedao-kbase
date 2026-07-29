# KBase Release Kit Delivery Dossier

## Status

G1-G2 passed. G3-G4 are in progress. G5-G6 have not started.

No release-kit commit has been pushed, merged, or deployed.

## Requirement

Convert the proven KBase release procedure into a repeatable repository
capability so future releases do not rediscover frontend build order,
configuration requirements, Linux permission behavior, Nginx compatibility,
or rollback steps during production deployment.

## Gate Decisions

### G1 - Admission

PASS. The preceding rollout required multiple operator-only corrections before
the candidate could pass Linux and Nginx preflight. Keeping those corrections
outside the repository would repeat the same risk and delay.

### G2 - Feasibility And Risk

PASS with controls.

- Keep the current systemd and Nginx runtime architecture.
- Split release work into immutable assemble, prepare, and install stages.
- Require explicit host inputs; do not publish host paths or credentials.
- Keep production installation outside CI and behind explicit authorization.
- Test both successful replacement and complete rollback before deployment.

### G3 - Verification

IN PROGRESS.

### G4 - Independent Review

PENDING successful G3.

### G5 - Deployment Health

PENDING explicit release authorization after G4.

### G6 - Online Verification

PENDING successful G5.

