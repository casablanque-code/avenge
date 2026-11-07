# E-008: does the disk stay bounded?

**Question:** with sensors running non-stop, does the database stop growing once `RETENTION_DAYS` have passed, and how large is the plateau?
**Level:** S (synthetic sensors; the arithmetic does not depend on the signal)
**Status:** planned
**Hypotheses:** H4 (long-running deployment)

## Pass line (to be fixed before the run)

To decide first: the largest plateau we accept for one vibration and one audio sensor at the default 30 days, and for ClickHouse's own tables. The arithmetic in [OPERATIONS](../OPERATIONS.md#data-retention) predicts a small figure; the experiment checks whether the prediction is right and whether anything else (system logs, Docker logs, Mosquitto) grows.

## Plan

1. Start the core stack and two simulated sensors, one per profile, with `RETENTION_DAYS=3` so the plateau arrives within a week.
2. Once a day, record the output of the `system.parts` query from OPERATIONS, `docker system df -v` and the size of `/var/lib/clickhouse`.
3. Run for at least 10 days. The sizes must stop growing after day 3 to 4, give or take the merge delay.
4. Repeat the first day with the system-log limits switched off, to see what they save.

## What this will not show

Real signals produce more anomaly events than the simulators; the volume of events is small next to the telemetry, but it is not measured here.
