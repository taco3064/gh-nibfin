# Repository instructions

## Shared Runner upgrades

Use the Nibfin Dashboard's Pause/Resume for an individual Issue when an
operator needs to hold that work at a stage boundary. Record which Issues were
paused for the maintenance and resume only those Issues after verification.

For a Core or Runner upgrade affecting the shared pool, follow the
[Core rollout instructions](https://github.com/taco3064/nibfin-core/blob/main/AGENTS.md#shared-runner-upgrades).
The deployment owns the idle checks, safe service stop, version switch and
restart. Individual Issue pauses are optional. Preserve existing pauses and
let admitted work settle before the services stop.

The Dashboard's Issue control is documented in [docs/dashboard.md](docs/dashboard.md).
