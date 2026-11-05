# Data Model: End-to-End Suite and Demo Script

No stored data changes. Run-scoped resources (all named `tiny-s3-e2e-<pid>-<role>` and removed at exit):

| Resource | Role |
|----------|------|
| network | private network for the run; the server answers as `s3d` |
| volume | fresh data volume for the server |
| server container | the image built from `Dockerfile`, `-data-dir=/data` on the volume |
| client container | e2e image (aws CLI v2 + `cmp`), runs `scripts/e2e/checks.sh` via `docker exec` |
| inspector container | mounts the volume read-only to inspect `tmp/` and `data/` |
